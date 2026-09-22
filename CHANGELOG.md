# Changelog

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
