# docrot 設計規格

日期：2026-09-23
狀態：已定案（v1 範圍）

## 1. 一句話

**docrot 找出「文件在說謊」的地方。**

程式碼一直在動，文件卻很少跟著動。README、`llms.txt`、`docs/*.md` 裡寫的路徑早就搬走、
函式早就改名、flag 早就刪掉、章節錨點早就失效、中文版 README 落後英文版三個 commit。
沒有工具會告訴你這些——連結檢查器只看 URL，linter 只看 Markdown 格式。

docrot 把文件裡每一個「指向程式碼世界的引用」拿出來，逐一對照 repo 的真實狀態，
再用 git 歷史判斷哪些段落大概率已經過期，最後給出可以進 CI 的報告。

## 2. 為什麼值得做

- 每個 repo 都有文件，每份文件都會腐爛（documentation rot）。這是普遍痛點，但幾乎沒有人把它當成一個獨立工具來做——現有的工具要嘛只查 URL，要嘛只查 Markdown 語法，要嘛需要 Node.js 生態一整套。
- AI agent 時代文件產量暴增（spec、plan、llms.txt），腐爛速度更快；而 agent 讀到過期的 `llms.txt` 會直接寫出錯的程式碼。文件正確性第一次變成「執行正確性」的一部分。
- 使用者手上的 meowbase 系列 repo 正是典型：README 與 `llms.txt` 密集引用 `httpx.WriteData`、`docs/contracts.md`、`--flag`、`README-zh.md` 等，非常適合當第一批受測對象。

## 3. 限制與非目標

限制：

- 只用 Go 標準庫。不引入任何第三方模組（Markdown 解析、glob、YAML、顏色輸出全部自寫）。
- 單一靜態執行檔，跨平台（Windows 為主要開發與驗證平台）。
- git 是「可選加速器」：沒有 git 或不在 repo 內時，所有非 git 功能照常運作。

非目標：

- 不是 Markdown linter（不管標題層級、行寬、清單樣式）。
- 不做完整 CommonMark 解析；只需要正確辨識標題、fenced code block、inline code span、連結、表格、HTML 註解這幾種結構。
- 不執行文件裡的程式碼範例。
- 不做翻譯品質判斷；雙語只比「結構是否同步」。
- 不自動改文件（v1 只報告，不 `fix`）。

## 4. 使用者故事

1. 工程師改了 `httpx.WriteJSON` → `httpx.WriteData`，跑 `docrot check`，看到 README 第 42 行與 `llms.txt` 第 12 行還在引用舊名字，並附「did you mean `httpx.WriteData`」。
2. CI 上 `docrot check --fail-on error` 擋住新引入的壞路徑；既有的 200 個歷史問題先用 `docrot baseline` 凍結，之後只對新增問題失敗。
3. 維護 `README.md` 與 `README-zh.md` 的人跑 `docrot check`，得知中文版少了兩個標題、第三個程式碼區塊內容不同、英文版在中文版最後更新後又有 4 個 commit。
4. 想知道 `llms.txt` 覆蓋率的人跑 `docrot coverage`，看到哪些 exported 符號 / flag / 環境變數從未出現在任何文件中。
5. 想調整誤判的人跑 `docrot explain README.md`，看到每一個被抽出來的引用、判定種類、信心值，再決定加 ignore 規則。

## 5. 架構總覽

```text
              ┌─────────────┐
   docs ─────►│  markdown   │ tokens（heading / span / link / fence / table / comment）
              └──────┬──────┘
                     ▼
              ┌─────────────┐
              │  extract    │ References（kind / text / confidence / location）
              └──────┬──────┘
                     ▼
  repo ────► ┌─────────────┐      ┌──────────┐
  (code,     │   index     │◄─────┤  gitx    │（可選）
   files)    │ files/go/   │      └──────────┘
             │ odin/py/    │
             │ anchors/cfg │
             └──────┬──────┘
                    ▼
              ┌─────────────┐
              │  resolve    │ Findings（missing / suggestion）
              └──────┬──────┘
                     ▼
     ┌──────────┬────┴─────┬──────────┐
     │  stale   │  pairs   │ coverage │  其他 Findings
     └──────────┴────┬─────┴──────────┘
                     ▼
              ┌─────────────┐
              │  baseline   │ 過濾已知問題
              └──────┬──────┘
                     ▼
              ┌─────────────┐
              │  report     │ text / json / sarif / html
              └─────────────┘
```

