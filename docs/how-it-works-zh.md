# docrot 怎麼判斷

**繁體中文** · [English](how-it-works.md)

## 處理流程

1. **Tokenize** 每份文件：標題、fenced 區塊、inline code span、連結、圖片、表格、註解。
   Markdown、reStructuredText（Sphinx 的 role、directive、toctree、label；`.txt` 來源也行）與
   AsciiDoc 都讀成同一種結構。不依賴任何 CommonMark 實作。
2. **抽取** code span、連結目標、shell 區塊、Go 區塊、C／C++ 區塊（`#include`）與 JavaScript 區塊裡的引用
   （JavaScript 區塊的 import 也告訴我們哪些名字屬於本套件、哪些屬於別人）。每個引用都有*種類*與*信心值*：
   有目錄有副檔名的路徑是 high；只有檔名是 medium；第一段是不認識的小寫字的點號名稱
   （`app.Run`）是 low，永遠不會被報出來。
3. **建索引**，整個 repo 只做一次：檔案樹、Go 套件／符號／flag／環境變數／tag（`go/parser`）、
   Odin、Python、Rust、JavaScript／TypeScript、C# 與 C／C++ 宣告（逐行樣式）、HTTP 路由註冊、Markdown 錨點、JSON 樣本鍵、每一個像識別字的
   字串常值，以及各種 manifest（`go.mod`、`pyproject.toml`、`package.json`、`Cargo.toml`、`*.csproj`、`CMakeLists.txt`、Makefile、justfile、Taskfile）。
   Go 以外的每個語言都走同一個介面（`internal/index/lang`）加 `model.Langs` 表裡的一列：
   抽取器的命名規則、解析器與 CLI 都是對這張表迴圈。
4. **解析**每個引用，產生附「你是不是想找」建議的 finding（在正確的候選集合上做
   Damerau-Levenshtein，路徑另外參考 git 改名歷史）。
5. **過期判定**：`git blame` 給每個章節一個編輯時間；`git log` 數出那之後每個被引用檔案的 commit 數
   （文件自己的翻譯不算，那是雙語配對規則的事），
   被引用的宣告本身也用 blame 看它的本體有沒有繼續變動。blame 與 log 的答案會快取在輸出目錄裡，下次沿用。
6. **雙語配對**：對兩份文件的結構指紋做 diff；`docs/en/` ↔ `docs/<lang>/` 這種樹另外檢查兩邊 <!-- docrot:ignore missing-path -->
   各自缺了哪些頁。
7. **註解**：文件指到的每個符號，其 doc comment／docstring 用同一套方法檢查——引用的名字必須存在，
   註解修改之後本體大幅變動也會標出來。
8. **Baseline**：指紋不含行號，所以一般編輯不會讓 baseline 失效。
9. **範例**：抽取的同時，每個 ```` ```go ```` 區塊都用 `go/parser` 以片段可能的幾種形狀
   （整檔、陳述句、struct 欄位……）試著 parse；哪種都不是的區塊會被報出來。

## 信心值與嚴重度

嚴重度跟著信心值走：high → error、medium → warning、low → info。flag 與環境變數再放寬一級，
因為它們太常是在講*別的*程式；glob 沒命中、內文裡的設定鍵與只有檔名的路徑一律 info。文字報告
預設隱藏 info，加 `--info` 才列出；JSON、SARIF、HTML 與 Markdown 報告則一律包含，GitHub 標註與
JUnit 輸出跟文字報告一樣。
`docrot explain <doc>` 會列出每個引用拿到的信心值。

## 刻意忽略的東西

docrot 是在真實 repo 上調校的，不是合成範例。它刻意忽略的東西：被 `.gitignore` 排除的路徑
（建置產物）、`health/ready` 或 `net/http` 這類散文、外部程式後面的 flag（`go test -race`）、
所在行完全沒提到環境的 `UPPER_SNAKE` 字、`cfg` 同時是套件又是變數時的 `cfg.Addr`，以及
`Type.Method`、`--flag`、`path/to/file` 這類示意用名稱。最後一道防線：程式碼裡以字串常值拼出來的
東西——`"request_id"`、`"X-Request-ID"`、`"/openapi.json"`——一律視為存在，log 欄位、header 名稱、
用常數註冊的路由因此不會被誤報。

本質上是歷史的文件——changelog、release notes、研究筆記、有日期的規格與計畫（`stale.exclude`）——
不做過期判定，也不檢查工具鏈版本；但它們提到的已移除符號仍然會被檢查，因為那仍然是文件。

## 大小寫

大小寫在每個平台上都嚴格比對。檔案叫 `Docs/Foo.md` 而文件寫 `docs/foo.md`，在 Windows
與 macOS 上一樣會得到 finding，訊息寫明「只差大小寫」——因為這種連結在作者的筆電上能開，
到 Linux CI 就壞。文件探索本身則不分大小寫，所以 `README.MD` 與 `readme.md` 都會被掃描。

## 啟發式規則從哪裡來

八份實地報告記錄了每一輪調校：第一次跑在八個 Go／Odin repo、七個 Python 專案、三個 Rust crate、三個 JavaScript／TypeScript 專案、三個 C# 專案、三個 C／C++ 專案、Go 範例檢查在五個 Go repo 的 1,570 個範例區塊，以及一次以「修掉」而非「量測」為目的、掃完 meowbase 五個 repo 的全程巡檢，各自上報了什麼，哪些是真的、哪些是噪音、又是哪條規則消掉了哪一類噪音。見 [field-report.md](field-report.md)、[field-report-python.md](field-report-python.md)、[field-report-rust.md](field-report-rust.md)、[field-report-js.md](field-report-js.md)、[field-report-csharp.md](field-report-csharp.md)、[field-report-c.md](field-report-c.md)、[field-report-examples.md](field-report-examples.md) 與 [field-report-sweep.md](field-report-sweep.md)；每條規則的確切行為在 [rules.md](rules.md)，每一條因實地跑而改動的規則都記在 `CHANGELOG.md`。
