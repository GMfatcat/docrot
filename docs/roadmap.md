<!-- docrot:ignore-file -->
<!-- Every path, flag and package named below is something that does NOT exist
     yet; that is the point of a roadmap, so docrot skips this file. -->
# Roadmap: what docrot does not check yet

A full review of the pipeline after 0.2.0, updated after 0.3.0 and 0.4.0
shipped the first two tiers, listing claims documents make that docrot still
takes on faith. Ordered by the ratio of "how often this
rots in real repositories" to "how much code it takes". Nothing here is
started; each item names the stage it would live in.

## Done in 0.5.0: language plugins and Rust

Every non-Go language is now one index package behind `internal/index/lang`
plus a row in `model.Langs`; Rust shipped through it. JavaScript/TypeScript,
C# and C/C++ follow the same path, in that order; the design is
`docs/superpowers/specs/2026-09-23-docrot-languages-design.md`. Lessons
from Rust: re-exports are the API (a facade crate's `pub use other::*`
must be followed), a document's own `use` lines say which names belong to
other crates, and changelogs are where removed APIs live.

## Done in 0.4.0 (from tier 2)

reStructuredText and AsciiDoc input, the persistent git cache,
`stale-symbol` and `default-mismatch` shipped in 0.4.0; see `CHANGELOG.md`.
Lessons: Sphinx documentation sets are global (labels resolve across pages,
`/paths` are relative to a source root that is not the repository root,
intersphinx owns labels the tree does not define); a name missing from a
module that exists deserves a warning with its new home, not a rescue by any
same-named object; documented defaults are rarer in the wild than expected
(none of the corpus repositories states one that the code contradicts), so
the rule stays narrow — one subject per line, literal-looking values only.

## Done in 0.3.0 (was tier 1)

`missing-route`, the string-literal index, JSON configuration examples,
install lines vs module identity, toolchain claims, make/npm/just/task
targets, `--changed [--since REF]`, rule-scoped ignores and HTML anchors
all shipped in 0.3.0; see `CHANGELOG.md`. What the field runs taught: a
bare `/path` span is usually a mention, not a claim (info); version
sentences must carry a requirement word and not be negated, and changelogs
are history; a README inside a package with its own Makefile means that
Makefile; route, target and toolchain claims must not feed the staleness
analysis (handlers, Makefiles and go.mod churn for unrelated reasons).

Left over from that tier: Python `import x` lines in code fences against
the repository's top-level packages (low value: a renamed package rarely
keeps the old import in its own docs), and nested runner files (a
monorepo's `packages/x/Makefile`) — today such documents are skipped
rather than checked.

## Tier 2 — real value, more code or more judgement

| Candidate | The lie it catches | Where | Cost |
|---|---|---|---|
| **Go example blocks must parse** — run `go/parser` on ```` ```go ```` fences (with an implicit `package main` wrapper when needed) and report syntax errors; optionally `gofmt` drift | Copy-paste examples with a missing brace or an old syntax. Stdlib-only and cheap for Go; Python would need a real parser (out of scope). | `extract` or a new `examples` stage | small for Go |
| **Untranslated pages** (`pair-missing`) — in a `docs/en` ↔ `docs/zh` tree, list source pages with no counterpart, and translations whose source disappeared (`pair-orphan`) | FastAPI has both kinds; translation teams track this by hand. | `pairs` | small |
| **Coverage for routes and config keys** — once routes and string literals are indexed, "documented / total" for endpoints and for config keys, next to symbols/flags/env | The `llms.txt` audience: an agent needs the endpoint list and the config keys more than the exported Go surface. | `coverage` | small after Tier 1 |
| **`docrot fix --dry-run`** — apply the high-confidence suggestions: git-recorded renames (`old/path.go → new/path.go`) and case-only mismatches; print a diff, `--apply` to write | The mechanical half of the fixes; the rename map is already computed. Symbol renames stay manual. | new `fix` stage over `Finding.Suggestion` + `Data["candidates"]` | medium |
| **GitHub/Gitea annotation output** (`--format github`, `::warning file=…,line=…::msg`) and JUnit XML | CI surfaces findings inline on the PR without SARIF upload; Gitea Actions understands the same syntax. | `report` | tiny |

## Tier 3 — possible, not obviously worth it yet

- Executing examples (`go run`, doctests) — a different product; docrot stays static.
- Odin `#load("…")` paths, `-define:` flags, `core:` imports — the Odin corpus here is two repositories; wait for demand.
- Jupyter notebooks as documents — markdown cells are trivial, code cells are not.
- Watch mode / `docrot serve` dashboard — the output directory plus a file watcher covers most of it.
- Cross-repository symbol resolution (`siblings` for symbols, not only paths) — needs the sibling's index; feasible, slow.
- Detecting *missing* documentation of behaviour (a new flag with no README line) is `coverage`; detecting missing documentation of *changes* (a CHANGELOG entry per exported change) is a policy, not a fact — out.

## Explicitly rejected

- Grammar, style or link *formatting* — there are linters for that.
- Translation quality — structure only.
- Anything that needs a network by default — `--net` stays opt-in.
