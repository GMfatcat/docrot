<!-- docrot:ignore-file -->
<!-- This report quotes paths and symbols from other repositories; they are
     not claims about docrot itself, so docrot skips the whole file. -->
# Field report: docrot on three Rust crates

Date: 2026-09-23. Binary: `docrot 0.5.0`, default `.docrot.json`, git
enabled, shallow clones from GitHub. The crates were chosen for how they
document themselves: ripgrep (a CLI with a long FAQ and a GUIDE), axum (a
web framework whose reference pages are Markdown files pulled into rustdoc
with `include_str!`, full of intra-doc links) and clap (a workspace of five
crates where the facade crate re-exports everything with
`pub use clap_builder::*`).

## Numbers

| crate | .rs files | lines of Rust | docs | references | errors | warnings | info | time |
|---|---|---|---|---|---|---|---|---|
| ripgrep | 110 | 56,386 | 22 | 1,265 | 0 | 2 | 22 | 0.16 s |
| axum | 300 | 46,706 | 37 | 743 | 10 | 56 | 5 | 0.20 s |
| clap | 337 | 84,668 | 73 | 1,776 | 9 | 76 | 9 | 0.46 s |

Times are the warm run with git; the cold run of clap takes 1.1 s. No
crate needed configuration.

## What it found (verified by hand)

- **ripgrep** is clean. The two warnings are `benchsuite/runs/…/README.md`
  files that link `runs/<date>/raw.csv` relative to `benchsuite/` rather
  than to themselves; the file exists one directory up and the finding
  says so. The 22 info findings are dot-files named in the changelog
  (`.rgignore`, `.jj`) and shell profiles in the FAQ (`.zshrc`).
- **axum**: every error and 53 of the 56 warnings sit in a `CHANGELOG.md`
  and name an API that a release removed — `axum::Server`,
  `axum::body::box_body`, `axum::sse`, `RequestParts::extract` and its
  siblings, `PathRejection::WrongNumberOfParameters`. Accurate, and the
  reason changelogs are excluded from the staleness and toolchain checks
  but not from symbol checks: a document that names a symbol is making a
  claim, even about the past. The three remaining warnings are on doc
  comments: `MakeService` and `WithQueryParams::to_uri` are a tower trait
  and a default trait method that the impl does not restate, and
  `Poll::Ready(None` is a tokenizer bite.
- **clap**: the same picture. The nine errors and 74 of the 76 warnings
  are the 3.x → 4.0 changelog: `clap::App`, `clap::AppSettings::*`,
  `App::get_version` and forty more names that became `Command`. The two
  `unknown-flag` warnings are `--register` and `--shell` in
  `clap_complete`'s changelog, flags of the generated completion command.
  `CONTRIBUTING.md`'s `make test-full` resolves against the Makefile's
  `test-%` pattern rule.

## What Rust taught the heuristics

The first run on axum reported 12 errors and 149 warnings, clap 30 and
85. Six rules removed the noise; every one is general.

1. **Multi-line `pub use` lists.** `pub use axum_core::response::{ … ,
   Response, … };` spans four lines. The first scanner only read one, so
   `axum::response::Response` did not exist. The list is now accumulated
   until its `;`, nested trees (`self::{path::{Path, RawPathParams},
   state::State}`) are walked, and `Type` re-exported under `pub use`
   is a name of the re-exporting module.
2. **Glob re-exports.** clap's `src/lib.rs` is `pub use clap_builder::*;`.
   A name missing from `clap` is now looked up under every module `clap`
   re-exports wholesale, recursively, and `clap::error::ErrorKind` finds
   `clap_builder::error::ErrorKind`. `pub use self::net::tcp;` makes
   `mycrate::tcp` another name of the module the same way.
3. **Paths into other crates.** `hyper::Body`, `serde::Deserialize`,
   `tokio::io::AsyncRead`: a `::` path whose first segment is lower-case
   and is neither a crate nor a module of the repository is a dependency,
   not a claim. Skipped, like `std::`.
4. **Types the example imports.** axum's middleware page writes
   `ServiceBuilder::map_request` after `use tower::ServiceBuilder;` in
   its code block. When a document's own examples import a name from a
   crate the repository does not own, `Name::anything` in that document
   is about that crate. This removed the only non-changelog symbol
   warnings on axum.
5. **Generic parameters and enum variants.** `S::Error` and `T::Item`
   are type parameters, never claims. `ArgAction::HelpLong` is an enum
   variant, which the first index did not record; variants are symbols
   now, inline (`enum Mode { Fast, Slow }`) or one per line.
6. **`impl` headers.** `impl<'s, F: Fn() -> clap::Command> CompleteEnv<'s,
   F>` defeated a regular expression (the `>` of `->`). The header is now
   read by a small bracket matcher, so methods attach to the right type.

Two rules came from the other direction. rustdoc intra-doc links
(`[extractors](crate::extract)`, `[x](Router::fallback)`) were path claims
in the first run — 24 info findings on axum — and are Rust symbols now. A
Makefile pattern rule (`test-%`) used to be dropped as "not a target";
`make test-full` matches it.

## What is still noisy

- A `Type::method` on an external type the document never imports
  (`StatusCode::NO_CONTENT` in a changelog) is a warning with a poor
  suggestion. The index cannot tell a removed local type from a foreign
  one without reading `Cargo.toml`'s dependency list; that is a possible
  next step.
- Default trait methods are recorded on the trait, not on every
  implementing type, so `WithQueryParams::to_uri` in a comment is a
  warning although the call compiles.
- `Poll::Ready(None` and `try_remove_` show that the comment tokenizer
  keeps an unbalanced parenthesis or a trailing underscore; harmless, and
  both are warnings on comments rather than on documents.

## How to reproduce

```sh
git clone --depth 1 https://github.com/BurntSushi/ripgrep ../surprise-rust/ripgrep   # etc.
python scripts/demo.py ../surprise-rust/ripgrep ../surprise-rust/axum ../surprise-rust/clap --out reports-rust
```
