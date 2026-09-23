# docrot

[繁體中文](README-zh.md) · **English**

**docrot finds where your documentation lies about your code.**

Code moves; docs rarely follow. The README still names a function that was
renamed two sprints ago, `llms.txt` points an AI agent at a file that no
longer exists, the `--config` flag in the quick-start was deleted, the anchor
in the guide is dead, and the Chinese README is three commits behind the
English one. Link checkers only look at URLs. Markdown linters only look at
formatting. Nothing checks the *claims*.

docrot extracts every claim a document makes about the repository — file
paths, Go/Odin/Python symbols, CLI flags, environment variables, config
keys, heading anchors, shell commands, Go import paths — and checks each one
against the real code. Then it uses git history to spot sections whose
referenced code has churned since the prose was last touched, and compares
bilingual document pairs for structural drift.

It is a single static binary written in Go with **no dependencies outside
the standard library**.

```text
CHANGELOG.md:91:5: error missing-symbol `httpx.Retry` not found in package httpx (did you mean httpx.ClientConfig.Retry?)
docs/llms-reference.md:148:3: warning missing-path `internal/api` not found at the repo root (exists under examples/service/)
README.md:59: warning stale-section section "🚀 快速上手" last edited 2026-08-04; since then servicex/app.go: 4 commits (latest 2026-08-12)
README-zh.md:1: warning pair-heading translation has 4 headings, source has 5

50 errors, 52 warnings, 214 info — 51 docs, 2,306 references, 1.36s [214 info hidden; --info to show]
```

Those lines are from a real run on an internal Go repository; see the
[field report](docs/field-report.md) for what docrot found across eight
repositories in three languages, and the
[Python field report](docs/field-report-python.md) for httpx, Starlette,
Typer, Pydantic and FastAPI (1,692 documents, 372 translation pairs).

## Install

```sh
go install ./cmd/docrot          # from a clone
go build -o docrot ./cmd/docrot  # or just build the binary
```

Requires Go 1.26+. `git` on `PATH` is optional; without it the git-based
rules are silently disabled.

## Quick start

```sh
cd your-repo
docrot check                    # text report, exit 1 on errors
docrot check --format html --output docrot.html
docrot explain README.md        # what did it extract, and why?
docrot coverage                 # which exported API is never documented?
docrot baseline                 # freeze today's findings; fail only on new ones
```

## What it checks

| Rule | The document says… | docrot verifies… |
|---|---|---|
| `missing-path` | `` `internal/gitx/gitx.go` ``, `[rules](docs/rules.md)` | the file or directory exists (relative to the doc or the repo root; globs allowed) |
| `missing-symbol` | `` `report.WriteSARIF` ``, `` `Resolver.Resolve()` ``, `` `render_frame()` `` | the Go symbol exists (via `go/parser`), or the Odin / Python declaration exists |
| `unknown-flag` | `` `--format` `` | some `flag.*` call defines it |
| `unknown-env` | `` `DOCROT_DEBUG` `` | the code reads it (`os.Getenv`, `os.LookupEnv`, any `*Env*` call) |
| `unknown-config-key` | `` `stale.minChurn` `` | a `json:"…"` / `yaml:"…"` / `toml:"…"` tag path or a sample config file has it |
| `broken-anchor` | `[x](docs/rules.md#exit-codes)` | the heading exists (GitHub slug rules, CJK-aware) |
| `missing-command` | `python scripts/verify.py` inside a ```` ```sh ```` block | the script / package path exists |
| `missing-import` | `import "docrot/internal/model"` inside a ```` ```go ```` block | the package directory exists in this module |
| `broken-url` | `https://…` (only with `--net`) | the URL answers 2xx/3xx |
| `stale-section` | a section last edited on 2026-06-01 | the code it references has not churned since (git) |
| `pair-*` | `README.md` ↔ `README-zh.md` | same headings, identical code blocks, same links/tables/numbers, translation not behind source (git) |
| `undocumented` | — | every exported symbol / flag / env var is mentioned somewhere (`docrot coverage`) |
| `stale-comment` | a doc comment / docstring on a symbol the docs point at | the function body has not churned in several commits since the comment was edited (git) |
| `comment-mentions-missing` | `// raw is re-read on retry` above a function | `raw` still exists in the signature, the file, or the index |

