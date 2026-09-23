<!-- docrot:ignore-file -->
<!-- A design document: the rules, flags and packages named here do not
     exist yet, so docrot skips this file. -->
# docrot 0.9.0 設計：roadmap tier 2

對應計畫：[`../plans/2026-09-24-docrot-tier2-plan.md`](../plans/2026-09-24-docrot-tier2-plan.md)

## 目標

把 `docs/roadmap.md` tier 2 剩下的五項做完。五項彼此獨立，共同點是「已經算出來的東西
再多走一步」：報告多兩種輸出、配對多兩條規則、Go 範例多一道語法檢查、覆蓋率多兩個
分組、高信心的建議直接套用。仍然只用 Go 標準庫，不執行任何東西。

## 1. `--format github` 與 `--format junit`

- `github`：每個 finding 一行 workflow command，
  `::error file=README.md,line=12,col=3,title=missing-path::message (did you mean x?)`，
  severity 對應 `error`／`warning`／`notice`。baselined 的 finding 不輸出（它們本來就不算）；
  最後一行是摘要，以 `::notice title=docrot::` 印出，所以 PR 的 Checks 頁面直接看得到
  每一條。Gitea Actions 認同一套語法。
- `junit`：一個 `<testsuite name="docrot">`。JUnit 裡一個 testcase 只能失敗一次，所以
  testcase 是「文件 × 規則」：`<testcase classname="README.md" name="missing-path">`，
  `<failure message="第一條">` 的內文列出該文件該規則的全部 finding（`file:line: message`）。
  info 級的 finding 變成 `<skipped>`，乾淨的文件是一個沒有子元素的 `<testcase name="ok">`，
  GitLab、Jenkins、Gitea 的 JUnit 面板都能讀。suite 屬性：`tests`、`failures`、`skipped`、`time`。
- 兩者都是 `report.Write` 的新 case，`report.Formats` 多兩個名字；輸出目錄（`.docrot/`）
  不寫這兩種，它們只在 `--format … --output …` 時有意義。

## 2. `pair-missing` 與 `pair-orphan`

只在**目錄式**翻譯樹成立：`docs/en/x.md` ↔ `docs/zh/x.md`。後綴式（`README-zh.md`）
沒有「集合」可言，一份沒翻的 README 不是漏。

- 偵測：`pairs.Detect` 找出來的配對裡，凡是靠目錄慣例配成的（`en` 段換成 `LangSiblings`
  其中一個），記下 `(root, lang)`——例如 `docs`、`zh`。一個 `(root, lang)` 只要有一對存在，
  就是一個翻譯集合。
- `pair-missing`（info）：集合的來源樹裡每一份 `docs/en/**.md`，在 `docs/zh/` 沒有同名檔。
  報在來源檔第 1 行，訊息「no zh translation (docs/zh/x.md)」。
- `pair-orphan`（warning）：`docs/zh/**.md` 在 `docs/en/` 沒有同名檔。報在翻譯檔第 1 行，
  訊息「source docs/en/y.md no longer exists」。
- 兩條都不需要 git；`docrot pairs` 一起列；severity 可從 `severity` 覆寫；`stale.exclude`
  無關。fixture 加一棵 `docs/en`／`docs/zh` 小樹（一對完整、一份漏翻、一份孤兒）。

## 3. `example-syntax`：Go 範例區塊要能 parse

