# docrot

**繁體中文** · [English](README.md)

**docrot 找出「文件在說謊」的地方。**

程式碼一直在動，文件卻很少跟著動。README 還在講兩個 sprint 前就改名的函式，
`llms.txt` 把 AI agent 指向早已不存在的檔案，快速上手裡的 `--config` flag 早就刪了，
指南裡的錨點失效，中文 README 落後英文版三個 commit。連結檢查器只看 URL，Markdown
linter 只看排版，沒有任何工具檢查文件裡的「主張」。

docrot 把文件對 repo 的每一個主張——檔案路徑、Go/Odin/Python 符號、CLI flag、環境變數、
設定鍵、標題錨點、shell 指令、Go import 路徑——逐一抽出來，對照真正的程式碼。接著用 git
歷史找出「散文最後一次修改之後，引用的程式碼已經大幅變動」的章節，並比對雙語文件的結構是否漂移。

它是單一靜態執行檔，以 Go 撰寫，**除了標準庫沒有任何依賴**。

```text
CHANGELOG.md:91:5: error missing-symbol `httpx.Retry` not found in package httpx (did you mean httpx.ClientConfig.Retry?)
docs/llms-reference.md:148:3: warning missing-path `internal/api` not found at the repo root (exists under examples/service/)
README.md:59: warning stale-section section "🚀 快速上手" last edited 2026-08-04; since then servicex/app.go: 4 commits (latest 2026-08-12)
README-zh.md:1: warning pair-heading translation has 4 headings, source has 5

50 errors, 52 warnings, 214 info — 51 docs, 2,306 references, 1.36s [214 info hidden; --info to show]
```

這幾行來自對一個內部 Go repo 的真實執行；docrot 在八個 repo、三種語言上找到了什麼，
見[實地報告](docs/field-report.md)；對 httpx、Starlette、Typer、Pydantic 與 FastAPI
（1,692 份文件、372 組翻譯配對）的結果見 [Python 實地報告](docs/field-report-python.md)。

## 安裝

```sh
go install ./cmd/docrot          # from a clone
go build -o docrot ./cmd/docrot  # or just build the binary
```

需要 Go 1.26+。`PATH` 上的 `git` 是選配；沒有 git 時，依賴 git 的規則會靜默停用。

## 快速上手

```sh
cd your-repo
docrot check                    # text report, exit 1 on errors
docrot check --format html --output docrot.html
docrot explain README.md        # what did it extract, and why?
docrot coverage                 # which exported API is never documented?
docrot baseline                 # freeze today's findings; fail only on new ones
```

## 檢查什麼

| 規則 | 文件說… | docrot 驗證… |
|---|---|---|
| `missing-path` | `` `internal/gitx/gitx.go` ``、`[rules](docs/rules.md)` | 檔案或目錄存在（相對於文件或 repo 根目錄；允許 glob） |
| `missing-symbol` | `` `report.WriteSARIF` ``、`` `Resolver.Resolve()` ``、`` `render_frame()` `` | Go 符號存在（透過 `go/parser`），或 Odin／Python 宣告存在 |
| `unknown-flag` | `` `--format` `` | 有某個 `flag.*` 呼叫定義了它 |
| `unknown-env` | `` `DOCROT_DEBUG` `` | 程式碼有讀它（`os.Getenv`、`os.LookupEnv`、任何名字含 `Env` 的呼叫） |
| `unknown-config-key` | `` `stale.minChurn` `` | 某個 `json:"…"`／`yaml:"…"`／`toml:"…"` tag 路徑或設定樣本檔有這個鍵 |
| `broken-anchor` | `[x](docs/rules.md#exit-codes)` | 標題存在（GitHub slug 規則，支援 CJK） |
| `missing-command` | ```` ```sh ```` 區塊裡的 `python scripts/verify.py` | 腳本／套件路徑存在 |
| `missing-import` | ```` ```go ```` 區塊裡的 `import "docrot/internal/model"` | 套件目錄存在於本模組 |
| `broken-url` | `https://…`（僅在 `--net` 時） | URL 回應 2xx/3xx |
| `stale-section` | 某章節最後編輯於 2026-06-01 | 它引用的程式碼此後沒有大幅變動（git） |
| `pair-*` | `README.md` ↔ `README-zh.md` | 標題相同、程式碼區塊相同、連結／表格／數字相同、翻譯沒有落後原文（git） |
| `undocumented` | — | 每個 exported 符號／flag／環境變數都有文件提到（`docrot coverage`） |

