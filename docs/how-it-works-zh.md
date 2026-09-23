# docrot 怎麼判斷

**繁體中文** · [English](how-it-works.md)

## 處理流程

1. **Tokenize** 每份文件：標題、fenced 區塊、inline code span、連結、圖片、表格、註解。
   Markdown、reStructuredText（Sphinx 的 role、directive、toctree、label；`.txt` 來源也行）與
   AsciiDoc 都讀成同一種結構。不依賴任何 CommonMark 實作。
2. **抽取** code span、連結目標、shell 區塊與 Go 區塊裡的引用。每個引用都有*種類*與*信心值*：
   有目錄有副檔名的路徑是 high；只有檔名是 medium；第一段是不認識的小寫字的點號名稱
   （`app.Run`）是 low，永遠不會被報出來。
3. **建索引**，整個 repo 只做一次：檔案樹、Go 套件／符號／flag／環境變數／tag（`go/parser`）、
   Odin 與 Python 宣告（regex）、HTTP 路由註冊、Markdown 錨點、JSON 樣本鍵、每一個像識別字的
   字串常值，以及各種 manifest（`go.mod`、`pyproject.toml`、`package.json`、Makefile、justfile、Taskfile）。
4. **解析**每個引用，產生附「你是不是想找」建議的 finding（在正確的候選集合上做
   Damerau-Levenshtein，路徑另外參考 git 改名歷史）。
5. **過期判定**：`git blame` 給每個章節一個編輯時間；`git log` 數出那之後每個被引用檔案的 commit 數，
   被引用的宣告本身也用 blame 看它的本體有沒有繼續變動。blame 與 log 的答案會快取在輸出目錄裡，下次沿用。
6. **雙語配對**：對兩份文件的結構指紋做 diff。
7. **註解**：文件指到的每個符號，其 doc comment／docstring 用同一套方法檢查——引用的名字必須存在，
   註解修改之後本體大幅變動也會標出來。
8. **Baseline**：指紋不含行號，所以一般編輯不會讓 baseline 失效。

## 信心值與嚴重度

嚴重度跟著信心值走：high → error、medium → warning、low → info。flag 與環境變數再放寬一級，
因為它們太常是在講*別的*程式；glob 沒命中、內文裡的設定鍵與只有檔名的路徑一律 info。文字報告
預設隱藏 info，加 `--info` 才列出；JSON、SARIF、HTML 與 Markdown 報告則一律包含。
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

兩份實地報告記錄了每一輪調校：第一次在八個 Go／Odin repo 與七個 Python 專案上跑出什麼、
哪些是真的、哪些是噪音、以及消除每一類噪音的規則。見 [field-report.md](field-report.md) 與
[field-report-python.md](field-report-python.md)；每條規則的精確行為在 [rules.md](rules.md)。
