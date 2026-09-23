# How docrot decides

[繁體中文](how-it-works-zh.md) · **English**

## The pipeline

1. **Tokenize** each document: headings, fenced blocks, inline code
   spans, links, images, tables, comments. Markdown, reStructuredText
   (Sphinx roles, directives, toctrees, labels; `.txt` sources too) and
   AsciiDoc are all read into the same shape. No CommonMark dependency.
2. **Extract** references from code spans, link targets, shell blocks, Go
   blocks, C/C++ blocks (their `#include` lines) and JavaScript blocks
   (whose imports also say which names belong to this package and which
   to others). Each reference gets a *kind* and a *confidence*: a path with a
   directory and an extension is high; a bare file name is medium; a
   dotted name whose first part is an unknown lower-case word (`app.Run`) is
   low and never reported.
3. **Index** the repository once: file tree, Go packages/symbols/flags/env/
   tags (`go/parser`), Odin, Python, Rust, JavaScript/TypeScript, C# and
   C/C++ declarations (line-level patterns), HTTP route registrations, Markdown
   anchors, JSON sample keys, every identifier-like string literal, and the
   manifests (`go.mod`, `pyproject.toml`, `package.json`, `Cargo.toml`,
   `*.csproj`, `CMakeLists.txt`, Makefile, justfile, Taskfile). Every language except Go goes through one interface
   (`internal/index/lang`) and one row in `model.Langs`: the extractor's
   naming rules, the resolver and the CLI range over that table.
4. **Resolve** each reference and produce a finding with a *did-you-mean*
   suggestion (Damerau-Levenshtein over the right candidate set, plus git
   rename history for paths).
5. **Stale**: `git blame` gives each section an edit time; `git log` counts
   commits to every referenced file after that time (the document's own
   translation excepted: that is the pair check's job), and the blame of each
   referenced declaration says whether *its* body moved on. Blame and log
   answers are cached in the output directory between runs.
6. **Pairs**: structural fingerprints of both documents are diffed.
7. **Comments**: for every symbol a document referred to, the attached
   doc comment or docstring is checked the same way — names it cites must
   exist, and a body that churned after the comment was edited is flagged.
8. **Baseline**: fingerprints exclude line numbers, so a baseline survives
   ordinary editing.

## Confidence and severity

Severity follows confidence: high → error, medium → warning, low → info.
Flags and environment variables are one step softer because they are so
often about *other* programs; glob misses, config keys in prose and bare
file names are always info. The text report hides info unless you pass
`--info`; the JSON, SARIF, HTML and Markdown reports always include it.
`docrot explain <doc>` shows the confidence given to every reference.

## What is deliberately ignored

docrot was tuned against real repositories, not synthetic examples. Things
it deliberately ignores: paths matched by `.gitignore` (build artifacts),
prose like `health/ready` or `net/http`, flags after external programs
(`go test -race`), `UPPER_SNAKE` words on lines that never mention an
environment, `cfg.Addr` when `cfg` is both a package and a variable, and
illustrative names such as `Type.Method`, `--flag` or `path/to/file`. As a
last resort, anything the code spells as a string literal — `"request_id"`,
`"X-Request-ID"`, `"/openapi.json"` — counts as existing, which is what
keeps log fields, header names and routes registered through constants
from being reported.

Documents that are history by nature — changelogs, release notes, research
notes, dated specs and plans (`stale.exclude`) — are not judged for
staleness or for toolchain requirements; what they say about removed
symbols is still checked, because it is still a document.

## Letter case

Letter case is checked exactly on every platform. A document that says
`docs/foo.md` when the file is `Docs/Foo.md` gets a finding on Windows
and macOS as well, worded "differs only by letter case", because that link
works on the author's laptop and breaks on the Linux CI runner. Document
discovery itself is case-insensitive, so `README.MD` and `readme.md` are
scanned.

## Where the heuristics came from

The six field reports record every round of tuning: what the first run
reported on eight Go and Odin repositories, seven Python projects, three
Rust crates, three JavaScript/TypeScript projects, three C# projects and
three C/C++ projects, which findings were real, which were noise, and the
rule that removed each class of noise. See [field-report.md](field-report.md),
[field-report-python.md](field-report-python.md),
[field-report-rust.md](field-report-rust.md),
[field-report-js.md](field-report-js.md),
[field-report-csharp.md](field-report-csharp.md) and
[field-report-c.md](field-report-c.md); every rule's exact behaviour is
in [rules.md](rules.md).
