# Changelog

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
