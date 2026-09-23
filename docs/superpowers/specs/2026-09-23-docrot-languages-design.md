<!-- docrot:ignore-file -->
<!-- A design document: most of the paths, kinds and packages named here do
     not exist yet, so docrot skips this file. -->
# docrot 多語言支援設計（0.5.0 – 0.8.0）

對應計畫：[`../plans/2026-09-23-docrot-languages-plan.md`](../plans/2026-09-23-docrot-languages-plan.md)

## 目標

讓 docrot 檢查 Rust、JavaScript／TypeScript、C#、C／C++ 專案的文件，精準度與現在的
Python 支援同一水準，而且**加一個語言等於加一個套件**，不再需要碰 model、extract、
resolve、engine、cmd 各一處。混合語言 repo（Go + TS + Python、Odin + C++）本來就支援，
重構後仍然支援：每個語言各自建索引，找不到的名字會去其他語言找。

非目標：真正的語法分析器。四個語言都用「行首宣告形狀」的正規表示式索引，跟 Odin 與
Python 一樣；模板、多載、巨集展開、跨行簽章一律靠 did-you-mean 與字串常值兜底。
不執行任何東西。仍然只用 Go 標準庫。

## 為什麼要先重構

現在每個非 Go 語言在七個地方各有一份 copy：`model.Kind` 常數、`model.Index` 的五個
方法（`HasOdin`、`OdinPackages`、`HasOdinSymbol`、`SimilarOdinSymbols`、`SymbolSpan`
分支）、`extract.Hints` 的兩個方法、`classify.go` 的 `symbolRef` 分支、
`resolve.go` 的 `resolveOtherSymbol`（兩兩寫死的「另一個語言可能擁有它」）、
`comments.Lookup`、`index.Build` 的 goroutine 與 `Stats` 欄位、`engine` 的統計輸出、
`cmd` 的 `--kind` 清單。三個語言撐得住，七個就是七份 copy 與 21 對「去別的語言找」。

## 架構

### `internal/index/lang`：語言索引的共同介面

```go
// Index is what every non-Go language index provides. Go keeps its own
// richer index (go/parser: packages, types, members, flags, env, tags).
type Index interface {
    Empty() bool
    Namespaces() []string                    // packages / modules / crates / namespaces, sorted
    IsNamespace(qualified string) bool       // a module or package, not a declaration
    IsExample(namespace string) bool         // lives under tests/, docs/, examples/…
    Has(qualified string) bool
    File(qualified string) (string, int, bool)
    Similar(qualified string, n int) []string
    Symbols() []string                       // every qualified declaration, sorted (docrot index)
    Span(qualified string) (model.SymbolSpan, bool)
    AllSpans() []model.SymbolSpan
    Literals() *literals.Set
    Routes() []routes.Route
    Defaults() *defaults.Set
    Stats() Stats
}

type Stats struct{ Files, Namespaces, Symbols int }
```

`odin` 與 `py` 改成滿足這個介面（結構上滿足即可，不 import `lang`）；`py` 特有的
`IsExampleModule`／`IsModule` 就是 `IsExample`／`IsNamespace`，Odin 的 `IsExample`
永遠 false。行為不變，golden test 守著。

### `internal/model`：語言表

```go
KindOdinSym   Kind = "odinsym"
KindPySym     Kind = "pysym"
KindRustSym   Kind = "rustsym"
KindJSSym     Kind = "jssym"
KindCSharpSym Kind = "cssym"
KindCSym      Kind = "csym"

// Lang describes one language whose symbols a lightweight index resolves.
type Lang struct {
    Kind   Kind
    ID     string   // "odin", "python", "rust", "js", "csharp", "c" (docrot index --kind)
    Name   string   // "Odin", "Python", "Rust", "JavaScript", "C#", "C/C++" (messages)
    Sep    string   // "." or "::" — how documents qualify names
    Naming Naming   // which bare-call spellings ("name()") belong to it
    Stdlib []string // first segments never reported: std, core, System, console…
}
var Langs = []Lang{odin, python, rust, js, csharp, c}   // fixed order = tie-break order
func LangOf(k Kind) (Lang, bool)
func (k Kind) IsSymbol() bool   // Go or any Lang
```

Kind 字串維持每語言一個（`rustsym`…）而不是一個通用 `symbol`：fingerprint 含 kind，
現有 baseline 才不會失效，`explain` 與報告也一眼看得出語言。

`model.Index` 的 Odin／Python 段落換成：

```go
Languages() []Kind                            // present languages, Langs order
HasLang(kind Kind) bool
Namespaces(kind Kind) []string
IsNamespace(kind Kind, qualified string) bool
IsExample(kind Kind, namespace string) bool
HasSymbol(kind Kind, qualified string) bool
SimilarSymbols(kind Kind, qualified string, n int) []string
```

`SymbolSpan(kind, q)` 與 `AllSpans` 內部改成迴圈。

### composite index

