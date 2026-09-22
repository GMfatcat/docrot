# docrot rules

Every finding carries a rule id. This page explains what each rule means,
how docrot decided, and how to make the finding go away.

Severity for the `missing-*` / `unknown-*` / `broken-*` rules follows the
extractor's confidence: **high → error**, **medium → warning**, **low → info**.
Confidence depends on *how unambiguous the text is*, not on how sure docrot is
that the thing is missing. `docrot explain <doc>` shows the confidence assigned
to every extracted reference.

To silence a single false positive, add an HTML comment:

```markdown
<!-- docrot:ignore -->
The next non-blank line is ignored.

Inline: `legacy/path.go` <!-- docrot:ignore -->

<!-- docrot:ignore-start -->
Everything here is ignored.
<!-- docrot:ignore-end -->
```

`<!-- docrot:ignore-file -->` anywhere in a file skips the whole file. For
patterns, add regular expressions to `ignore` in `.docrot.json`; they are
matched against the reference text.

## Reference rules

| Rule | Kind | Meaning |
|---|---|---|
| `missing-path` | path | A file or directory mentioned in the document does not exist. docrot tries the path relative to the document, relative to the repo root, and as a glob. Suggestions come from same-name files elsewhere, case differences, small typos, and git rename history. |
| `missing-symbol` | gosym / odinsym / pysym | A code symbol mentioned in the document does not exist. Go symbols are resolved with `go/parser`: `pkg.Name`, `pkg.Type.Method`, `Type.Method`, `Name()`. Odin and Python symbols are resolved from a lightweight declaration index. |
| `unknown-flag` | flag | `--name` / `-name` in the document is not defined by any `flag.*` call in the code. `-` and `_` are treated as equivalent. |
| `unknown-env` | env | An `UPPER_SNAKE` name is never read via `os.Getenv`, `os.LookupEnv`, or any call whose name contains `Env`. Names without an underscore are only reported when the code defines *some* environment variables. |
| `unknown-config-key` | configkey | A dotted key such as `server.addr` appears neither as a `json:"…"` / `yaml:"…"` / `toml:"…"` tag path in any struct nor in any sample config file (`config*.json`, `*.example.json`, …). Always low confidence → info. |
| `broken-anchor` | anchor | A link such as `[x](docs/guide.md#setup)` or `[x](#setup)` points to a heading that does not exist. Slugs follow GitHub rules, including CJK headings and `-1` suffixes for duplicates. |
| `broken-url` | url | Only with `--net`: an external URL returned 4xx/5xx or failed to connect. |
| `missing-command` | command | In a shell code block, the script or path a command refers to (`./scripts/x.ps1`, `go run ./cmd/x`, `python tools/y.py`, `odin build dir`) does not exist. |
| `missing-import` | import | In a Go code block, an import path under this module's path does not correspond to a package directory. Imports outside the module (stdlib, third-party) are ignored. |

## Staleness

| Rule | Meaning |
|---|---|
| `stale-section` | Requires git. The section (heading → next heading) was last edited at time *T* (from `git blame`), but the code it references has `minChurn` or more commits after *T*, or at least one commit and `minDays` days have passed. The message lists the most-changed references. Tune `stale.minChurn` / `stale.minDays` in `.docrot.json` or disable with `stale.enabled: false` or `--no-git`. |

## Bilingual pairs

Pairs come from `pairs` in `.docrot.json` plus automatic patterns
(`README-zh.md`, `README.zh-TW.md`, `docs/en/x.md` ↔ `docs/zh/x.md`, …).
Findings are reported on the translation file.

| Rule | Meaning |
|---|---|
| `pair-heading` | The heading structure (count and level sequence) differs. |
| `pair-code` | Code block *n* differs between source and translation, or the number of code blocks differs. Code is expected to be identical across translations. |
| `pair-link` | A link target appears in only one of the two files. |
| `pair-table` | A table has a different row/column shape. |
| `pair-number` | A number or version string appears in only one of the two files (info). |
| `pair-lag` | Requires git. The source has commits newer than the translation's last change. The message lists them. |

## Coverage

| Rule | Meaning |
|---|---|
| `undocumented` | Only when `coverage.report` is true (or with `docrot coverage`): an exported Go symbol, flag, or environment variable is not mentioned by any document. |

## Exit codes

| Code | Meaning |
|---|---|
| 0 | No new finding at or above `--fail-on` (default `error`). |
| 1 | At least one new finding at or above `--fail-on`. Baselined findings never count. |
| 2 | Usage error, bad config, or internal error. |