`engine` 套件負責把上面串起來；`cmd/docrot` 只做 CLI 解析與 exit code。

## 6. 套件切分（Go module `docrot`）

| 套件 | 職責 | 依賴 |
|---|---|---|
| `internal/model` | 共用型別：`Reference`、`Finding`、`Kind`、`Confidence`、`Severity`、`Location`、`Index` 介面 | 無 |
| `internal/globx` | `**` doublestar glob 比對（stdlib `path.Match` 不支援） | 無 |
| `internal/markdown` | 行導向 Markdown tokenizer：heading、fence、span、link、image、table、HTML 註解、ignore 指令 | 無 |
| `internal/extract` | tokens → typed `Reference`；啟發式分類（path / gosym / odinsym / pysym / flag / env / configkey / anchor / url / command / import） | model, markdown |
| `internal/index/files` | 檔案樹索引（相對路徑集合、目錄集合、大小寫敏感比對、glob 查詢） | globx |
| `internal/index/gosym` | `go/parser` 建 Go 索引：package 名稱與 import path、func / type / method / field / const / var、`flag.*` 定義、`os.Getenv` 環境變數、`json:"..."` tag 路徑 | 無 |
| `internal/index/odin` | 正規表示式索引 Odin：`package`、`name :: proc`、`name :: struct/enum/union`、常數 | 無 |
| `internal/index/py` | 正規表示式索引 Python：`def`、`class`、模組名 | 無 |
| `internal/index/anchors` | 每個 Markdown 檔的標題 slug 集合（GitHub 規則，含 CJK） | markdown |
| `internal/index/config` | 從 JSON 樣本檔（`config.json`、`*.example.json`）抽出 dotted key 路徑 | 無 |
| `internal/index` | 組合上述為 `model.Index` 實作；併發建索引 | 以上 |
| `internal/resolve` | 每種 Kind 一個 resolver；產生 Finding；fuzzy 建議（Damerau-Levenshtein + 前綴/後綴啟發） | model |
| `internal/gitx` | `os/exec` 包 git：檔案最後 commit 時間、區間 commit 數、`blame --line-porcelain` 逐行時間、rename map；git 不存在時回 `ErrUnavailable` | 無 |
| `internal/stale` | 章節時間 vs 引用檔案 churn 的過期評分 | model, gitx |
| `internal/pairs` | 雙語配對偵測與結構指紋比對；git 落後統計 | markdown, gitx |
| `internal/coverage` | 反向：哪些 exported 符號 / flag / env 沒被任何文件提到 | model, index |
| `internal/baseline` | baseline 檔讀寫與 fingerprint | model |
| `internal/config` | `.docrot.json` 載入、預設值、驗證 | globx |
| `internal/report` | `text`（TTY 彩色）、`json`、`sarif`、`html` | model |
| `internal/engine` | 串接所有階段；併發；計時統計 | 全部 |
| `cmd/docrot` | 子命令與 flag、exit code | engine, report |

每個套件都能獨立測試；`model` 是唯一共享契約。

## 7. 核心型別（`internal/model`）