Every rule is described in [docs/rules.md](docs/rules.md), including how to
silence it.

## How it decides

1. **Tokenize** each Markdown file: headings, fenced blocks, inline code
   spans, links, images, tables, HTML comments. No CommonMark dependency.
2. **Extract** references from code spans, link targets, shell blocks and
   Go blocks. Each reference gets a *kind* and a *confidence*: a path with a
   directory and an extension is high; a bare file name is medium; a
   dotted name whose first part is an unknown lower-case word (`app.Run`) is
   low and never reported.
3. **Index** the repository once: file tree, Go packages/symbols/flags/env/
   tags (`go/parser`), Odin and Python declarations (regex), Markdown
   anchors, JSON sample keys.
4. **Resolve** each reference and produce a finding with a *did-you-mean*
   suggestion (Damerau-Levenshtein over the right candidate set, plus git
   rename history for paths).
5. **Stale**: `git blame` gives each section an edit time; `git log` counts
   commits to every referenced file after that time.
6. **Pairs**: structural fingerprints of both documents are diffed.
7. **Comments**: for every symbol a document referred to, the attached
   doc comment or docstring is checked the same way — names it cites must
   exist, and a body that churned after the comment was edited is flagged.
8. **Baseline**: fingerprints exclude line numbers, so a baseline survives
   ordinary editing.

Severity follows confidence: high → error, medium → warning, low → info.
Flags and environment variables are one step softer because they are so
often about *other* programs; glob misses, config keys and bare file names
are always info. The text report hides info unless you pass `--info`.

docrot was tuned against real repositories, not synthetic examples. Things
it deliberately ignores: paths matched by `.gitignore` (build artifacts),
prose like `health/ready` or `net/http`, flags after external programs
(`go test -race`), `UPPER_SNAKE` words on lines that never mention an
environment, `cfg.Addr` when `cfg` is both a package and a variable, and
illustrative names such as `Type.Method`, `--flag` or `path/to/file`.

Letter case is checked exactly on every platform. A document that says
`docs/foo.md` when the file is `Docs/Foo.md` gets a finding on Windows
and macOS as well, worded "differs only by letter case", because that link
works on the author's laptop and breaks on the Linux CI runner. Document
discovery itself is case-insensitive, so `README.MD` and `readme.md` are
scanned.

## Configuration

`docrot init` writes a `.docrot.json` with the defaults:

```json
{
  "docs": ["**/*.md", "llms.txt"],
  "exclude": ["vendor/**", "node_modules/**", "third_party/**", "3rdparty/**", "external/**", "**/testdata/**", "dist/**", ".git/**", ".*/**"],
  "ignore": [],
  "siblings": [],
  "pairs": [],
  "pairPatterns": ["{stem}-zh.md", "{stem}_zh.md", "{stem}.zh.md", "{stem}.zh-TW.md", "{stem}-zh-TW.md"],
  "configSamples": ["config.json", "config*.json", "*.example.json", "*.sample.json", "configs/**/*.json"],
  "stale": { "enabled": true, "minChurn": 3, "minDays": 90, "exclude": ["CHANGELOG*.md", "CHANGES*.md", "HISTORY*.md", "**/superpowers/**", "**/specs/**", "**/plans/**", "**/*-report.md", "**/adr/**"] },
  "coverage": { "report": false, "includeInternal": false },
  "severity": { "stale-section": "warning", "pair-lag": "warning", "pair-number": "info" },
  "net": false,
  "failOn": "error",
  "minConfidence": "low",
  "outDir": ".docrot",
  "maxFileMB": 8,
  "comments": { "enabled": true, "minChurn": 2, "minFrac": 0.5 }
}
```

- `ignore` holds regular expressions matched against the reference text.
- `siblings` lists other repositories (relative to the root) where a path
  missing here may legitimately live — useful when a service documents the
  library it is built on.
- `stale.exclude` keeps dated documents (changelogs, design specs) out of
  the staleness analysis; they are historical records by nature.
- `severity` overrides a rule's level, e.g. `{"stale-section": "info"}`.
- `outDir` is the directory every run rewrites; see below.
- `comments` tunes the code-comment checks that run for every symbol a
  document refers to: `minChurn` newer commits (or one commit rewriting
  `minFrac` of the body) make a comment "stale"; `docrot comments` runs
  the same checks over every exported declaration.
