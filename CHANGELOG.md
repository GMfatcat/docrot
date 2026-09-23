# Changelog

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
