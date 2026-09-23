<!-- docrot:ignore-file -->
<!-- An implementation plan: it names rules, flags and packages that do not
     exist yet, so docrot skips this file. -->
# docrot 0.9.0 實作計畫：roadmap tier 2

對應規格：[`../specs/2026-09-24-docrot-tier2-design.md`](../specs/2026-09-24-docrot-tier2-design.md)

原則同前：只用標準庫；每個套件獨立可測；每個波次各自 commit；
`python scripts/verify.py` 綠、自檢 0 error 0 warning 才算完成。五個波次彼此獨立，
依成本由低到高排。

## Wave A — `--format github` 與 `--format junit`

- [x] A1 `internal/report/github.go`：`WriteGitHub`，workflow command 逸出（`%`、`\r`、`\n`、
      `,`、`:` 在屬性裡）；baselined 不輸出；結尾 notice 摘要。
- [x] A2 `internal/report/junit.go`：`WriteJUnit`，`encoding/xml`；testcase = 文件 × 規則；
      info → skipped；乾淨文件一個 ok testcase。
- [x] A3 `report.Formats`、`Write` 的 case；`cmd` 的 `--format` 說明；main_test 的格式迴圈；
      verify.py 的格式迴圈（junit 要能 `xml.etree` parse、github 每行以 `::` 開頭）。
- [x] A4 文件：commands 兩語、README 兩語的 CI 段多一行 `--format github`、llms.txt。

## Wave B — `pair-missing` 與 `pair-orphan`

- [x] B1 `internal/pairs/gaps.go`：`Gaps(docs, prs, opts) []model.Finding`；由目錄式配對
      推出翻譯集合，來源樹缺翻譯 → missing，翻譯樹缺來源 → orphan。
- [x] B2 `model`：兩個 rule 常數、`AllRules`、`RuleDescriptions`；`pairs.defaultSeverity`；
      `config.Default().Severity` 加 `pair-missing: info`。
- [x] B3 engine 第 6 步呼叫 `Gaps`；`--changed` 時只保留有一側被改到的 finding。
- [x] B4 fixture：`docs/en/intro.md` + `docs/zh/intro.md`（一對）、`docs/en/extra.md`（漏翻）、
      `docs/zh/old.md`（孤兒）；golden wants 與 `Docs` 計數更新。
- [x] B5 文件：rules.md 配對表兩列、how-it-works 兩語第 6 步、README 兩語配對那一點。

## Wave C — `example-syntax`

- [x] C1 `internal/examples/go.go`：四種包法、略過規則、錯誤行換算、fingerprint；測試涵蓋
      完整檔、片段、struct 片段、省略號、資訊字串。
- [x] C2 `model`：rule 常數、描述；engine 在 extract 之後對每份 checked 文件呼叫；
      `dropRuleIgnored` 自然生效。
- [x] C3 實地：`docs/field-report-examples.md`（短）——meowbase、meowbase-rpc、
      meowbase-sqlite、meowbase-web、meowshare 的首輪數字、噪音類型、調整後的數字。
- [x] C4 fixture README 加一個少了右大括號的 Go 區塊；golden want；rules.md「Code examples」
      一節；how-it-works 兩語；README 兩語。

## Wave D — 路由與設定鍵覆蓋率

- [x] D1 `coverage.Result` 加 `Routes`、`Configs`；`coverage.Routes(listed, documented)`、
      `coverage.Configs(keys, mentioned)`（只算葉節點）；`Findings` 多兩類；測試。
- [x] D2 engine 第 7 步：由 `mentioned` 的 `route|`／`configkey|`／`default` 鍵算出
      documented 集合（`routes.Normalize`）；`toReportCoverage` 帶過去。
- [x] D3 report：`Coverage` 結構加 `Routes`、`Configs`；text、md、html、json 各多兩列；
      `docrot coverage` 表多兩行；report 測試。
- [x] D4 文件：rules.md `undocumented` 那列、commands 的 coverage 說明、README 兩語的
      覆蓋率那一點、llms.txt。

## Wave E — `docrot fix`

- [ ] E1 resolver：改名命中與大小寫命中時填 `Data["fix"]`（依原文框架換算）；測試兩種框架。
- [ ] E2 `internal/fix`：`Edit{File, Line, Col, Old, New}`、`Plan(findings)`、
      `Apply(root, edits, write)`；同行多處由右往左；CRLF／BOM 保留；測試用暫存目錄。
- [ ] E3 `cmd`：`fix [dir] [--apply] [--format text|json]`；usage；main_test（dry-run 不改檔、
      `--apply` 改檔）。
- [ ] E4 文件：commands 兩語、rules.md 一段「What `docrot fix` applies」、README 兩語 quick
      start 多一行、llms.txt。

## 收尾

- [ ] F1 CHANGELOG 0.9.0；roadmap tier 2 表移到「Done in 0.9.0」並記錄教訓；
      `release = "0.9.0"`；`scripts/screenshots.py` 重跑；verify 綠；自檢 0/0；tag `v0.9.0`。