- `maxFileMB` caps the size of any file whose *contents* docrot reads
  (documents, Go/Odin/Python sources, JSON samples). Binaries are never
  opened at all — only their names enter the path index, so a 4 GB model
  file costs one directory entry, gitignored or not. A text file above the
  cap is skipped with a warning; paths to it still resolve.

Inline escape hatches:

```markdown
<!-- docrot:ignore -->            the next non-blank line
inline text <!-- docrot:ignore -->  this line
<!-- docrot:ignore-start --> … <!-- docrot:ignore-end -->
<!-- docrot:ignore-file -->
```

### Output directory

Every `docrot check` run rewrites one directory, named by the `outDir`
setting, so that a human and an agent always find the current report in the
same place:

```text
.docrot/.gitignore   a single "*", so the reports never reach a commit
.docrot/report.md    for agents: findings by file, how to read them, a checklist
.docrot/report.html  for humans: the filterable single-file page
.docrot/report.json  the stable JSON schema
.docrot/report.txt   the terminal report, with info findings
```

Each file is rendered into a temporary file and renamed into place, so an
interrupted run never leaves half a report where the next reader expects a
whole one. The directory is excluded from document discovery, so yesterday's
report is never checked as though it were documentation. Pass `--out-dir` to
put it somewhere else, `--no-out` to write nothing this run, or set `outDir`
to the empty string to turn it off for good.

## Commands

```text
docrot check [dir] [--format text|md|json|sarif|html] [--output FILE]
             [--fail-on error|warning|info|none] [--min-confidence low|medium|high]
             [--out-dir DIR] [--no-out]
             [--no-git] [--net] [--info] [--all] [--coverage] [--quiet] [--config FILE]
docrot baseline [dir]            write .docrot-baseline.json
docrot coverage [dir]            documentation coverage table
docrot pairs [dir]               only the bilingual checks
docrot comments [dir]            comment checks over every exported declaration
docrot explain <doc> [--kind K]  every extracted reference with its verdict
docrot index [dir] --kind symbols|flags|env|paths|anchors|config|odin|python
docrot init [dir]
docrot version
```

Flags shared by the scanning commands: `--config` picks the config file,
`--no-git` disables the git rules, `--net` checks URLs, `--verbose` prints
index and git warnings, `--min-confidence` drops weak references. `check`
adds `--format`, `--output`, `--fail-on`, `--info`, `--all`, `--coverage`,
`--quiet`, `--out-dir` and `--no-out`; `explain` adds `--kind` and `--root`;
`index` takes `--kind`.

Exit codes: `0` clean, `1` a new finding at or above `--fail-on`, `2` usage
or internal error. The SARIF output uploads directly to GitHub code
scanning; baselined findings carry `baselineState: unchanged`.

## CI

```yaml
- run: go run ./cmd/docrot check --format sarif --output docrot.sarif --fail-on error
- uses: github/codeql-action/upload-sarif@v3
  with: { sarif_file: docrot.sarif }
```

## Development

```sh
python scripts/verify.py        # gofmt, vet, test, build, self-check, fixture check, formats
python scripts/demo.py ../some-repo --out reports
```

docrot checks its own documentation as part of `scripts/verify.py`; the
dated design documents under `docs/superpowers/` are excluded there because
they are full of illustrative paths by design.

Design: [docs/superpowers/specs/2026-09-23-docrot-design.md](docs/superpowers/specs/2026-09-23-docrot-design.md).
Plan: [docs/superpowers/plans/2026-09-23-docrot-plan.md](docs/superpowers/plans/2026-09-23-docrot-plan.md).
Rules: [docs/rules.md](docs/rules.md). Field reports: [docs/field-report.md](docs/field-report.md), [docs/field-report-python.md](docs/field-report-python.md).
Agent entry point: [llms.txt](llms.txt). Changes: [CHANGELOG.md](CHANGELOG.md).
What it does not check yet: [docs/roadmap.md](docs/roadmap.md).

## Non-goals

- Not a Markdown linter. Formatting is none of docrot's business.
- Not a CommonMark implementation. It recognises exactly what it needs.
- Does not execute examples, and does not judge translation quality.
- Does not rewrite documents (yet).

## License

MIT — see [LICENSE](LICENSE).
