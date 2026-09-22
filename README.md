# docrot

[繁體中文](README-zh.md) · **English**

**docrot finds where your documentation lies about your code.**

Code moves; docs rarely follow. The README still names a function that was
renamed two sprints ago, `llms.txt` points an AI agent at a file that no
longer exists, the `--flag` in the quick-start was deleted, the anchor in
the guide is dead, and the Chinese README is three commits behind the
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
README.md:42:15: error missing-symbol `httpx.WriteJSON` not found in package httpx (did you mean httpx.WriteData?)
llms.txt:12:3: warning missing-path `docs/contract.md` not found (did you mean docs/contracts.md?)
README-zh.md: warning pair-lag 4 commits to README.md since README-zh.md last changed (a1b2c3d "rename WriteJSON", …)

3 errors, 2 warnings, 5 info (12 baselined) — 14 docs, 1,204 references, 0.83s
```

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
| `missing-path` | `` `internal/httpx/server.go` ``, `[guide](docs/guide.md)` | the file or directory exists (relative to the doc or the repo root; globs allowed) |
| `missing-symbol` | `` `httpx.WriteData` ``, `` `Server.Addr()` ``, `` `render_frame` `` | the Go symbol exists (via `go/parser`), or the Odin / Python declaration exists |
| `unknown-flag` | `` `--config` `` | some `flag.*` call defines it |
| `unknown-env` | `` `APP_TOKEN` `` | the code reads it (`os.Getenv`, `os.LookupEnv`, any `*Env*` call) |
| `unknown-config-key` | `` `server.addr` `` | a `json:"…"` / `yaml:"…"` / `toml:"…"` tag path or a sample config file has it |
| `broken-anchor` | `[x](docs/guide.md#setup)` | the heading exists (GitHub slug rules, CJK-aware) |
| `missing-command` | `./scripts/verify.ps1` inside a ```` ```sh ```` block | the script / package path exists |
| `missing-import` | `import "example.com/mod/pkg"` inside a ```` ```go ```` block | the package directory exists in this module |
| `broken-url` | `https://…` (only with `--net`) | the URL answers 2xx/3xx |
| `stale-section` | a section last edited on 2026-06-01 | the code it references has not churned since (git) |
| `pair-*` | `README.md` ↔ `README-zh.md` | same headings, identical code blocks, same links/tables/numbers, translation not behind source (git) |
| `undocumented` | — | every exported symbol / flag / env var is mentioned somewhere (`docrot coverage`) |

Every rule is described in [docs/rules.md](docs/rules.md), including how to
silence it.

## How it decides

1. **Tokenize** each Markdown file: headings, fenced blocks, inline code
   spans, links, images, tables, HTML comments. No CommonMark dependency.
2. **Extract** references from code spans, link targets, shell blocks and
   Go blocks. Each reference gets a *kind* and a *confidence*:
   `internal/x/y.go` is a high-confidence path; `main.go` is a medium one;
   `app.Run(ctx)` is a low-confidence symbol that is never reported as an
   error.
3. **Index** the repository once: file tree, Go packages/symbols/flags/env/
   tags (`go/parser`), Odin and Python declarations (regex), Markdown
   anchors, JSON sample keys.
4. **Resolve** each reference and produce a finding with a *did-you-mean*
   suggestion (Damerau-Levenshtein over the right candidate set, plus git
   rename history for paths).
5. **Stale**: `git blame` gives each section an edit time; `git log` counts
   commits to every referenced file after that time.
6. **Pairs**: structural fingerprints of both documents are diffed.
7. **Baseline**: fingerprints exclude line numbers, so a baseline survives
   ordinary editing.

Severity follows confidence: high → error, medium → warning, low → info.
Flags and environment variables are one step softer because they are so
often about *other* programs.

## Configuration

`docrot init` writes a `.docrot.json` with the defaults:

```json
{
  "docs": ["**/*.md", "llms.txt"],
  "exclude": ["vendor/**", "node_modules/**", "**/testdata/**", "dist/**", ".git/**"],
  "ignore": [],
  "pairs": [],
  "pairPatterns": ["{stem}-zh.md", "{stem}_zh.md", "{stem}.zh.md", "{stem}.zh-TW.md", "{stem}-zh-TW.md"],
  "configSamples": ["config.json", "config*.json", "*.example.json", "*.sample.json", "configs/**/*.json"],
  "stale": { "enabled": true, "minChurn": 3, "minDays": 90 },
  "coverage": { "report": false, "includeInternal": false },
  "severity": {},
  "net": false,
  "failOn": "error",
  "minConfidence": "low"
}
```

`ignore` holds regular expressions matched against the reference text.
`severity` overrides a rule's level, e.g. `{"stale-section": "info"}`.

Inline escape hatches:

```markdown
<!-- docrot:ignore -->            the next non-blank line
inline text <!-- docrot:ignore -->  this line
<!-- docrot:ignore-start --> … <!-- docrot:ignore-end -->
<!-- docrot:ignore-file -->
```

## Commands

```text
docrot check [dir] [--format text|json|sarif|html] [--output FILE]
             [--fail-on error|warning|info|none] [--min-confidence low|medium|high]
             [--no-git] [--net] [--all] [--coverage] [--quiet] [--config FILE]
docrot baseline [dir]            write .docrot-baseline.json
docrot coverage [dir]            documentation coverage table
docrot pairs [dir]               only the bilingual checks
docrot explain <doc> [--kind K]  every extracted reference with its verdict
docrot index [dir] --kind symbols|flags|env|paths|anchors|config|odin|python
docrot init [dir]
docrot version
```

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
python scripts/verify.py        # gofmt, vet, test, build, self-check, fixture check
python scripts/demo.py ../some-repo --out reports
```

Design: [docs/superpowers/specs/2026-09-23-docrot-design.md](docs/superpowers/specs/2026-09-23-docrot-design.md).
Plan: [docs/superpowers/plans/2026-09-23-docrot-plan.md](docs/superpowers/plans/2026-09-23-docrot-plan.md).
Rules: [docs/rules.md](docs/rules.md). Changes: [CHANGELOG.md](CHANGELOG.md).

## Non-goals

- Not a Markdown linter. Formatting is none of docrot's business.
- Not a CommonMark implementation. It recognises exactly what it needs.
- Does not execute examples, and does not judge translation quality.
- Does not rewrite documents (yet).

## License

MIT — see [LICENSE](LICENSE).
