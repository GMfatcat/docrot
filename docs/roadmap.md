<!-- docrot:ignore-file -->
<!-- Every path, flag and package named below is something that does NOT exist
     yet; that is the point of a roadmap, so docrot skips this file. -->
# Roadmap: what docrot does not check yet

A full review of the pipeline after 0.2.0, updated as 0.3.0, 0.4.0 and 0.9.0
shipped the first two tiers, listing claims documents make that docrot still
takes on faith. Ordered by the ratio of "how often this
rots in real repositories" to "how much code it takes". Nothing here is
started; each item names the stage it would live in.

## Done in 0.8.0: C and C++

The fourth plugin language; see `CHANGELOG.md` and
`docs/field-report-c.md`. The four languages the user asked for (Rust,
JavaScript/TypeScript, C#, C/C++) all ship through `internal/index/lang`;
adding another is one package plus one row in `model.Langs`. Lessons from
C: a repository's documentation describes its build system and its helper
scripts as much as its code, so configure flags, CMake options and the
options of a test runner written in Perl are never this program's flags.

## Done in 0.7.0: C#

The third plugin language; see `CHANGELOG.md` and
`docs/field-report-csharp.md`. Lessons: a C# document names base-class-library
types on every page, so a `Type.Member` whose type the repository does not
declare is never a claim; NuGet package ids look like namespaces.

## Done in 0.6.0: JavaScript and TypeScript

The second plugin language; see `CHANGELOG.md` and
`docs/field-report-js.md`. Lessons: documents name the instance after the
module (`reply.send`), the document's own imports say which names belong
to other packages, and a class that `extends` another must not be held to
the members it declares.

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

## Done in 0.9.0 (was tier 2)

GitHub-annotation and JUnit output, `pair-missing`/`pair-orphan`,
`example-syntax`, route and config-key coverage and `docrot fix` all
shipped in 0.9.0; see `CHANGELOG.md`. Lessons: a Go snippet in a README
takes eight shapes (file, statements, statements then declarations,
switch cases, literal elements, struct fields, interface methods, go.mod
content that is not an example at all) and a block cut before its
closing brace is an excerpt far more often than a mistake, so it is only
info; a translation *set* exists only where a directory convention
defines one; a route mentioned without a method documents every method;
and the only suggestions safe to apply unattended are the ones the
resolver could prove — git renames and letter case — everything else
stays a suggestion. Left over: gofmt drift of Go examples (the parser
already has the AST; a formatting diff is a policy, not a lie) and
`fix` for anchors, which would need a unique heading match to be safe.

## Tier 3 — possible, not obviously worth it yet

- Executing examples (`go run`, doctests) — a different product; docrot stays static.
- Odin `#load("…")` paths, `-define:` flags, `core:` imports — the Odin corpus here is two repositories; wait for demand.
- Jupyter notebooks as documents — markdown cells are trivial, code cells are not.
- Watch mode / `docrot serve` dashboard — the output directory plus a file watcher covers most of it.
- Cross-repository symbol resolution with the sibling's *types* (`App.Run` for a sibling's `servicex.App`) — needs the sibling's index; feasible, slow. The cheap form — a sibling's package of the same name exporting the missing name — shipped in 1.1.0.
- Detecting *missing* documentation of behaviour (a new flag with no README line) is `coverage`; detecting missing documentation of *changes* (a CHANGELOG entry per exported change) is a policy, not a fact — out.

## Explicitly rejected

- Grammar, style or link *formatting* — there are linters for that.
- Translation quality — structure only.
- Anything that needs a network by default — `--net` stays opt-in.