```go
type Kind string
const (
    KindPath      Kind = "path"       // 檔案或目錄路徑（含 glob）
    KindGoSymbol  Kind = "gosym"      // pkg.Name / Type.Method / Name()
    KindOdinSym   Kind = "odinsym"
    KindPySym     Kind = "pysym"
    KindFlag      Kind = "flag"       // --name / -name
    KindEnv       Kind = "env"        // UPPER_SNAKE
    KindConfigKey Kind = "configkey"  // a.b.c（非 Go package 開頭）
    KindAnchor    Kind = "anchor"     // file.md#slug 或 #slug
    KindURL       Kind = "url"        // http(s)://
    KindCommand   Kind = "command"    // shell 區塊中的可執行路徑
    KindImport    Kind = "import"     // go 區塊中的 import path
)

type Confidence int // Low=1 Medium=2 High=3

type Location struct {
    File string // 相對 repo root，forward slash
    Line int    // 1-based
    Col  int    // 1-based，byte offset
}

type Reference struct {
    Kind       Kind
    Text       string     // 原始文字（已去掉反引號等）
    Norm       string     // 正規化後用於查詢的文字
    Confidence Confidence
    Loc        Location
    Section    string     // 所屬最近標題（給 stale 用）
    Context    string     // 來源行（截斷）
}

type Severity string // "error" | "warning" | "info"

type Finding struct {
    Rule       string     // 例：missing-path, missing-symbol, unknown-flag, broken-anchor,
                          //     stale-section, pair-heading, pair-code, pair-link, pair-lag, undocumented
    Severity   Severity
    Message    string
    Loc        Location
    Ref        *Reference // 可能為 nil（pair / coverage）
    Suggestion string     // did you mean
    Fingerprint string    // baseline 用，穩定（不含行號）
    Data       map[string]any // 給 json/html 用的附加欄位
}
```

`Severity` 由 rule 與 confidence 決定：High → error、Medium → warning、Low → info；
stale/pair/coverage 一律 warning 或 info（可設定）。

## 8. 引用抽取規則（`extract`）

輸入是 Markdown tokens。抽取範圍：

- inline code span（主要來源）
- 連結目標 `[x](target)`、圖片 `![x](target)`、reference-style `[x]: target`
- fenced code block：依語言處理
  - `sh|bash|powershell|ps1|console|shell|cmd|text（含 $ 提示）`：逐行取指令，抽 `KindCommand`（`./scripts/x.ps1`、`scripts/x.py`、`go run ./cmd/x`、`go test ./pkg/...`、`odin build dir`、`python x.py`）
  - `go`：抽 `import "..."` → `KindImport`；`pkg.Ident` → `KindGoSymbol`（Medium）
  - 其他語言的區塊不抽（誤判太多）
- 純文字段落中的裸路徑（`docs/foo.md` 形式，需含 `/` 且以已知副檔名結尾）→ `KindPath`（Medium）

分類啟發式（依序判定，先中先贏）：