`Index` 持有 `langs map[model.Kind]lang.Index`；`Build` 用一張表
`{Kind, build func(root, exclude) (lang.Index, error)}` 起 goroutine，
路由、字串常值、預設值、Stats 都用迴圈彙整。`Stats` 的 `OdinFiles`… 四個欄位換成
`Langs map[model.Kind]lang.Stats`。`parsedExt` 由各語言的副檔名表組成。

### extract：命名規則表取代分支

`Hints` 換成 `Languages()`、`Namespaces(kind)`。`symbolRef` 的判斷順序：

1. `a.b[.c]`：Go 套件／型別優先（不變）；否則對每個在場語言（`Langs` 順序），
   第一段是它的 namespace → 該語言 High。
2. 全小寫 `a.b`：第一個在場、`Sep == "."`、`Naming` 含 snake 的語言 → Low（不變：
   Odin 在 Python 前）；沒有 → config key Low。
3. `Class.method`：snake 方法 → Python Medium；camel 方法 → JS Medium；
   每段都大寫開頭 → C# Medium；否則 Go Low（有 go.mod 時）。
4. `a::b[::c]`（新的 `reColons`）：第一個在場、`Sep == "::"` 的語言；第一段是它的
   namespace → High，否則 Medium。`Norm` 保留 `::`。
5. 裸呼叫 `name()`：snake → 第一個 `Naming` 含 snake 的在場語言（Odin、Python、
   Rust、C）；camelCase → JS；PascalCase → Go（有 go.mod）否則 C#。皆 Medium。

### resolve：一個通用解析器

`resolveOtherSymbol` 改名 `resolveLangSymbol`，邏輯不變但用語言表：自己的語言有 →
OK；任一其他語言有 → OK；`Type.field` 擁有者在任一語言 → OK；`owner.attr` 擁有者
存在且不是 namespace → OK；字串常值 → OK；`Stdlib` 第一段 → Skipped；Python 特有的
receiver／example 規則保留在 `if lg.Kind == KindPySym`；「模組存在但名字搬家」的
warning 改用 `IsNamespace` 與 `Sep`，Rust 的 `crate::io::read` 同樣受惠。

### comments、engine、cmd

`comments.Lookup` 改用 `Languages`／`HasSymbol`；`engine` 的 `isSym` 用
`Kind.IsSymbol()`，統計輸出 `for kind, st := range Langs`；`docrot index --kind <ID>`
列出該語言的 `Symbols()`。

## 各語言

每個語言一個套件 `internal/index/<id>`，一個 fixture 子樹 `testdata/fixture/<dir>`
種了錯誤的文件行，一段實地報告。上線標準與 Python 相同：fixture 種的錯誤全抓到，
兩三個真實 repo 的誤報看過一輪並寫進報告。

### Rust（0.5.0）

- 宣告：`pub fn`、`pub struct`、`pub enum`、`pub trait`、`pub type`、`pub const`、
  `pub static`、`pub mod`、`macro_rules!`、`impl [Trait for] Type {` 底下縮排的
  `pub fn` → `Type::method`；`pub(crate)` 視同 pub；私有項目也索引但 `Exported=false`。
  屬性行（`#[…]`）與 doc comment（`///`、`//!`）算進 span。
- 模組：`src/lib.rs`／`src/main.rs` 是 crate 根；`src/foo.rs` 與 `src/foo/mod.rs` 是
  `foo`；子目錄依此類推。Namespace 名字含 crate 名（`Cargo.toml` 的 `[package] name`，
  `-` 換成 `_`）與 `crate`。
- 文件寫法：`Foo::new()`、`foo::bar`、`crate::io::read`、`mycrate::Foo`。
- 額外主張：clap 的 `#[arg(long, short, env = "PORT", default_value = "8080")]`、
  `#[arg(long = "name")]` → flag、env、default；`std::env::var("X")`、`env!("X")`、
  `option_env!` → env；axum `.route("/x", get(h))`、actix `#[get("/x")]`、
  `web::resource("/x")`、rocket `#[get("/x")]` → route；`Cargo.toml` → `project.Targets`
  無，`install` 的 `cargo add <name>`／`cargo install <name>` 對 `[package] name`；
  `rust-version = "1.70"` → toolchain。
- 跳過：`std`、`core`、`alloc`、`proc_macro`、`test`。

### JavaScript／TypeScript（0.6.0）

- 宣告：`export [default] [async] function name`、`export const|let|var name =`、
  `export class Name`、`export interface|type|enum Name`、`export { a, b }`、
  `module.exports = {…}`／`exports.name =`、class 內縮排的 `[static] [async] name(`
  與 `get name(` → `Class.method`；未 export 的頂層 `function`／`class` 也索引但
  `Exported=false`。副檔名 `.js .mjs .cjs .jsx .ts .tsx`；跳過 `*.d.ts`、`*.min.js`、
  `dist/`、`build/`、`node_modules/`。
