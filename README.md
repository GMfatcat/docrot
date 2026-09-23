# docrot

[繁體中文](README-zh.md) · **English**

**docrot finds where your documentation lies about your code.**

- Does your README still name the function you renamed last sprint?
- Does `llms.txt` send your AI agent to a file that no longer exists?
- Is the `--config` flag in the quick start still there?
- Do the anchors in the guide still land? Is the Chinese README three commits behind?

Nobody knows — link checkers look at URLs, Markdown linters look at
formatting, nothing checks the *claims*.

docrot does:

- 📌 pulls every claim a document makes about the repository (paths, symbols, flags, environment variables, config keys, routes, defaults, install lines) and checks it against the real code
- 🧪 parses every Go example, so a snippet with a missing brace is caught before a reader copies it
- ⏳ uses git history to find the sections the code has moved on from
- 🌏 keeps bilingual document pairs honest, and rewrites the mechanical mistakes itself (`docrot fix`: git renames, letter case)
- 📦 reads Markdown, reStructuredText and AsciiDoc about Go, Python, TypeScript/JavaScript, Rust, C#, C/C++ and Odin code, in any mix; one static Go binary, **standard library only**; an HTML report that opens offline

![docrot check on the fixture repository](docs/assets/terminal.svg)

Both pictures come from the seeded test repository under `testdata/fixture`.
For real runs — eight Go and Odin repositories, seven Python projects,
three Rust crates, three JavaScript/TypeScript projects, three C# projects,
three C/C++ projects, FastAPI's 1,692 documents, Django's 686 Sphinx pages
and curl's 928 man pages among them — see the
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
docrot fix --apply              # rewrite paths git renamed or that differ only by letter case
```

## 🔍 What it checks

- 📁 **Paths, symbols, imports** — `` `internal/gitx/gitx.go` ``,
  `` `report.WriteSARIF` ``, `` `render_frame()` `` and
  `import "docrot/internal/model"` exist (Go through `go/parser`; Odin,
  Python, Rust, JavaScript/TypeScript, C# and C/C++ through a declaration
  index that follows re-exports), with did-you-mean suggestions and git
  rename history.
- 🎛️ **Flags, environment variables, config keys, defaults** — `--format`
  is defined, `DOCROT_DEBUG` is read, `stale.minChurn` is a struct tag or a
  sample key, a ```` ```json ```` config example has no dropped key, and
  "`--port` defaults to `8080`" says what the code says.
- 🌐 **HTTP routes** — `GET /v1/items` is registered by a handler
  (net/http, chi, gin, echo, FastAPI, Flask, Starlette, Django, axum,
  actix-web, rocket, express, fastify, hono, NestJS, ASP.NET Core).
- 🔗 **Anchors, commands, install lines, toolchain, targets** —
  `[x](docs/rules.md#exit-codes)` resolves, `python scripts/verify.py`
  exists, `#include "x.h"` is in the tree, `go get` / `pip install` /
  `cargo add` / `npm install` / `dotnet add package` name this project
  correctly, `import { x } from 'pkg/sub'` names a real sub-path,
  "requires Go 1.21" agrees with `go.mod`, `make lint` is a target. <!-- docrot:ignore toolchain-mismatch -->
- 🧪 **Go examples** — every ```` ```go ```` block parses, as a file, a
  snippet, a struct or interface body; an excerpt cut before its closing
  brace is only info.
- ⏳ **Staleness (git)** — a section whose referenced files or declarations
  kept changing after the section was last edited.
- 🌏 **Bilingual pairs** — `README.md` ↔ `README-zh.md` keep the same
  headings, code blocks, links, tables and numbers, the translation is
  not behind the source, and a `docs/en/` ↔ `docs/zh/` tree has no <!-- docrot:ignore missing-path -->
  untranslated or orphaned page.
- 💬 **Code comments** — the doc comment of a documented symbol names
  things that still exist, and its body has not moved on since the comment
  was written.
- 📊 **Coverage** — exported symbols, flags, environment variables, routes
  and config keys that no document mentions.

Every rule, how it decides and how to silence it: [docs/rules.md](docs/rules.md).

## 🧠 How it decides

Tokenize each document, extract references with a *kind* and a
*confidence*, index the repository once (files, Go/Odin/Python/Rust/JS/C#/C++
declarations, routes, string literals, manifests, anchors), resolve each
reference, parse the Go examples, blame and log for staleness, diff pair
fingerprints, check comments, apply the baseline. Severity follows confidence: high → error,
medium → warning, low → info; the text report hides info unless you pass
`--info`. The heuristics were tuned on real repositories; what they
deliberately ignore is written down in
[docs/how-it-works.md](docs/how-it-works.md).

## ⚙️ Configuration

`docrot init` writes a `.docrot.json` with the defaults. The keys you will
actually touch: `docs` and `exclude` (what to scan), `ignore` (regular
expressions over reference text), `siblings` (other repositories where a
path may live), `configSamples` (the JSON files mined for configuration
keys; `appsettings*.json` is among the defaults), `stale.exclude`
(changelogs and other historical documents), `severity` and `outDir`. To
silence one false positive where it happens:

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
docrot index [dir] --kind K      what was indexed: symbols, flags, env, routes, targets… or one language (rust, js, csharp, c)
docrot fix [dir] [--apply]       rewrite paths git renamed or that differ only by letter case (dry run by default)
docrot baseline | coverage | pairs | comments | init | version
```

Every `check` writes the HTML, Markdown, JSON and text reports into `.docrot/`; the HTML one looks like this:

![The HTML report: findings with filters by severity, rule and file](docs/assets/report.png)

Every flag, the exit codes and the CI recipes: [docs/commands.md](docs/commands.md).

## 🤖 CI

```yaml
- run: go run ./cmd/docrot check --format sarif --output docrot.sarif --fail-on error
- run: go run ./cmd/docrot check --changed --since origin/main --fail-on warning   # PR: changed docs only
- run: go run ./cmd/docrot check --format github --fail-on warning                # annotations on the PR, no SARIF upload needed
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
- [Field report](docs/field-report.md) (Go and Odin), [Python](docs/field-report-python.md), [Rust](docs/field-report-rust.md), [JavaScript/TypeScript](docs/field-report-js.md), [C#](docs/field-report-csharp.md), [C/C++](docs/field-report-c.md) and [Go examples](docs/field-report-examples.md) field reports — what it found on real repositories, and what was noise
- [Changelog](CHANGELOG.md) · [Roadmap](docs/roadmap.md) · [Design spec](docs/superpowers/specs/2026-09-23-docrot-design.md) · [Plan](docs/superpowers/plans/2026-09-23-docrot-plan.md) · [llms.txt](llms.txt) for agents

## 🚫 Non-goals

- Not a Markdown linter. Formatting is none of docrot's business.
- Not a CommonMark implementation. It recognises exactly what it needs.
- Does not execute examples, and does not judge translation quality.
- Does not rewrite prose. `docrot fix` touches only paths that git renamed or that differ by letter case.

## 📄 License

MIT — see [LICENSE](LICENSE).