- 對每個 ```` ```go ```` 區塊用 `go/parser` 試四種包法，任一成功就過：
  完整檔案；補 `package main`；`package main` + 區塊裡的 `import` 宣告 + 其餘行包進
  `func _() { … }`；`package main` + `import` + 其餘行包進 `var _ = T{ … }`（struct／map
  常值片段）。
- 略過：資訊字串帶 `ignore`、`skip`、`no-check`、`nocheck`、`norun` 的區塊；內容含 `...`
  或 `…` 的區塊（示意省略）；內容含 `{{`（模板）；被 `docrot:ignore` 蓋住的行；空區塊。
- 找到錯誤時一條 warning，`Loc` 指到文件裡出錯的那一行（區塊起始行 + parser 回報的行），
  訊息「Go example does not parse: expected '}', found 'EOF'」；fingerprint 用區塊內容的雜湊，
  不含行號。規則 id `example-syntax`，`severity` 可覆寫；`<!-- docrot:ignore example-syntax -->`
  放在區塊開頭那一行。
- 住在新套件 `internal/examples`，engine 在 extract 之後對每份文件呼叫一次；
  `--changed` 時只看改到的文件。不做 gofmt 比對（roadmap 說「optionally」，先不做）。
- 調校：meowbase 系列六個 repo 有 1,500 多個 Go 區塊，先跑一輪看噪音類型再定包法。

## 4. 覆蓋率：路由與設定鍵

- `coverage.Result` 多 `Routes Group` 與 `Configs Group`；`Compute` 不動（既有測試用
  `reflect.DeepEqual`），engine 另外呼叫 `coverage.Routes(listed, documented)` 與
  `coverage.Configs(keys, mentioned)`。
- 路由：`ix.Routes()` 列的每一條 `METHOD /path/{}`（或無 method、或 `/prefix/**`），
  文件提到的 `route|…` 引用先正規化成同一種形狀（`routes.Normalize` + `Display`）；
  無 method 的文件引用蓋住同路徑的每個 method，prefix 條目由任何以該前綴開頭的引用蓋住。
- 設定鍵：`ix.ConfigKeys()` 的每一個 dotted key（含所有前綴）；只算**葉節點**
  （不是其他鍵的前綴），否則 `server` 這種中間節點永遠算沒文件。文件提到 `configkey|X`
  或 `default` 引用的 `key:X|…` 就算有。
- 三種報告（text、md、html、json）多兩列；`docrot coverage` 的表多 `routes`、`config` 兩行；
  `coverage.report: true` 時 `undocumented` finding 多兩類，訊息「route GET /x is not
  mentioned in any document」、「config key a.b is not mentioned…」，位置是註冊處
  （路由）或樣本檔（設定鍵）。

## 5. `docrot fix`

- resolver 在**確定是機械性**的情況下把修好的文字放進 `Finding.Data["fix"]`：
  git 改名（`Renames` 命中，新路徑照原文的寫法框架——`./` 前綴、結尾 `/` 保留）、
  只差大小寫（候選路徑與建議 `EqualFold`，同樣照原文所在的框架：文件相對就換算成文件相對）。
  符號改名、錨點、fuzzy 建議一律不算。
- `docrot fix [dir]`：跑一次完整 check（含 git，改名表要它），收集帶 `Data["fix"]` 且
  未 baselined 的 finding，依檔案分組，每條在 `Loc.Line` 那一行從 `Loc.Col`（或行首）
  起找第一個 `Ref.Text`，換成 fix 文字。預設 dry-run：印
  `README.md:12: docs/guid.md → docs/guide.md` 與該行前後（`-`／`+` 兩行），
  最後「N fixes in M files (dry run; pass --apply to write)」。`--apply` 才寫檔，
  保留原檔的換行風格（CRLF／LF）與 BOM，同一行多個修正由右往左套。exit 0；
  沒有可修的東西時印「nothing to fix」。
- 新套件 `internal/fix`：`Plan(findings) []Edit`、`Apply(root, edits, write bool) (Summary, error)`；
  測試用暫存檔。CLI 的 `fix` 子命令與 `--apply`；`--format text|json`（json 列 edits）。

## 文件與版本

rules.md 多三條規則、commands 多一個子命令與兩種格式、how-it-works 的流程多一步、
README 兩語的檢查清單與指令段、llms.txt、configuration（severity 例子）、CHANGELOG 0.9.0、
roadmap 把 tier 2 表移到「Done in 0.9.0」、`release = "0.9.0"`、verify.py 的格式迴圈多
`github`／`junit`、tag `v0.9.0`。