- Namespace：檔案 stem（`utils`、`client`），`index.*` 用目錄名。
- 文件寫法：`useFoo()`、`client.query()`、`Foo.bar()`、`import { x } from './lib/foo'`。
- 額外主張：`import … from './x'`／`require('./x')` 的相對路徑要解析到
  `x.{js,ts,tsx,jsx,mjs,cjs}` 或 `x/index.*`（新 `missing-import` 的 JS 版）；
  `process.env.X`、`import.meta.env.X` → env；express／koa／hono／fastify／NestJS 的
  `app.get('/x'`、`router.post('/x'`、`@Get('/x')` → route；commander
  `.option('--port <n>', '…', 8080)`、yargs `.option('port', { default: 8080 })` → flag
  與 default；`package.json` 的 `engines.node` → toolchain。
- 跳過：`console`、`document`、`window`、`process`、`Math`、`JSON`、`Object`、
  `Array`、`Promise`、`Buffer`、`fetch`、`require`、`module`、`globalThis`。

### C#（0.7.0）

- 宣告：`namespace A.B;`／`namespace A.B {`、`[modifiers] class|struct|interface|enum|record Name`、
  `[modifiers] RET Name(` 方法、`[modifiers] TYPE Name { get;` 屬性、`const`、
  `event`；泛型 `Name<T>` 去掉 `<…>`。巢狀類別用縮排堆疊。`public`／`internal` 決定
  `Exported`。副檔名 `.cs`；跳過 `bin/`、`obj/`、`*.Designer.cs`、`*.g.cs`。
- Namespace：`namespace` 宣告（含點）；文件常省略 namespace 只寫 `Class.Method`。
- 額外主張：`app.MapGet("/x"`、`[HttpGet("/x")]`、`[Route("api/[controller]")]`
  （`[controller]` 代換成類別名去掉 `Controller`）→ route；
  `Environment.GetEnvironmentVariable("X")` → env；`Configuration["A:B"]`、
  `GetSection("A").GetValue<int>("B")` → config key（`:` 正規化成 `.`），
  `appsettings*.json` 加進預設 `configSamples`；`dotnet add package X` 對 `.csproj`
  的 `PackageId`／專案名；`<TargetFramework>net8.0</TargetFramework>` → toolchain
  （".NET 8"）；System.CommandLine 的 `new Option<int>("--port", () => 8080)` → flag
  與 default。
- 跳過：`System`、`Microsoft`、`Newtonsoft`、`Console`、`Task`、`String`、`Int32`。

### C／C++（0.8.0）

- 宣告：標頭檔（`.h .hpp .hh .hxx`）是公開介面：行首的 `RET name(` 原型（含
  `static inline`、`extern "C"`、指標回傳）、`#define NAME`（含函式巨集）、
  `typedef … name;`、`struct|union|enum|class name {`、`namespace a::b {`、
  `using name =`；`.c .cc .cpp .cxx` 只抓 `RET Class::method(` 定義與非 static 的
  函式定義。宣告與定義同名只算一個。C++ class 內縮排的方法 → `Class::method`。
  跳過 `build/`、`cmake-build-*/`、`third_party/`、`vendor/`。
- Namespace：C 沒有；C++ 的 `namespace` 與 class。文件寫法：`foo_init()`、
  `FOO_MAX`、`Foo::Bar`、`ns::func()`、`#include "foo/bar.h"`。
- 額外主張：`#include "…"` 在 code fence 裡 → path（相對於 repo 根、`include/`、
  `src/`）；`CMakeLists.txt` 的 `add_executable`／`add_library`／`add_custom_target`
  → target（`cmake --build --target x`、`make x`）；`option(FOO "…" ON)` 與
  `set(FOO x CACHE …)` → config key 與 default；`project(name VERSION x)` 給名字；
  `cmake_minimum_required(VERSION 3.20)` → toolchain（"CMake 3.20"）；`getenv("X")`
  → env；`getopt_long` 的 `{"port", …}` 表格、CLI11 `add_option("--port"`、
  `cxxopts` `("p,port", …)` → flag。
- 跳過：`std`、`boost`、`__builtin_*`、`NULL`、`size_t` 等關鍵字與標準型別，
  `<…>` 系統標頭不檢查。

## 混合語言的規則

- 每個語言只索引自己的副檔名；同一 repo 的六個索引並行建。
- 分類器依 `Langs` 順序給名字一個語言；解析器找不到時問其他每個語言，找到就算通過，
  所以分類猜錯語言不會誤報，只會在真的不存在時用猜的語言命名訊息。
- `docrot index --kind rust|js|csharp|c` 各自列出；報告摘要每個語言一行
  `<id> symbols: N`。
- 大小限制 `maxFileMB` 對所有語言的原始碼一體適用。

## 版本

| 版本 | 內容 |
|---|---|
| 0.5.0 | 語言外掛重構（行為不變）、`::` 名字、Rust |
| 0.6.0 | JavaScript／TypeScript |
| 0.7.0 | C# |
| 0.8.0 | C／C++ |

每個版本：CHANGELOG、rules.md 的格式與規則表、how-it-works、llms.txt、README 兩語、
實地報告（新語言的真實 repo 至少兩個；meowboard 同時覆蓋 Odin、Python、TS、C++，
是混合語言的驗收場）、`scripts/verify.py` 綠、自檢 0 error 0 warning。
