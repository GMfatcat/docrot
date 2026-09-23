<!-- docrot:ignore-file -->
<!-- An implementation plan: it names packages and paths that do not exist
     yet, so docrot skips this file. -->
# docrot 多語言實作計畫

對應規格：[`../specs/2026-09-23-docrot-languages-design.md`](../specs/2026-09-23-docrot-languages-design.md)

原則同主計畫：只用標準庫；每個套件獨立可測；每個波次各自 commit；
`python scripts/verify.py` 綠、自檢 0 error 0 warning 才算完成。

## Wave 0 — 語言外掛重構（0.5.0 前半，行為不變）— 已完成

- [x] 0A `internal/index/lang`：`Index` 介面、`Stats`。
- [x] 0B `internal/model`：`Lang`、`Langs`、`LangOf`、`Kind.IsSymbol`、新 Kind 常數；
      `Index` 介面換成 `Languages`／`HasLang`／`Namespaces`／`IsNamespace`／`IsExample`／
      `HasSymbol`／`SimilarSymbols`。
- [x] 0C `internal/index/odin`、`internal/index/py` 滿足 `lang.Index`（`IsNamespace`、
      `IsExample`、`Symbols`、`Defaults`／`Routes` 的空實作）。
- [x] 0D `internal/index`：`langs` 表、`Build` 迴圈、`Stats.Langs`、`SymbolSpan`／
      `AllSpans`／`Symbols(kind)` 迴圈、`parsedExt` 由語言表組成。
- [x] 0E `internal/extract`：`Hints` 換介面；`symbolRef` 改用命名規則表；`reColons`。
- [x] 0F `internal/resolve`：`resolveLangSymbol`；`comments.Lookup`；`engine` 統計；
      `cmd` 的 `--kind`。
- [x] 0G 全部測試改用新 API；golden test 不變；verify 綠。commit。

## Wave 1 — Rust（0.5.0 後半）— 已完成

- [x] 1A `internal/index/rust`：宣告、模組樹、`impl` 方法、span（`///`、`#[…]`）、
      字串常值、clap 屬性 → flag／env／default、`std::env::var`、axum／actix／rocket 路由。
- [x] 1B `internal/index/project`：`Cargo.toml` 的 `[package] name`、`rust-version`；
      `resolveInstall` 的 `cargo add`／`cargo install`；`toolchainRefs` 認得 "Rust 1.70"。
- [x] 1C fixture：`testdata/fixture/` 根目錄的 `Cargo.toml` 與 `src/`（lib.rs、一個模組、一個 impl、
      clap 選項、兩條 axum 路由）與 README 裡種的錯誤行；golden test 更新。
- [x] 1D 實地：shallow clone 三個 Rust repo 到 `../surprise-rust/`（ripgrep、axum、clap），
      `docrot explain` 看噪音，調整；寫 `docs/field-report-rust.md`。
- [x] 1E 文件：rules.md、how-it-works、llms.txt、README 兩語、CHANGELOG 0.5.0；
      `release = "0.5.0"`；tag。

## Wave 2 — JavaScript／TypeScript（0.6.0）— 已完成

- [x] 2A `internal/index/js`：宣告、class 方法、`module.exports`、副檔名與排除、
      字串常值、`process.env`、express／hono／fastify／NestJS 路由、commander／yargs
      預設值。
- [x] 2B `internal/extract`＋`resolve`：code fence 裡 `import … from './x'`／
      `require('./x')` 的相對路徑解析（`.js .ts .tsx .jsx .mjs .cjs`、`index.*`）。
- [x] 2C `project`：`package.json` 的 `engines.node` → toolchain。
- [x] 2D fixture `testdata/fixture/web/` 與根目錄 `package.json`；golden test。
- [x] 2E 實地：fastify、hono、zod（meowboard 的 TS 全在 `third_party/`，預設排除）；
      `docs/field-report-js.md`。
- [x] 2F 文件與 0.6.0。

## Wave 3 — C#（0.7.0）— 已完成

- [x] 3A `internal/index/csharp`：namespace、型別、方法、屬性、泛型去角括號、巢狀
      類別、`Exported`；ASP.NET 路由含 `[controller]`；`GetEnvironmentVariable`；
      `Configuration["A:B"]` → config key；System.CommandLine 選項與預設值。
- [x] 3B `config`：預設 `configSamples` 加 `appsettings*.json`；`project`：`.csproj`
      的名字與 `TargetFramework`；`dotnet add package`。
- [x] 3C fixture `testdata/fixture/dotnet/`；golden test。
- [x] 3D 實地：Polly、Humanizer（含一千多頁產生的 API 文件）、TodoApi（minimal API）；
      `docs/field-report-csharp.md`。
- [x] 3E 文件與 0.7.0。

## Wave 4 — C／C++（0.8.0）— 已完成

- [x] 4A `internal/index/c`：標頭原型、巨集、typedef、struct／enum／class、namespace、
      `Class::method` 定義、排除目錄；`getenv`；getopt／CLI11／cxxopts flag。
- [x] 4B `extract`＋`resolve`：fence 裡 `#include "…"` → path（根、`include/`、`src/`）。
- [x] 4C `project`：`CMakeLists.txt` 的 target、`option()`／`set(… CACHE)` →
      config key 與 default、`project()` 名字、`cmake_minimum_required` → toolchain。
- [x] 4D fixture `testdata/fixture/native/`；golden test。
- [x] 4E 實地：curl、nlohmann/json、CLI11（meowboard 的 C++ 幾乎全在 `third_party/`）；`docs/field-report-c.md`。
- [x] 4F 文件與 0.8.0；roadmap 更新。
