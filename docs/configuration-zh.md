# 設定

**繁體中文** · [English](configuration.md)

## `.docrot.json`

`docrot init` 會寫出帶預設值的 `.docrot.json`；設定檔只需要寫有改動的鍵。不認識的鍵會被拒絕，
所以打錯字會被報出來，而不是被忽略。

```json
{
  "docs": ["**/*.md", "**/*.rst", "**/*.adoc", "llms.txt"],
  "exclude": ["vendor/**", "node_modules/**", "third_party/**", "3rdparty/**", "external/**", "**/testdata/**", "dist/**", ".git/**", ".*/**"],
  "ignore": [],
  "siblings": [],
  "pairs": [],
  "pairPatterns": ["{stem}-zh.md", "{stem}_zh.md", "{stem}.zh.md", "{stem}.zh-TW.md", "{stem}-zh-TW.md"],
  "configSamples": ["config.json", "config*.json", "*.example.json", "*.sample.json", "configs/**/*.json"],
  "stale": { "enabled": true, "minChurn": 3, "minDays": 90, "exclude": ["CHANGELOG*.md", "CHANGES*.md", "HISTORY*.md", "NEWS*.md", "RELEASE*.md", "**/release-notes*.md", "**/release_notes*.md", "**/releases/**", "**/superpowers/**", "**/specs/**", "**/plans/**", "**/research/**", "**/deep-research/**", "**/*-report.md", "**/adr/**"] },
  "coverage": { "report": false, "includeInternal": false },
  "severity": { "stale-section": "warning", "stale-symbol": "warning", "pair-lag": "warning", "pair-number": "info", "stale-comment": "info", "comment-mentions-missing": "warning" },
  "net": false,
  "failOn": "error",
  "minConfidence": "low",
  "outDir": ".docrot",
  "maxFileMB": 8,
  "comments": { "enabled": true, "minChurn": 2, "minFrac": 0.5 }
}
```

- `docs` 選出要檢查的文件；`exclude` 列出永遠不走訪、不索引的路徑。兩者都是支援 `**` 的 glob。
  `docs` 納入的 `.txt` 檔若看起來像 reStructuredText，就當 reStructuredText 讀（Django 的
  `docs/**/*.txt`）。
- `ignore` 是套用在引用文字上的正規表示式。
- `siblings` 列出其他 repo（相對於根目錄），這裡找不到的路徑可能合理地住在那邊——
  服務文件裡引用它所依賴的函式庫時很有用。
- `pairs` 明確指定原文與翻譯的配對；`pairPatterns` 由原文檔名推導翻譯（`{stem}` 是去掉副檔名的
  檔名）。`docs/en/…` ↔ `docs/<lang>/…` 這種目錄結構不用設定就會被偵測到。 <!-- docrot:ignore missing-path -->
- `configSamples` 是用來挖設定鍵的 JSON 檔。
- `stale.minChurn` 與 `stale.minDays` 是過期判定的門檻；`stale.exclude` 把有日期的文件（changelog、
  release notes、設計規格）排除在過期分析與工具鏈版本檢查之外，它們本質上是歷史紀錄。
  `stale.enabled: false` 整個關掉依賴 git 的過期規則。
- `coverage.report` 讓每次執行都附上 `undocumented` finding；`includeInternal` 把 `internal/`
  套件也算進去。
- `severity` 覆蓋某條規則的等級，例如 `{"stale-section": "info"}`；`none` 直接關掉那條規則。
- `net` 開啟外部 URL 檢查（也可以用 `--net`）。
- `failOn` 是讓 `docrot check` 回傳 exit 1 的最低嚴重度；`minConfidence` 丟掉低於某個信心值的引用。
- `outDir` 是每次執行都會重寫的輸出目錄；見下。
- `maxFileMB` 限制 docrot 會「讀內容」的檔案大小（文件、Go／Odin／Python 原始碼、JSON 樣本）。
  二進位檔完全不會被打開——只有檔名進入路徑索引，所以一個 4 GB 的模型檔不論有沒有被
  gitignore 都只花一筆目錄項目。超過上限的文字檔會跳過並警告；指向它的路徑仍然能解析。
- `comments` 調整程式碼註解檢查，它會對文件指到的每個符號執行：註解之後有 `minChurn` 個新 commit
  （或一個 commit 改寫了本體 `minFrac` 的比例）就算「過期」；`docrot comments` 對所有 exported 宣告跑同一套檢查。

## 在原地壓掉一個 finding

```markdown
<!-- docrot:ignore -->            the next non-blank line
inline text <!-- docrot:ignore -->  this line
<!-- docrot:ignore-start --> … <!-- docrot:ignore-end -->
<!-- docrot:ignore-file -->
<!-- docrot:ignore missing-path unknown-flag -->   only these rules (also with ignore-start)
```

reStructuredText 用 `.. docrot:ignore` 註解，AsciiDoc 用 `// docrot:ignore`，形式相同。
要用樣式比對就把正規表示式加進 `ignore`；要改某條規則的等級就設 `severity`。

## 輸出目錄

每次 `docrot check` 都會重寫一個目錄——由 `outDir` 設定指定——讓人與 agent
永遠在同一個地方找到最新的報告：

```text
.docrot/.gitignore   a single "*", so the reports never reach a commit
.docrot/report.md    for agents: findings by file, how to read them, a checklist
.docrot/report.html  for humans: the filterable single-file page
.docrot/report.json  the stable JSON schema
.docrot/report.txt   the terminal report, with info findings
.docrot/git-cache.json  blame and log answers of the last run (speed only; safe to delete)
```

每個檔案都先寫到暫存檔再改名就位，所以中途被打斷的執行，不會在下一個讀者期待
完整報告的地方留下半份。這個目錄會被排除在文件探索之外，所以昨天的報告永遠不會
被當成文件來檢查。用 `--out-dir` 換個地方寫，用 `--no-out` 讓這次不寫，或把
`outDir` 設成空字串永久關掉。

## git 快取

`git-cache.json` 存的是上一次執行的 blame 與 log 結果：blame 以檔案在 HEAD 的 blob hash 為鍵
（有未提交修改的檔案永遠不快取），log 以 HEAD 為鍵，而且只寫回這次用到的項目。大型 repo
第二次執行只要十分之一的時間就是靠它；有沒有快取 finding 都一樣，刪掉也只是損失這點速度。
`--no-out` 會連同報告一起停用快取。