1. `scheme://` → url
2. 含 `#` 且前段為 `.md` 或空 → anchor
3. `--x` / `-x`（`x` 長度 ≥ 2，不含空白與 `/`） → flag；`--x=v` 取 `x`
4. `[A-Z][A-Z0-9_]{2,}` 且含 `_` → env（High）；純大寫無底線且 ≥ 4 字元 → env（Low，僅在索引有此名時才報）
5. 含 `/` 或 `\`，或以已知副檔名結尾，或首段等於 repo 頂層目錄名 → path；含 `*` → glob path
6. `ident.Ident` 且首段是已知 Go package 名（索引提供） → gosym（High）；`Type.Method` 首段是已知型別 → gosym（High）；`Ident(`…`)` → gosym（Medium）
7. Odin repo 中 `pkg.snake_case`、`snake_case(` → odinsym；Python repo 中 `module.func` / `CamelCase` class → pysym（Medium）
8. 小寫 dotted `a.b.c`（首段非 package 名）→ configkey（Low）
9. 其餘 → 不抽

排除：含 `<`、`>`、`{`、`}`、`$`、`%`、`...`（省略號當佔位）、空白內有多個片段但沒有任何 `.`/`/` 的自然語句；長度 > 120。

一個 span 可能產生多個 Reference（例如 `` `cfg.LoadJSON(path, &c); cfg.Validate(&c)` `` → 兩個 gosym）。
span 內用簡單 tokenizer 切出 `pkg.Ident` 型態的 token。

Ignore 指令（HTML 註解）：

- `<!-- docrot:ignore -->` 單獨一行：忽略下一個非空行
- `<!-- docrot:ignore-start -->` … `<!-- docrot:ignore-end -->`：忽略區間
- `<!-- docrot:ignore-file -->`：忽略整份文件
- 行尾 `<!-- docrot:ignore -->`：忽略該行

## 9. 索引（`index`）

### 9.1 files

- 走訪 repo（尊重 `exclude` glob；固定排除 `.git`）。
- 存：檔案集合、目錄集合、每個路徑的小寫版本（用於「大小寫不符」建議）。
- 查詢：`Exists(rel)`, `IsDir(rel)`, `Glob(pattern) []string`, `Similar(rel, n)`（同檔名不同目錄、大小寫差異、edit distance）。

### 9.2 gosym（`go/parser`，`parser.SkipObjectResolution|parser.ParseComments` 不需要 comments）

- 讀 `go.mod` 取 module path；每個含 `.go` 的目錄為一個 package（略過 `_test.go` 中的符號進另一集合 testOnly；略過 `vendor/`、`testdata/`）。
- 索引：
  - `Packages`: name → []dir；importPath → dir
  - `Symbols`: `pkg.Name`、`pkg.Type.Method`、`pkg.Type.Field`、`Type.Method`（不限 pkg，用於文件省略 pkg 的情況）、`Name`（不限 pkg，Low 用）
  - `Flags`: 由 `flag.String/Int/Bool/Duration/Float64/Uint/Var/...`、`flag.XxxVar`、`fs.Xxx`（任何 receiver 且方法名匹配且第一個或第二個引數為字串常值）抽出 name
  - `Env`: `os.Getenv("X")`、`os.LookupEnv("X")`、`os.Setenv("X"…)`、以及任何字串常值符合 `^[A-Z][A-Z0-9_]+$` 且出現在函式名含 `Env` 的呼叫中
  - `JSONKeys`: struct 欄位 `json:"name"` tag，遞迴建 dotted path（struct 內嵌 struct 型別在同 package 可解析；跨 package 不追）
- 併發解析檔案；解析錯誤的檔案略過並記錄警告（不中止）。

### 9.3 odin / py（regex）

- Odin：`^package\s+(\w+)`、`^(\w+)\s*::\s*(proc|struct|enum|union|distinct|#force_inline proc)`、`^(\w+)\s*::\s*[^p]`（常數）。索引 `pkg.name` 與 `name`。
- Python：`^\s*def\s+(\w+)`、`^\s*class\s+(\w+)`、模組名由檔名；索引 `module.name`、`Class.method`（依縮排推）與 `name`。

### 9.4 anchors

- 對每個 Markdown 檔（含被 exclude 的？否——只索引納入的 docs）產生 heading slug 集合。
- GitHub slug 規則：小寫、移除除了字母/數字/空白/連字號以外的字元（Unicode 字母數字保留，含 CJK）、空白→`-`、重複標題加 `-1`、`-2`。
- 也接受原始標題文字（URL-encoded）作為比對備援。

### 9.5 config

- 掃 `config*.json`、`*.example.json`、`*.sample.json`、`configs/**/*.json`（可設定），遞迴抽 dotted key（陣列以 `[]` 表示但比對時忽略）。

## 10. 解析與建議（`resolve`）

每個 Kind 一個 resolver：

| Kind | 檢查 | 建議來源 |
|---|---|---|
| path | 相對文件目錄 → 相對 root → 若含 glob 則至少匹配一個；目錄允許無尾斜線 | 同名檔案其他目錄、大小寫修正、edit distance ≤ 2、git rename map |
| gosym | `pkg.Name` 在 Symbols；`Type.Method`；`Name(`；generic `[T]` 去除 | 同 package 內 edit distance；其他 package 同名符號 |
| odinsym / pysym | 索引存在 | edit distance |
| flag | Flags 集合（去 `-`/`--`，允許 `_`/`-` 互換） | edit distance |
| env | Env 集合；Low confidence 者僅當索引非空且找不到才報 info | edit distance |
| configkey | JSONKeys 或 config 樣本鍵；Low → info | 前綴匹配 |
| anchor | 目標檔存在且 slug 存在 | 同檔 slug edit distance |
| url | 預設不檢查；`--net` 時 HEAD/GET，逾時 5s，並發 8，只報 4xx/5xx/連線失敗 | 無 |
| command | 第一個 token 或 `go run/test/build` 後的 `./x` 路徑存在 | 同 path |
| import | 以 module path 為前綴 → 對應目錄存在；否則若為 stdlib（`goroot/src` 存在或無 `.`）略過 | 同 path |

Fuzzy：Damerau-Levenshtein，候選集合限制在同類（同 package / 同目錄），距離 ≤ max(2, len/4)，最多 3 個建議，最佳者放 `Suggestion`。

## 11. 過期評分（`stale`，需要 git）

1. 對每份文件 `git blame --line-porcelain` 取逐行 committer time；章節時間 = 章節內所有行的最大值。
2. 對章節內每個成功解析的 path/command/import/gosym（gosym 對應到定義所在檔案）引用，查 `git log --format=%ct --since=<章節時間> -- <file>` 的 commit 數 `n` 與最新時間。
3. 若 `n ≥ stale.minChurn`（預設 3）或（`n ≥ 1` 且最新變更距章節時間 ≥ `stale.minDays` 天，預設 90）→ `stale-section` warning，訊息列出前 5 個最動盪的引用。
4. 每份文件只報一次同一章節；fingerprint = doc + section slug。
5. git 呼叫做快取（每檔一次 blame，每檔一次 log），並發上限 4（Windows 上 git 起程序較貴）。

## 12. 雙語配對（`pairs`）

配對來源：設定檔 `pairs`，加上自動樣式（預設）：
`{stem}-zh.md`、`{stem}_zh.md`、`{stem}.zh.md`、`{stem}.zh-TW.md`、`{stem}.zh-CN.md`、`{stem}-zh-TW.md`、`{stem}.ja.md`、`{stem}.ko.md`、`docs/en/**` ↔ `docs/zh/**`。

結構指紋（對兩份文件各算一次）：

- 標題序列（層級 + 序號；文字不比）
- fenced code block 序列（語言 + 內容 SHA-256；預期翻譯版程式碼相同）
- 連結目標集合（排除 anchor 與指向彼此的連結）
- 圖片目標集合
- 表格序列（列數 × 欄數）
- 數字集合（整數與版本號 `\d+(\.\d+)+`）

規則：

- `pair-heading`：標題數不同或層級序列不同（列出差異位置）
- `pair-code`：第 i 個程式碼區塊內容不同 / 數量不同
- `pair-link`：只在一邊出現的連結
- `pair-table`：表格形狀不同
- `pair-number`：只在一邊出現的數字（info）
- `pair-lag`（需要 git）：source 在 translation 最後 commit 之後的 commit 數 ≥ 1 → warning，附 commit 短 hash 與標題（最多 5）

## 13. 覆蓋率（`coverage`）

- 收集所有文件中成功解析的 gosym / flag / env 集合。
- Exported 符號來源：非 `internal/`、非 `_test`、非 `main` package 的 exported func/type/method/const/var（可設定 `coverage.includeInternal`）。
- 產出：每個 package 的 `documented/total` 與未提及列表；flag 與 env 各一組。
- `docrot coverage` 單獨顯示表格；`docrot check` 預設不把 undocumented 當 finding（`coverage.report: true` 才報 info）。

## 14. Baseline

- `docrot baseline` 寫 `.docrot-baseline.json`：`{"version":1,"generated":"…","findings":[{"fingerprint":"…","rule":"…","file":"…","message":"…"}]}`。
- fingerprint = SHA-256 前 16 hex of `rule|file|kind|norm|section`（不含行號；section 用 slug）。
- `check` 時 baseline 內的 finding 標記 `baselined`，預設不顯示、不計入 exit code；`--all` 顯示。
- 報告末尾顯示「N baselined, M new, K fixed（baseline 有但這次沒出現）」。

## 15. 設定檔 `.docrot.json`

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
  "severity": { "stale-section": "warning", "pair-lag": "warning", "pair-number": "info" },
  "net": false,
  "failOn": "error"
}
```

未提供設定檔時使用預設；CLI flag 覆蓋設定檔。`ignore` 為 regex 陣列，套用在 `Reference.Text`。

## 16. CLI

```text
docrot check [dir] [--format text|json|sarif|html] [--output FILE] [--fail-on error|warning|info|none]
             [--min-confidence low|medium|high] [--no-git] [--net] [--all] [--config FILE] [--quiet]
docrot baseline [dir]                    重新產生 baseline
docrot coverage [dir] [--format text|json]
docrot pairs [dir]                       只跑雙語配對
docrot explain <doc> [--kind K]          列出該文件抽出的所有引用與解析結果
docrot index [dir] --kind symbols|flags|env|paths|anchors|config
docrot init [dir]                        寫出預設 .docrot.json
docrot version
```

Exit code：`0` 無達到 fail-on 的新 finding；`1` 有；`2` 用法或內部錯誤。

text 報告格式（一行一個 finding，與編譯器慣例一致，方便編輯器跳轉）：

```text
README.md:42:15: error missing-symbol `httpx.WriteJSON` not found in package httpx (did you mean httpx.WriteData?)
llms.txt:12:3: warning missing-path `docs/contract.md` not found (did you mean docs/contracts.md?)
README-zh.md: warning pair-lag 4 commits to README.md since README-zh.md last changed (a1b2c3d "rename WriteJSON", …)

3 errors, 2 warnings, 5 info (12 baselined) — 14 docs, 1,204 references, 0.83s
```

## 17. 報告（`report`）

- `text`：如上；stdout 為 TTY 且非 Windows 舊主控台時上色（用 ANSI，Windows 10+ 支援；透過 `os.Getenv("NO_COLOR")` 關閉）。
- `json`：`{"version":1,"summary":{…},"findings":[…]}` 穩定 schema。
- `sarif`：SARIF 2.1.0 最小合法輸出（tool.driver.rules、results[].locations），可直接上傳 GitHub code scanning。
- `html`：單檔、內嵌 CSS 與極少量 JS（篩選 rule / severity / 檔案），摘要卡片、依檔案分組列表、雙語與覆蓋率各一區。深淺色跟隨系統。

## 18. 錯誤處理

- 文件讀取失敗：略過該檔並發出 `docrot: warning: …` 到 stderr，繼續。
- Go 檔解析失敗：略過該檔，索引仍可用；`--verbose` 顯示。
- git 不可用 / 非 repo / 逾時：stale 與 pair-lag 靜默停用，摘要行標記 `(git: unavailable)`。
- 設定檔 JSON 錯誤：exit 2 並指出行號。
- 內部 panic：只在 `--verbose` 印 stack；exit 2。

## 19. 效能目標

- meowbase 規模（~200 .go、~40 .md）：`check` < 2 秒（不含 git）；含 git < 6 秒。
- 記憶體不超過 200 MB。
- 索引建構與文件抽取併發（`runtime.NumCPU()` worker）。

## 20. 測試策略

- 每個套件單元測試（table-driven）。
- `testdata/fixture/`：一個小型多語言 repo（Go module + Odin + Python + docs + 雙語 README + config.json），內含已知的正確與錯誤引用；`engine` 的 golden 測試比對 JSON 輸出（去掉時間欄位）。
- `gitx`、`stale`、`pairs` 的 git 相關測試：在 `t.TempDir()` 用 `git init` 建立真實歷史；找不到 git 則 `t.Skip`。
- Dogfood：`scripts/verify.py` 執行 `go vet`、`go test ./...`、build、然後對 docrot 自己的 repo 跑 `docrot check --fail-on error`（docrot 的 README.md / README-zh.md 就是雙語配對的活範例），最後對 `../meowbase` 等同層 repo 跑一次並印出摘要（僅示範，不影響結果）。

## 21. 交付物

- `docrot.exe`（`go build ./cmd/docrot`）
- `README.md`（英文）、`README-zh.md`（繁中）、`CHANGELOG.md`、`docs/superpowers/specs/*`、`docs/superpowers/plans/*`、`docs/rules.md`（每條 rule 的意義與消除方法）
- `.docrot.json` 範例（repo 自用）
- `scripts/verify.py`、`scripts/demo.py`（對指定目錄跑並輸出 HTML 報告）