每條規則的說明與消除方法都在 [docs/rules.md](docs/rules.md)。

## 怎麼判斷

1. **Tokenize** 每個 Markdown 檔：標題、fenced 區塊、inline code span、連結、圖片、表格、
   HTML 註解。不依賴任何 CommonMark 實作。
2. **抽取** code span、連結目標、shell 區塊與 Go 區塊裡的引用。每個引用都有*種類*與*信心值*：
   有目錄有副檔名的路徑是 high；光一個檔名是 medium；第一段是不認識的小寫字的點號名稱
   （`app.Run`）是 low，永遠不會被報出來。
3. **建索引**，整個 repo 只做一次：檔案樹、Go 套件／符號／flag／環境變數／tag（`go/parser`）、
   Odin 與 Python 宣告（regex）、Markdown 錨點、JSON 樣本鍵。
4. **解析**每個引用，產生附「你是不是想找」建議的 finding（在正確的候選集合上做
   Damerau-Levenshtein，路徑另外參考 git 改名歷史）。
5. **過期判定**：`git blame` 給每個章節一個編輯時間；`git log` 數出那之後每個被引用檔案的 commit 數。
6. **雙語配對**：對兩份文件的結構指紋做 diff。
7. **Baseline**：指紋不含行號，所以一般編輯不會讓 baseline 失效。

嚴重度跟著信心值走：high → error、medium → warning、low → info。flag 與環境變數再軟一級，
因為它們太常是在講*別的*程式；glob 沒命中、設定鍵與光禿禿的檔名一律 info。文字報告預設隱藏
info，加 `--info` 才列出。

docrot 是在真實 repo 上調校的，不是合成範例。它刻意忽略的東西：被 `.gitignore` 排除的路徑
（建置產物）、`health/ready` 或 `net/http` 這類散文、外部程式後面的 flag（`go test -race`）、
所在行完全沒提到環境的 `UPPER_SNAKE` 字、`cfg` 同時是套件又是變數時的 `cfg.Addr`，以及
`Type.Method`、`--flag`、`path/to/file` 這類示意用名稱。

大小寫在每個平台上都嚴格比對。檔案叫 `Docs/Foo.md` 而文件寫 `docs/foo.md`，在 Windows
與 macOS 上一樣會得到 finding，訊息寫明「只差大小寫」——因為這種連結在作者的筆電上能開，
到 Linux CI 就壞。文件探索本身則不分大小寫，所以 `README.MD` 與 `readme.md` 都會被掃描。

## 設定

