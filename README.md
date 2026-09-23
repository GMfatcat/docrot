# docrot

[繁體中文](README-zh.md) · **English**

**docrot finds where your documentation lies about your code.**

Code moves; docs rarely follow. The README still names a function that was
renamed two sprints ago, `llms.txt` points an AI agent at a file that no
longer exists, the `--config` flag in the quick-start was deleted, the anchor
in the guide is dead, and the Chinese README is three commits behind the
English one. Link checkers only look at URLs. Markdown linters only look at
formatting. Nothing checks the *claims*.

docrot extracts every claim a document makes about the repository and checks
it against the real code, then uses git history to find the sections the
code has moved on from, and compares bilingual document pairs. It reads
Markdown, reStructuredText and AsciiDoc. It is a single static Go binary
with **no dependencies outside the standard library**, and the HTML report
it writes is one file that opens offline.

![docrot check on the fixture repository](docs/assets/terminal.svg)

![The HTML report: findings with filters by severity, rule and file](docs/assets/report.png)

Both pictures come from the seeded test repository under `testdata/fixture`.
For real runs — eight Go and Odin repositories, seven Python projects,
FastAPI's 1,692 documents and Django's 686 Sphinx pages among them — see the
[field reports](docs/field-report.md).

## 🚀 Install

```sh
go install ./cmd/docrot          # from a clone
go build -o docrot ./cmd/docrot  # or just build the binary
```

Requires Go 1.26+. `git` on `PATH` is optional; without it the git-based
rules are silently disabled.

## ⚡ Quick start

```sh
cd your-repo
docrot check                    # text report, exit 1 on errors; .docrot/ gets html, md, json and txt
docrot check --changed          # only the documents you touched (pre-commit speed)
docrot explain README.md        # what did it extract, and why?
docrot coverage                 # which exported API is never documented?
docrot baseline                 # freeze today's findings; fail only on new ones
```

## 🔍 What it checks

- 📁 **Paths, symbols, imports** — `` `internal/gitx/gitx.go` ``,
  `` `report.WriteSARIF` ``, `` `render_frame()` `` and
  `import "docrot/internal/model"` exist (Go through `go/parser`, Odin and
  Python through a declaration index), with did-you-mean suggestions and
  git rename history.
- 🎛️ **Flags, environment variables, config keys, defaults** — `--format`
  is defined, `DOCROT_DEBUG` is read, `stale.minChurn` is a struct tag or a
  sample key, a ```` ```json ```` config example has no dropped key, and
  "`--port` defaults to `8080`" says what the code says.
- 🌐 **HTTP routes** — `GET /v1/items` is registered by a handler
  (net/http, chi, gin, echo, FastAPI, Flask, Starlette, Django).
- 🔗 **Anchors, commands, install lines, toolchain, targets** —
  `[x](docs/rules.md#exit-codes)` resolves, `python scripts/verify.py`
  exists, `go get` / `pip install` name this project correctly, "requires
  Go 1.21" agrees with `go.mod`, `make lint` is a target. <!-- docrot:ignore toolchain-mismatch -->
- ⏳ **Staleness (git)** — a section whose referenced files or declarations
  kept changing after the section was last edited.
- 🌏 **Bilingual pairs** — `README.md` ↔ `README-zh.md` keep the same
  headings, code blocks, links, tables and numbers, and the translation is
  not behind the source.
- 💬 **Code comments** — the doc comment of a documented symbol names
  things that still exist, and its body has not moved on since the comment
  was written.
- 📊 **Coverage** — exported symbols, flags and environment variables that
  no document mentions.

Every rule, how it decides and how to silence it: [docs/rules.md](docs/rules.md).

## 🧠 How it decides

Tokenize each document, extract references with a *kind* and a
*confidence*, index the repository once (files, Go/Odin/Python
declarations, routes, string literals, manifests, anchors), resolve each
reference, blame and log for staleness, diff pair fingerprints, check
comments, apply the baseline. Severity follows confidence: high → error,
medium → warning, low → info; the text report hides info unless you pass
`--info`. The heuristics were tuned on real repositories; what they
deliberately ignore is written down in
[docs/how-it-works.md](docs/how-it-works.md).

## ⚙️ Configuration

`docrot init` writes a `.docrot.json` with the defaults. The keys you will
actually touch: `docs` and `exclude` (what to scan), `ignore` (regular
expressions over reference text), `siblings` (other repositories where a
path may live), `stale.exclude` (changelogs and other historical
documents), `severity` and `outDir`. To silence one false positive where it
happens:

```markdown
<!-- docrot:ignore -->            the next non-blank line
inline text <!-- docrot:ignore -->  this line
<!-- docrot:ignore-start --> … <!-- docrot:ignore-end -->
<!-- docrot:ignore-file -->
<!-- docrot:ignore missing-path unknown-flag -->   only these rules (also with ignore-start)
```

Every key with its default, the output directory and the git cache:
[docs/configuration.md](docs/configuration.md).

## 🖥️ Commands

```text
docrot check [dir] [--format text|md|json|sarif|html] [--changed] [--since REF] [--fail-on LEVEL]
docrot explain <doc>             every extracted reference with its verdict
docrot baseline | coverage | pairs | comments | index | init | version
```

Every flag, the exit codes and the CI recipes: [docs/commands.md](docs/commands.md).

## 🤖 CI

```yaml
- run: go run ./cmd/docrot check --format sarif --output docrot.sarif --fail-on error
- run: go run ./cmd/docrot check --changed --since origin/main --fail-on warning   # PR: changed docs only
- uses: github/codeql-action/upload-sarif@v3
  with: { sarif_file: docrot.sarif }
```

## 🛠️ Development

```sh
python scripts/verify.py        # gofmt, vet, test, build, self-check, fixture check, formats
python scripts/demo.py ../some-repo --out reports
python scripts/screenshots.py   # regenerate docs/assets/ from the fixture
```

docrot checks its own documentation as part of `scripts/verify.py`; the
dated design documents under `docs/superpowers/` are excluded there because
they are full of illustrative paths by design.

## 📚 Documentation

- [Rules](docs/rules.md) — every rule, how it decides, how to silence it
- [How it works](docs/how-it-works.md) — the pipeline, confidence and severity, what is deliberately ignored
- [Configuration](docs/configuration.md) — `.docrot.json`, the output directory, the git cache
- [Commands](docs/commands.md) — flags, exit codes, CI
- [Field report](docs/field-report.md) (Go and Odin) and [Python field report](docs/field-report-python.md) — what it found on real repositories, and what was noise
- [Changelog](CHANGELOG.md) · [Roadmap](docs/roadmap.md) · [Design spec](docs/superpowers/specs/2026-09-23-docrot-design.md) · [Plan](docs/superpowers/plans/2026-09-23-docrot-plan.md) · [llms.txt](llms.txt) for agents

## 🚫 Non-goals

- Not a Markdown linter. Formatting is none of docrot's business.
- Not a CommonMark implementation. It recognises exactly what it needs.
- Does not execute examples, and does not judge translation quality.
- Does not rewrite documents (yet).

## 📄 License

MIT — see [LICENSE](LICENSE).
