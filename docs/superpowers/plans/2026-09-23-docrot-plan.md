# docrot 實作計畫

對應規格：[`../specs/2026-09-23-docrot-design.md`](../specs/2026-09-23-docrot-design.md)

原則：

- 只用 Go 標準庫；每個套件獨立可測；`internal/model` 是唯一共享契約，先定案再開工。
- 波次（wave）之間有依賴，波次之內的任務彼此獨立，可平行由 subagent 實作。
- 每個任務完成條件：`go vet ./...` 乾淨、`go test ./internal/<pkg>/...` 全綠、有 table-driven 測試、有套件 doc comment。
- 每個波次結束各自 commit。

## Wave 0 — 契約（已完成）

- [x] `internal/model`：Kind / Confidence / Severity / Location / Reference / Finding / Index 介面 / Exported / Fingerprint。

## Wave 1 — 葉節點套件（彼此獨立，只依賴 model）— 已完成

| # | 套件 | 重點 |
|---|---|---|
| 1A | `internal/globx`, `internal/index/files`, `internal/config` | doublestar glob；檔案樹索引與相似路徑；`.docrot.json` 載入/預設/驗證 |
| 1B | `internal/markdown`, `internal/index/anchors` | 行導向 tokenizer（heading / fence / span / link / image / reflink / table / HTML 註解 / ignore 指令）；GitHub slug（含 CJK、重複 `-n`） |
| 1C | `internal/index/gosym` | go/parser 索引：packages、symbols、methods、fields、flags、env、json tag 路徑、exported 列表、相似符號 |
| 1D | `internal/index/odin`, `internal/index/py`, `internal/index/config` | regex 索引 Odin / Python；JSON 樣本 dotted key |
| 1E | `internal/gitx` | os/exec 包 git：可用性偵測、檔案最後 commit 時間、since 計數、blame 逐行時間、rename map、commits since（給 pair-lag）；快取；並發上限；ErrUnavailable |
| 1F | `internal/report`, `internal/baseline`, `internal/fuzzy` | text（ANSI、NO_COLOR）/ json / sarif 2.1.0 / html 單檔；baseline 讀寫與 diff；Damerau-Levenshtein 與候選排序 |

## Wave 2 — 組合層 — 已完成

| # | 套件 | 依賴 |
|---|---|---|
| 2A | `internal/extract` | markdown, model（package 名 / 型別名透過 `model.Index` 查） |
| 2B | `internal/index`（composite，實作 `model.Index`） + `internal/resolve` | 1A–1D, fuzzy |
| 2C | `internal/stale`, `internal/pairs`, `internal/coverage` | gitx, markdown, model |

## Wave 3 — 產品化 — 已完成

- [x] 3A `internal/engine`：串接、併發、統計、baseline 過濾、severity 覆蓋、`--min-confidence`、`--net`。
- [x] 3B `cmd/docrot`：子命令 check / baseline / coverage / pairs / explain / index / init / version；exit code。
- [x] 3C `testdata/fixture` 多語言 fixture repo + engine golden 測試。
- [x] 3D `scripts/verify.py`（vet/test/build/dogfood）、`scripts/demo.py`。
- [x] 3E 文件：`README.md`、`README-zh.md`、`CHANGELOG.md`、`docs/rules.md`、`.docrot.json`、`llms.txt`。
- [x] 3F 對 `../meowbase` 等八個真實 repo 跑了六輪，把誤判回饋到 extract／resolve 啟發式；整理成 `docs/field-report.md`。

## 驗收

- [x] `python scripts/verify.py` 全綠。
- [x] `docrot check` 對自身 repo：0 error（`docs/superpowers/**` 因示意路徑而排除）。
- [x] 對 meowbase：1.4 秒跑完、報告可讀；抽樣 error 級 finding 大多為真實過期（見實地報告）。

## 0.2.0 之後

後續候選功能與取捨整理在 `docs/roadmap.md`（路由檢查、字串常值索引、JSON 範例對照、RST 輸入、`--changed`、`fix --dry-run` 等）。

## 事後補充（與原計畫的差異）

- 新增 `siblings` 設定：多 repo 生態（服務文件引用函式庫路徑）沒有它會有整批假警報。
- 新增 `stale.exclude` 與檔案級（非目錄）引用限制：否則 CHANGELOG 與「套件一覽」章節永遠過期。
- 文字報告預設隱藏 info：真實 repo 上 info 是 error 的 4–10 倍，全部列出會淹沒訊號。
- `.gitignore` 感知：`git check-ignore` 一次批次查詢，建置產物不再被當成失蹤檔案。
