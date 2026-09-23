# docrot rules

Every finding carries a rule id. This page explains what each rule means,
how docrot decided, and how to make the finding go away.

Severity for the `missing-*` / `unknown-*` / `broken-*` rules follows the
extractor's confidence: **high → error**, **medium → warning**, **low → info**.
Confidence depends on *how unambiguous the text is*, not on how sure docrot is
that the thing is missing. `docrot explain <doc>` shows the confidence assigned
to every extracted reference. The text report hides info-level findings unless
you pass `--info`; JSON, SARIF and HTML always include them.

To silence a single false positive, add an HTML comment:

```markdown
<!-- docrot:ignore -->
The next non-blank line is ignored.

Inline: `legacy/thing.go` <!-- docrot:ignore -->

<!-- docrot:ignore-start -->
Everything here is ignored.
<!-- docrot:ignore-end -->
```

`<!-- docrot:ignore-file -->` anywhere in a file skips the whole file. For
patterns, add regular expressions to `ignore` in `.docrot.json`; they are
matched against the reference text. To change a rule's level, set
`severity` in `.docrot.json`, e.g. `{"stale-section": "info"}`.

## Reference rules

| Rule | Kind | Meaning |
|---|---|---|
| `missing-path` | path | A file or directory mentioned in the document does not exist. docrot tries the path relative to the document, relative to the repo root, as a glob, and under every configured sibling repo. Suggestions come from same-name files elsewhere, case differences, small typos, and git rename history. A path that exists under a sub-tree (`internal/api` → `examples/service/internal/api`) is reported as a warning that names the sub-tree. | <!-- docrot:ignore -->
| `missing-symbol` | gosym / odinsym / pysym | A code symbol mentioned in the document does not exist. Go symbols are resolved with `go/parser`: `pkg.Name`, `pkg.Type.Method`, `Type.Method`, `Name()`. Odin and Python symbols are resolved from a lightweight declaration index. |
| `unknown-flag` | flag | `--name` / `-name` in the document is not defined by any `flag.*` call. `-` and `_` are treated as equivalent. High confidence is a warning, medium is info; single-dash flags inside a longer command are never reported. Flags after an external program (`go test -race`, `git log --oneline`) are ignored, except after `go run ./cmd/x`. |
| `unknown-env` | env | An `UPPER_SNAKE` name is never read via `os.Getenv`, `os.LookupEnv`, or any call whose name contains `Env`. Only reported when the line mentions an environment (env, export, `$`, 環境…), when the code reads at least one variable, and when the name is not a well-known external one (`GOPATH`, `GIT_*`, `HOME`…). |
| `unknown-config-key` | configkey | A dotted key such as `server.addr` appears neither as a `json:"…"` / `yaml:"…"` / `toml:"…"` tag path in any struct nor in any sample config file (`config*.json`, `*.example.json`, …). Only reported when the top-level segment is a known section. Always info. |
| `broken-anchor` | anchor | A link such as `[x](docs/rules.md#exit-codes)` or `[x](#exit-codes)` points to a heading that does not exist. Slugs follow GitHub rules (underscores kept), including CJK headings and `-1` suffixes for duplicates. MkDocs custom ids (`## Title { #id }`, `[](){#id}`) count as anchors, and a page containing a mkdocstrings `::: module` directive accepts any anchor. Line anchors (`#L10-L20`) are ignored. |
| `broken-url` | url | Only with `--net`: an external URL returned 4xx/5xx or failed to connect. Local, private and `example.*` hosts are skipped. |
| `missing-command` | command | In a shell code block, the script or path a command refers to (`./scripts/verify.py`, `go run ./cmd/docrot`, `python scripts/demo.py`, `odin build dir`) does not exist. Output arguments (`-o dist/app`, `> out.txt`, `cp`/`mv` destinations) are never checked. In plain ```` ```text ```` blocks only lines with a shell prompt (`$ cmd`) count. |
| `missing-import` | import | In a Go code block, an import path under this module's path does not correspond to a package directory. Imports outside the module (stdlib, third-party) are ignored. |

Letter case is compared exactly on every platform: `docs/foo.md` is not
`Docs/Foo.md`, even on Windows, and the finding says "differs only by
letter case". For Python, module and package names are valid symbols,
names imported at the top of a module count as that module's names (so
`fastapi.status` resolves), dotted names starting with a standard-library
module (`typing.Annotated`) or an example object (`app.routes`, `client.get`)
are skipped, and symbols defined under `tests/`, `docs/`, `docs_src/`,
`examples/` or `scripts/` are info rather than errors.

Things the extractor does not treat as references at all: slash-separated
prose (`health/ready`, `net/http`), lists of top-level directories
(`errx/logx/timex`), host-like first segments (`ghcr.io/org/image`), paths
with a `path/to`, `foo`, `x` placeholder segment, illustrative identifiers
(`Type.Method`, `Class.method`, `--flag`, `UPPER_SNAKE`), receiver variables
that collide with a package name (`cfg.Addr` becomes info), and anything
matched by `.gitignore` (build artifacts).

## Staleness

| Rule | Meaning |
|---|---|
| `stale-section` | Requires git. The section (heading → next heading) was last edited at time *T* (from `git blame`), but a **file** it references has `minChurn` or more commits after *T*, or at least one commit and `minDays` days have passed. Directory references do not count. The message lists the most-changed files. Tune `stale.minChurn` / `stale.minDays`, exclude dated documents with `stale.exclude` (changelogs, specs and plans are excluded by default), or disable with `stale.enabled: false` or `--no-git`. |

## Bilingual pairs

Pairs come from `pairs` in `.docrot.json` plus automatic patterns <!-- docrot:ignore -->
(`README-zh.md`, `README.zh-TW.md`, `docs/en/…` ↔ `docs/zh/…`). <!-- docrot:ignore -->
Findings are reported on the translation file.

| Rule | Meaning |
|---|---|
| `pair-heading` | The heading structure (count and level sequence) differs. |
| `pair-code` | Code block *n* differs between source and translation, or the number of code blocks differs. Code is expected to be identical across translations. |
| `pair-link` | A link target appears in only one of the two files (links between the pair itself are exempt). |
| `pair-table` | A table has a different row/column shape. |
| `pair-number` | A number or version string appears in only one of the two files (info). |
| `pair-lag` | Requires git. The source has commits newer than the translation's last change. The message lists them. |

## Coverage

| Rule | Meaning |
|---|---|
| `undocumented` | Only when `coverage.report` is true (or with `docrot coverage`): an exported Go symbol, flag, or environment variable is not mentioned by any document. Only references docrot extracted count, so a flag that appears solely inside a ```` ```text ```` block is "undocumented" until it is mentioned in prose or a code span. |

## Exit codes

| Code | Meaning |
|---|---|
| 0 | No new finding at or above `--fail-on` (default `error`). |
| 1 | At least one new finding at or above `--fail-on`. Baselined findings never count. |
| 2 | Usage error, bad config, or internal error. |
