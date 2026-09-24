# Changelog

## 1.0.0 — 2026-09-24

The planned scope is complete: every language the project set out to
cover, every tier of the roadmap worth doing, and the field runs behind
each rule. 1.0.0 adds only the wordmark: `docrot` and `docrot help` open
with a 2.5D ASCII banner, plain ASCII so that any console renders it and
absent from every output a script or CI job parses (`check`, `version`,
the report formats).

## 0.9.0 — 2026-09-24

The rest of `docs/roadmap.md`'s tier 2: five independent additions.

- `--format github` prints one GitHub Actions workflow command per finding
  (`::error file=README.md,line=12,title=missing-path::…`), so a job gets
  inline pull-request annotations on GitHub and Gitea without a SARIF
  upload; `--format junit` writes JUnit XML (one test case per document
  and rule, info findings as skipped cases) for the test panels of GitLab,
  Jenkins and Gitea. Both hide info and baselined findings like the text
  report.
- `pair-missing` (info, on the source) and `pair-orphan` (warning, on the
  translation): in a `docs/en/` ↔ `docs/<lang>/` tree with at least one <!-- docrot:ignore missing-path -->
  detected pair, a page missing on either side. Suffix pairs form no tree.
- `example-syntax`: every ```go block is parsed with `go/parser` under
  the shapes a snippet takes (file, statements, statements then
  declarations, switch cases, literal elements, struct fields, interface
  methods; imports hoisted). Elided (`...`, `…`, `{{`), go.mod and
  opted-out (```go ignore) blocks are skipped, changelogs too. A syntax
  error is a warning; a block that merely ends before its braces close is
  info, because on the 1,570 blocks of the meowbase repositories that
  shape was always an excerpt (`docs/field-report-examples.md`).
- Coverage for registered HTTP routes and for the leaf keys of the
  configuration samples, in `docrot coverage`, `--coverage` and every
  report format; `undocumented` findings for both with `coverage.report`.
  A route mentioned without a method covers every method; a mention that
  ends with a mounted router's path counts; a documented default counts as
  a mention of its key.
- `docrot fix [--apply]`: the resolver marks a `missing-path` finding as
  mechanically fixable when git history records the rename or the real
  file differs only by letter case (`data.fix` holds the corrected text,
  in the frame the document used). `fix` prints each line before and
  after and rewrites the documents only with `--apply`, keeping line
  endings and byte-order marks. Symbols, anchors and fuzzy suggestions
  stay manual.
- `stale-section` no longer counts a link to the document's own translation
  (a detected pair): a busy `README-zh.md` used to make the English intro
  that links to it "stale", while the pair rules already report that drift.

## 0.8.0 — 2026-09-24

C and C++, the last of the four languages the plugin table was built for.

- `.c .h .cc .cpp .cxx .hpp .hh .hxx .inl .ipp` (outside `build/`,
  `third_party/`, `vendor/`, `external/`, `deps/`) are indexed: macros,
  prototypes and definitions (K&R style included), out-of-line
  `Type::method` definitions, classes and structs with their methods and
  fields (access labels honoured), enum values — also curl's
  `CURLOPT(CURLOPT_URL, …)` macro lists — `typedef` and `using` aliases,
  and C++ namespaces (`namespace a::b`, `X_NAMESPACE_BEGIN` pairs).
  Documents may write `curl_easy_perform()`, `curl_easy_perform(3)`,
  `CURLOPT_URL(3)`, `json::parse`, `basic_json::dump`, `nlohmann::json`.
  Comments (`/* */`, `//`, Doxygen `///`) feed `docrot comments`.
- Also from C/C++: getopt-style option tables, CLI11 and cxxopts flags
  with `default_val`/`default_value` defaults and `envname`, `getenv`
  reads, `#include "x.h"` in code blocks against the tree (`<x.h>` is a
  system header when nothing matches), CMake targets for
  `cmake --build … --target x` and for `make x` when no Makefile exists,
  `cmake_minimum_required` for "requires CMake 3.16" claims, and CMake
  `option()` names, which are not environment variables.
- Never a claim: `std::`/`boost::` paths, a `::` path into another library,
  an unknown member of an alias, typedef or inheriting type, flags of the
  build system (`--enable-x`, `--with-x`, `--prefix`, `--std=`) or spelled
  with a dot, `LD_LIBRARY_PATH`-style environment variables, and an
  `UPPER_SNAKE` name that is a macro or enum value.
- When the language the classifier picked has no near-miss for a missing
  name, the suggestion comes from whichever present language has one.
- Field report on curl, nlohmann/json and CLI11: `docs/field-report-c.md`.

## 0.7.0 — 2026-09-24

C#, the third language through the plugin table.

- `.cs` files (outside `bin/`, `obj/` and generated `*.Designer.cs`/`*.g.cs`)
  are indexed: file-scoped and block namespaces, classes, structs,
  interfaces, enums, records, delegates and their methods, properties,
  fields, events and enum members, under `Namespace.Type.Member`. Documents
  may write that in full, `Type.Member`, `Type`, `Member()`, `Namespace.Type`
  or a type by an outer namespace. XML doc comments (`/// <summary>`) feed
  `docrot comments`.
- Never a claim: `Type.Member` where the type is declared nowhere in the
  repository (`TimeSpan.Zero`, a type of another assembly), members of the
  base class library, and an unknown member of a type that inherits. A
  dotted name equal to a `.csproj` package id is accepted.
- Also from C#: System.CommandLine options and their defaults,
  `Environment.GetEnvironmentVariable` and `Configuration["X"]` reads with
  `?? "default"`, minimal-API routes (`MapGet`, `MapGroup` prefixes followed
  through the group variable) and attribute-routed controllers,
  `dotnet add package` against the `.csproj` package ids, "requires .NET 6"
  against the lowest `TargetFramework`, and `appsettings*.json` among the
  default configuration samples.
- Field report on Polly, Humanizer and TodoApi: `docs/field-report-csharp.md`.

## 0.6.0 — 2026-09-24

JavaScript and TypeScript, the second language through the plugin table.

- `.js .mjs .cjs .jsx .ts .tsx` outside `node_modules/`, `dist/` and
  `build/` are indexed: functions, classes and their members (`#private`
  and `private` ones excluded from the exported surface), `const`/`let`/
  `var`, interfaces, types, enums and namespaces, object-literal keys —
  also of an object built inside a factory function, as fastify's instance
  is — `X.prototype.m`, `X.m = …`, `Object.defineProperty`/`defineProperties`,
  `export { a as b }`, `export * from`, `module.exports = { … }` and
  `exports.name`. A module is its file stem; `module.member` also matches
  a member of any class or object of that module (`reply.send`).
- Documents may write `client.fetchAll`, `Client.fetchAll()`, `fetchAll()`,
  `.parse()` and `Mode.Fast`. The document's own code blocks decide what a
  prefix means: after `import * as z from "zod"` in zod's repository
  `z.string()` is a claim; after `import express from "express"`
  `express.json()` is not. `import { Client } from "this-package"` claims
  that each name is exported; `import x from "this-package/sub"` claims
  the sub-path exists (`package.json` `exports` or the tree); relative
  imports are info.
- Also from JavaScript: commander and yargs flags with their defaults,
  `process.env.X` reads with `|| 'default'` fallbacks, express/koa/fastify/
  hono/NestJS routes, `npm install`-style claims from imports, "requires
  Node 18" against `engines.node`, JSDoc in `docrot comments`.
- General: an unknown member of a class that lists all its members is a
  finding, of one that `extends` another (or a Python class, a Rust type,
  an Odin struct) it is not; runtime globals (`console.log`, `Math.max`)
  are never paths; `\<` in a heading is text, not a tag; `NNNN` path
  segments are placeholders.
- Field report on fastify, hono and zod: `docs/field-report-js.md`.

## 0.5.0 — 2026-09-23

Language plugins, and Rust as the first language added through them.

- Every language except Go now goes through one interface
  (`internal/index/lang`) and one row in `model.Langs`; the extractor's
  naming rules, the resolver, the comment checks, the report summary and
  `docrot index --kind <language>` range over that table. Odin and Python
  behave exactly as before (the golden test and the self-check did not
  move). Adding a language is one index package plus one table row.
- Rust: `crate::module::item`, `Type::method`, `io::read_all` (any
  `::`-boundary suffix), enum variants and `name!()` macros resolve
  against a declaration index that follows the file tree, `impl`
  blocks, inline modules and `pub use` re-exports — single names, nested
  trees and `pub use other::*` globs, so `clap::Command` finds
  `clap_builder::Command`. rustdoc intra-doc links are symbol claims.
  Skipped: `std::`/`core::`/`alloc::`, paths into other crates
  (`hyper::Body`), types the document's own examples import from another
  crate (`ServiceBuilder::layer` after `use tower::ServiceBuilder;`) and
  generic parameters (`S::Error`).
- Rust claims beyond symbols: clap flags (`#[arg(long)]`, `.long("x")`),
  clap and `env::var` environment reads, `default_value`/`unwrap_or`
  defaults, axum/actix-web/rocket/tide routes, `cargo add`/`cargo install`
  against `Cargo.toml`'s package name, "requires Rust 1.70" against
  `rust-version`, `///` comments in `docrot comments`.
- Makefile pattern rules (`test-%`) satisfy `make test-full`; a link whose
  target is a broken URL (`]https://…`) is no longer a path claim.
- Field report on ripgrep (clean), axum and clap (every error and nearly
  every warning names an API their changelogs say was removed):
  `docs/field-report-rust.md`.

## 0.4.0 — 2026-09-23

The rest of `docs/roadmap.md`'s tier 2 that was worth doing.

- reStructuredText and AsciiDoc documents are checked like Markdown:
  `**/*.rst` and `**/*.adoc` are in the default `docs`, and a `.txt` that
  looks like RST is read as RST (Django). Sphinx roles, directives,
  toctrees, labels, autosectionlabel refs, literal and doctest blocks,
  tables and `.. docrot:ignore` are understood; `/rooted` targets climb to
  the source root; `:ref:` misses are warnings, info when intersphinx is
  configured. On requests the 21 RST pages produce one error (a removed
  module named in the changelog); on Django's 686 pages the errors are
  the deprecation timeline and release notes naming removed APIs, tutorial
  project files, and Sphinx labels owned by Python's own documentation.
- Persistent git cache: blame (by blob hash) and log (by HEAD) answers are
  kept in `<outDir>/git-cache.json`, pruned to what the run used. A second
  FastAPI run with git drops from 14.8 s to 1.9 s, meowbase from 1.7 s to
  0.2 s, with identical findings.
- `stale-symbol`: the body of a declaration a section names changed in
  several commits after the section was edited; replaces the section-level
  finding when it fires. Warning by default.
- `default-mismatch`: documented defaults ("`--port` defaults to `8080`",
  "(default: `info`)", a Default table column) against `flag.*` literals,
  `default:"…"` struct tags, typer/click/argparse literals and
  `os.getenv(NAME, default)`; booleans, numbers and durations compare by
  value. `docrot index --kind defaults`.
- Python index: module-level assignments of any case are symbols,
  attributes of known classes and objects resolve, and a name missing from
  an existing module is a warning that names where the name lives instead
  of being rescued by a same-named object elsewhere. Similar-name
  suggestions only score names of a compatible length: Django's 64,000
  symbols resolve in 4 s instead of 15 s.
- Paths that exist under a sub-tree anywhere are warnings naming the tree;
  `myapp/…` and `mysite/…` are placeholders; keys below a map-typed field
  are valid; bare manifest names (`package.json`, `Makefile`) are not claims.

## 0.3.0 — 2026-09-23

Everything in tier 1 of `docs/roadmap.md`, plus a performance fix.

- `missing-route`: HTTP paths in documents (`` `GET /v1/items` ``, table
  rows, http fences, curl examples, bare `` `/healthz` `` spans) against the
  routes the code registers — Go `net/http` patterns (Go 1.22 method
  prefixes, host-qualified), chi/gin/echo/gorilla method calls and prefixes,
  FastAPI/Flask decorators, Starlette `Route`/`Mount`, Django `path()`,
  `APIRouter` prefixes. Parameters normalise across frameworks, a literal
  segment matches a parameter, mounted routers match by their tail, and a
  path known only through other methods says so. `docrot index --kind
  routes` lists them. On FastAPI's 1,692 documents all 659 route claims
  resolve; on meowbase the one finding is a planned route that was never
  implemented.
- String-literal index: every identifier-like string literal and struct-tag
  value of the Go, Python and Odin sources is the last resort before a
  route, symbol, flag, environment variable or config key is reported
  missing, and the comment checks consult it too. Log fields, header names,
  `pflag`/`argparse` flags and routes registered through constants stop
  being false positives.
- ```` ```json ```` configuration examples are checked key by key against
  the struct tags and sample files (comments, trailing commas and `...`
  tolerated; a fragment `"key": value,` is wrapped): a dropped or misspelled
  key is a warning with a sibling suggestion (`timeout` → `timeout_ms`).
- `install-mismatch`, `toolchain-mismatch`, `missing-target`: `go get` /
  `pip install` / `npm install` of this project by the wrong path or name,
  "requires Go 1.21" against `go.mod`'s `go` directive and "Python 3.9+"
  against `requires-python`, and `make` / `npm run` / `just` / `task`
  targets against the runner files. Release notes and research notes join
  the historical defaults of `stale.exclude`, which the toolchain check
  also skips.
- `docrot check --changed [--since REF]`: only documents modified since
  HEAD (plus untracked ones), or since the merge base with REF; anchors of
  every document still resolve. A clean FastAPI tree checks in 0.5 s.
- Rule-scoped ignores (`<!-- docrot:ignore missing-path -->`,
  `docrot:ignore-start unknown-flag,unknown-env`) and explicit HTML ids
  (`<a id>`, `<a name>`, `<h2 id>`) as anchors.
- The Python span scanner converted the remaining byte slice to a string
  on every byte; fixing that takes a full FastAPI check from 8.3 s to
  2.3 s without git and pydantic from 22 s to 0.5 s.
  `DOCROT_CPUPROFILE=file` writes a CPU profile of `check`.

## 0.2.0 — 2026-09-23

- Output directory: every `docrot check` run now rewrites one directory —
  `outDir`, a dot-directory at the repo root by default — with the same
  report in every format, so a human and an agent always find the current
  findings in the same place. It holds a `.gitignore` of its own (written
  once, containing `*`) so the reports never reach a commit, each file is
  renamed into place from a temporary file so an interrupted run leaves no
  half-written report, and the directory is excluded from document
  discovery so yesterday's report is never checked as documentation.
  `--out-dir` moves it, `--no-out` skips it, an empty `outDir` disables it.
- New `md` report format, also available as `--format md`: Markdown written
  for an agent rather than a terminal. It explains what a finding is, what
  the severities mean and how to silence a false positive; groups findings
  by document; always includes info-level findings; lists the rules it used
  with their descriptions; and ends with a fix checklist of the distinct
  document/rule pairs, most severe first.
- Python support tuned on five real projects (httpx, Starlette, Typer,
  Pydantic, FastAPI): modules and packages count as references, top-level
  imports re-export names (`fastapi.status`), standard-library modules and
  example objects (`app.routes`) are skipped, tutorial modules under
  `docs_src/` or `tests/` are info, MkDocs-style `../../docs_src/x.py`
  includes resolve from ancestor directories, `{ #custom-id }` anchors and
  mkdocstrings pages are understood, slugs keep underscores. See
  `docs/field-report-python.md`.
- Letter case is checked the same way on every platform: `docs/foo.md`
  for a file called `Docs/Foo.md` is a finding on Windows and macOS too,
  with a message that says so; `README.MD` and `readme.md` are discovered
  by `**/*.md`.
- HTML/JSON reports no longer contain U+FFFD from mid-rune truncation.
- Comment checks, piggybacking on documented symbols (`stale-comment`,
  `comment-mentions-missing`), plus `docrot comments` for a full sweep;
  Go doc comments, Python docstrings and Odin comments.
- `maxFileMB` (default 8): documents and sources above the cap are skipped
  with a warning instead of being parsed; binaries were never opened and
  still are not — only their names are indexed.

## 0.1.0 — 2026-09-23

First complete release, built in one night against real repositories.

### Checks

- `missing-path`, `missing-command`, `missing-import`: file, script and Go
  import claims, resolved relative to the document, the repo root, globs,
  configured sibling repositories and `.gitignore` rules (build artifacts
  are never reported). Suggestions from same-name files, case fixes, typos
  and git rename history.
- `missing-symbol`: Go (`go/parser`: packages, funcs, types, methods,
  fields, generics), Odin and Python declarations.
- `unknown-flag`, `unknown-env`, `unknown-config-key`: cross-checked with
  `flag.*` definitions, `os.Getenv`-style reads and `json`/`yaml`/`toml`
  tag paths plus JSON sample files.
- `broken-anchor`: GitHub slugs, CJK headings, duplicate suffixes.
- `broken-url` (opt-in `--net`).
- `stale-section`: `git blame` section age versus `git log` churn of the
  files the section references.
- `pair-heading/code/link/table/number/lag`: bilingual document drift.
- `undocumented`: documentation coverage of the exported surface.

### Product

- Commands: `check`, `explain`, `baseline`, `coverage`, `pairs`, `index`,
  `init`, `version`; flags may follow the directory argument.
- Reports: text (info hidden unless `--info`), JSON, SARIF 2.1.0 (with
  `baselineState`), single-file HTML with filters and dark mode.
- Baseline file keyed by line-independent fingerprints.
- `.docrot.json` with defaults for excludes, pair patterns, config samples,
  staleness thresholds and dated-document exclusions, severities, siblings.
- Inline `docrot:ignore` directives.
- `scripts/verify.py` (gofmt, vet, test, build, fixture, self-check,
  formats) and `scripts/demo.py` (batch reports over repositories).

### Fixed after an independent review

- Parser crash on a link destination ending in a backslash.
- Data race in engine warnings emitted from worker goroutines.
- Broken anchors in `docs/*.md` pointing at root-level files were silently
  skipped.
- Context lines were truncated mid-rune (CJK corruption in JSON/HTML).
- Baseline fingerprints no longer include the section heading, so renaming
  a heading keeps the baseline valid.
- `docrot coverage` no longer counts `NO_COLOR`, `TERM` and other external
  variables as undocumented API; `-h` exits 0; `--output` fails fast and
  reports write errors; IPv6 hosts with ports are skipped by `--net`.

### Heuristics tuned on eight real repositories

- Slash-separated prose (`health/ready`), stdlib import paths, host-like
  segments, placeholder names (`path/to/file`, `Type.Method`, `--flag`) and
  package lists (`errx/logx/timex`) are not references.
- Receiver variables that collide with package names (`cfg.Addr`) and
  unexported names (`db.synchronous`) are info, not errors.
- Flags after external programs (`go test -race`) are ignored; flags after
  `go run ./cmd/x` are not.
- `UPPER_SNAKE` words only count as environment variables when the line
  talks about an environment.
- Paths that exist under a sub-tree (a template's `internal` package under
  an `examples` directory) are warnings that name the sub-tree.
- Directory references and dated documents are excluded from staleness.