`docrot init` 會寫出帶預設值的 `.docrot.json`：

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
  "maxFileMB": 8
}
```

- `ignore` 是套用在引用文字上的正規表示式。
- `siblings` 列出其他 repo（相對於根目錄），這裡找不到的路徑可能合理地住在那邊——
  服務文件裡引用它所依賴的函式庫時很有用。
- `stale.exclude` 把有日期的文件（changelog、設計規格）排除在過期分析之外；它們本質上是歷史紀錄。
- `severity` 覆蓋某條規則的等級，例如 `{"stale-section": "info"}`。
- `outDir` 是每次執行都會重寫的輸出目錄；見下。
- `maxFileMB` 限制 docrot 會「讀內容」的檔案大小（文件、Go／Odin／Python 原始碼、JSON 樣本）。
  二進位檔完全不會被打開——只有檔名進入路徑索引，所以一個 4 GB 的模型檔不論有沒有被
  gitignore 都只花一筆目錄項目。超過上限的文字檔會跳過並警告；指向它的路徑仍然能解析。

行內逃生口：

```markdown
<!-- docrot:ignore -->            the next non-blank line
inline text <!-- docrot:ignore -->  this line
<!-- docrot:ignore-start --> … <!-- docrot:ignore-end -->
<!-- docrot:ignore-file -->
```

### 輸出目錄

每次 `docrot check` 都會重寫一個目錄——由 `outDir` 設定指定——讓人與 agent
永遠在同一個地方找到最新的報告：

```text
.docrot/.gitignore   a single "*", so the reports never reach a commit
.docrot/report.md    for agents: findings by file, how to read them, a checklist
.docrot/report.html  for humans: the filterable single-file page
.docrot/report.json  the stable JSON schema
.docrot/report.txt   the terminal report, with info findings
```

每個檔案都先寫到暫存檔再改名就位，所以中途被打斷的執行，不會在下一個讀者期待
完整報告的地方留下半份。這個目錄會被排除在文件探索之外，所以昨天的報告永遠不會
被當成文件來檢查。用 `--out-dir` 換個地方寫，用 `--no-out` 讓這次不寫，或把
`outDir` 設成空字串永久關掉。

## 指令

```text
docrot check [dir] [--format text|md|json|sarif|html] [--output FILE]
             [--fail-on error|warning|info|none] [--min-confidence low|medium|high]
             [--out-dir DIR] [--no-out]
             [--no-git] [--net] [--info] [--all] [--coverage] [--quiet] [--config FILE]
docrot baseline [dir]            write .docrot-baseline.json
docrot coverage [dir]            documentation coverage table
docrot pairs [dir]               only the bilingual checks
docrot explain <doc> [--kind K]  every extracted reference with its verdict
docrot index [dir] --kind symbols|flags|env|paths|anchors|config|odin|python
docrot init [dir]
docrot version
```

掃描類指令共用的 flag：`--config` 指定設定檔、`--no-git` 停用 git 規則、`--net` 檢查 URL、
`--verbose` 印出索引與 git 警告、`--min-confidence` 丟掉弱引用。`check` 另有 `--format`、
`--output`、`--fail-on`、`--info`、`--all`、`--coverage`、`--quiet`、`--out-dir` 與
`--no-out`；`explain` 另有 `--kind` 與 `--root`；`index` 接受 `--kind`。

Exit code：`0` 乾淨、`1` 有達到 `--fail-on` 的新 finding、`2` 用法或內部錯誤。SARIF 輸出可直接上傳
GitHub code scanning；已 baseline 的 finding 會帶 `baselineState: unchanged`。

## CI

```yaml
- run: go run ./cmd/docrot check --format sarif --output docrot.sarif --fail-on error
- uses: github/codeql-action/upload-sarif@v3
  with: { sarif_file: docrot.sarif }
```

## 開發

```sh
python scripts/verify.py        # gofmt, vet, test, build, self-check, fixture check, formats
python scripts/demo.py ../some-repo --out reports
```

docrot 在 `scripts/verify.py` 裡會檢查自己的文件；`docs/superpowers/` 下有日期的設計文件在那裡被排除，
因為它們本來就充滿示意用路徑。

設計：[docs/superpowers/specs/2026-09-23-docrot-design.md](docs/superpowers/specs/2026-09-23-docrot-design.md)。
計畫：[docs/superpowers/plans/2026-09-23-docrot-plan.md](docs/superpowers/plans/2026-09-23-docrot-plan.md)。
規則：[docs/rules.md](docs/rules.md)。實地報告：[docs/field-report.md](docs/field-report.md)、[docs/field-report-python.md](docs/field-report-python.md)。
給 agent 的入口：[llms.txt](llms.txt)。變更：[CHANGELOG.md](CHANGELOG.md)。

## 非目標

- 不是 Markdown linter。排版不關 docrot 的事。
- 不是 CommonMark 實作。它只認得它需要的東西。
- 不執行範例，也不評斷翻譯品質。
- 不改寫文件（目前）。

## 授權

MIT——見 [LICENSE](LICENSE)。
