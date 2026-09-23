# 指令

**繁體中文** · [English](commands.md)

## 總覽

```text
docrot check [dir] [--format text|md|json|sarif|html|github|junit] [--output FILE]
             [--fail-on error|warning|info|none] [--min-confidence low|medium|high]
             [--out-dir DIR] [--no-out]
             [--no-git] [--net] [--info] [--all] [--coverage] [--quiet] [--config FILE]
             [--changed] [--since REF]
docrot baseline [dir]            write .docrot-baseline.json
docrot coverage [dir]            documentation coverage table (symbols, flags, env, routes, config keys)
docrot pairs [dir]               only the bilingual checks
docrot comments [dir]            comment checks over every exported declaration
docrot fix [dir] [--apply]       rewrite paths git renamed or that differ only by letter case
docrot explain <doc> [--kind K]  every extracted reference with its verdict
docrot index [dir] --kind symbols|flags|env|paths|anchors|config|routes|targets|defaults|odin|python|rust|js|csharp|c
docrot init [dir]
docrot version
```

`dir` 預設是目前目錄。`docrot <command> -h` 列出單一指令的 flag。

## Flag

掃描類指令共用：`--config` 指定設定檔、`--no-git` 停用 git 規則、`--net` 檢查 URL、
`--verbose` 印出索引與 git 警告、`--min-confidence` 丟掉弱引用。

`check` 另有：

- `--format` 與 `--output`——把一份報告寫到檔案而不是終端。輸出目錄照樣會寫。
  `github` 每條 finding 印一行 workflow command（`::error file=README.md,line=12,title=missing-path::…`），
  GitHub 與 Gitea Actions 會把它變成 PR 上的標註；`junit` 輸出 JUnit XML，每份文件每條規則
  一個 test case（info 級是 skipped），GitLab、Jenkins、Gitea 的測試面板都能讀。兩者都跟文字報告一樣，
  沒有 `--info` 就不列 info，沒有 `--all` 就不列 baselined。
- `--fail-on`——讓 exit code 變成 1 的最低嚴重度（預設取自設定檔，`error`）；`none` 永不失敗。
- `--info` 讓文字報告列出 info 級 finding；`--all` 連已 baseline 的也一起顯示。
- `--coverage` 附上文件覆蓋率那一節。
- `--quiet` 只印摘要那一行。
- `--out-dir` 與 `--no-out` 換掉或跳過輸出目錄。
- `--changed` 只檢查工作樹或索引裡相對 HEAD 有修改的文件，加上未追蹤的；`--since REF` 再加上
  這條分支自與 `REF` 的 merge base 以來改過的文件（PR 檢查）。所有文件仍會被解析，跨文件的
  錨點才找得到；覆蓋率會跳過，因為它需要全部文件；乾淨的工作樹什麼都不檢查，回傳 0。兩者都需要 git。

`explain` 另有 `--kind`（只列某一種引用）與 `--root`（文件路徑在 repo 外時指定 repo 根目錄）；
`index` 接受 `--kind`。

## Exit code

| Code | 意義 |
|---|---|
| 0 | 沒有達到 `--fail-on` 的新 finding。 |
| 1 | 至少一個達到 `--fail-on` 的新 finding。已 baseline 的永遠不算。 |
| 2 | 用法錯誤、設定檔錯誤或內部錯誤。 |

## Baseline

`docrot baseline` 把目前的 finding 凍結到 `.docrot-baseline.json`；之後的執行只對不在裡面的
finding 失敗，`--all` 可以把已 baseline 的再顯示出來。指紋不含行號與章節標題，所以在 finding
附近編輯不會讓它復活。

## 修正

`docrot fix` 只套用建議裡機械性的那一半：git 歷史記錄過改名的路徑（`old/name.go` → <!-- docrot:ignore missing-path -->
`pkg/httpx/server.go`，原文的 `./` 前綴與結尾 `/` 照留）、以及和真實檔名只差大小寫的路徑 <!-- docrot:ignore missing-path -->
（`docs/Guide.md` 被寫成 `docs/guide.md`，照文件原本用的框架改：repo 根或文件所在目錄）。 <!-- docrot:ignore missing-path -->
不加 `--apply` 只印出每一處修改與前後兩行，不寫檔；加 `--apply` 才改寫文件，保留原檔的換行
風格與 BOM。符號改名、錨點、模糊的「你是不是想找」建議一律不套用；baselined 的 finding 不動。
`--format json` 列出所有修改。改名表需要 git。

## CI

```yaml
- run: go run ./cmd/docrot check --format sarif --output docrot.sarif --fail-on error
- run: go run ./cmd/docrot check --changed --since origin/main --fail-on warning   # PR: changed docs only
- run: go run ./cmd/docrot check --format github --fail-on warning                # annotations on the PR, no SARIF upload needed
- uses: github/codeql-action/upload-sarif@v3
  with: { sarif_file: docrot.sarif }
```

SARIF 輸出可直接上傳 GitHub code scanning；已 baseline 的 finding 會帶 `baselineState: unchanged`。
當 pre-commit hook 用的話，`docrot check --changed --fail-on warning` 在乾淨的工作樹上遠低於一秒。

## 效能剖析

`DOCROT_CPUPROFILE=cpu.prof docrot check …` 會寫出這次執行的 CPU profile，給 `go tool pprof` 用。
