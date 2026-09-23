# docrot rules

Every finding carries a rule id. This page explains what each rule means,
how docrot decided, and how to make the finding go away.

Severity for the `missing-*` / `unknown-*` / `broken-*` rules follows the
extractor's confidence: **high → error**, **medium → warning**, **low → info**.
Confidence depends on *how unambiguous the text is*, not on how sure docrot is
that the thing is missing. `docrot explain <doc>` shows the confidence assigned
to every extracted reference. The text report hides info-level findings unless
you pass `--info`; JSON, SARIF and HTML always include them.

To silence a single false positive, add an HTML comment:

```markdown
<!-- docrot:ignore -->
The next non-blank line is ignored.

Inline: `legacy/thing.go` <!-- docrot:ignore -->

<!-- docrot:ignore-start -->
Everything here is ignored.
<!-- docrot:ignore-end -->
```

A directive may name the rules it silences, so the rest of the line is
still checked: `<!-- docrot:ignore missing-path -->`,
`<!-- docrot:ignore-start unknown-flag, unknown-env -->` … `<!-- docrot:ignore-end -->`.

`<!-- docrot:ignore-file -->` anywhere in a file skips the whole file. For
patterns, add regular expressions to `ignore` in `.docrot.json`; they are
matched against the reference text. To change a rule's level, set
`severity` in `.docrot.json`, e.g. `{"stale-section": "info"}`.

## Document formats

`docs` selects the files: `**/*.md`, `**/*.rst`, `**/*.adoc` and `llms.txt`
by default. reStructuredText covers titles (docutils ids), `code-block` /
`literalinclude` / `include` / `image` / `figure` / `toctree` and the autodoc
and `py:` declaration directives (their names are claims), `.. _label:`
targets, `::` literal and `>>>` blocks, grid and simple tables, ``literals``,
the `:func:` `:class:` `:meth:` `:mod:` `:attr:` `:data:` `:exc:` `:file:`
`:option:` `:envvar:` `:doc:` `:ref:` `:download:` roles (and
autosectionlabel `docname:Title` refs), `` `text <target>`_ `` and named
references, bare URLs, and `.. docrot:ignore` comments in every form the
Markdown directive has. A `.txt` file that looks like RST (Django's docs) is
read as RST when included in `docs`. Targets that start with `/` are relative
to the Sphinx source root, so the resolver climbs the document's ancestors.
AsciiDoc covers `=` titles, `[[id]]`/`[#id]` anchors, `[source,lang]`
listings, `link:`/`xref:`/`include::`/`image::` macros, `<<xrefs>>`, bare
URLs, `//` comments and `////` blocks. Whatever the format, the code a
document talks about may be Go, Python, Odin, Rust, JavaScript/TypeScript,
C# or C/C++, in any mix: each language is indexed on its own and a name is looked up in
every one before it is reported.

## Reference rules

| Rule | Kind | Meaning |
|---|---|---|
| `missing-path` | path | A file or directory mentioned in the document does not exist. docrot tries the path relative to the document, relative to the repo root, as a glob, and under every configured sibling repo. Suggestions come from same-name files elsewhere, case differences, small typos, and git rename history. A path that exists under a sub-tree (`internal/api` → `examples/service/internal/api`) is reported as a warning that names the sub-tree. | <!-- docrot:ignore -->
| `missing-symbol` | gosym / odinsym / pysym / rustsym / jssym / cssym / csym | A code symbol mentioned in the document does not exist. Go symbols are resolved with `go/parser`: `pkg.Name`, `pkg.Type.Method`, `Type.Method`, `Name()`. Odin, Python, Rust, JavaScript/TypeScript, C# and C/C++ symbols are resolved from a lightweight declaration index; Python module-level assignments of any case count (`handler500`, `connection`), attributes of a known class or object are accepted, and a name that is missing from a module that exists but is defined elsewhere in the package is a *warning* naming where it lives (a moved name, or one reachable through a compatibility shim) rather than an error. |
| `unknown-flag` | flag | `--name` / `-name` in the document is not defined by any `flag.*` call, clap attribute (`#[arg(long)]`, `#[arg(long = "name")]`), clap builder call (`.long("name")`), commander `.option('-p, --port <n>')`, yargs `.option('port', {…})`, System.CommandLine `new Option<T>("--port")`, a getopt_long-style option table (`{"verbose", no_argument, …}`), CLI11 `add_option("-f,--file")`/`add_flag` or cxxopts. Flags of the build system (`--enable-x`, `--with-x`, `--prefix`, `--std=c++17`) and flags spelled with a dot (`--sub.field`) are never claims. `-` and `_` are treated as equivalent. High confidence is a warning, medium is info; single-dash flags inside a longer command are never reported. Flags after an external program (`go test -race`, `git log --oneline`) are ignored, except after `go run ./cmd/x`. |
| `unknown-env` | env | An `UPPER_SNAKE` name is never read via `os.Getenv`, `os.LookupEnv`, or any call whose name contains `Env` (Go), nor via `env::var`, `env!`, `option_env!` or a clap `env` attribute (Rust), nor via `process.env.X`, `import.meta.env.X`, `Deno.env.get` or `Bun.env` (JavaScript), nor via `Environment.GetEnvironmentVariable("X")` or `Configuration["X"]` (C#), nor via `getenv("X")` / `std::getenv` / `curl_getenv` or a CLI11 `->envname("X")` (C/C++). An `UPPER_SNAKE` name that is a macro, an enum value or a CMake `option()` of the repository is never reported. Only reported when the line mentions an environment (env, export, `$`, 環境…), when the code reads at least one variable, and when the name is not a well-known external one (`GOPATH`, `GIT_*`, `HOME`…). |
| `unknown-config-key` | configkey | A dotted key such as `server.addr` appears neither as a `json:"…"` / `yaml:"…"` / `toml:"…"` tag path in any struct nor in any sample config file (`config*.json`, `*.example.json`, …). In prose it is only reported when the top-level segment is a known section, and always as info. A ```` ```json ```` block whose top-level keys include a known config key is a configuration example: every key path in it is checked (comments, trailing commas and `...` placeholders are tolerated; children of an unknown key are not repeated), a missing key is a warning when at least half the top-level keys are known and info otherwise, and suggestions come from the siblings under the same parent (`server.timeout` → `server.timeout_ms`). |
| `broken-anchor` | anchor | A link such as `[x](docs/rules.md#exit-codes)` or `[x](#exit-codes)` points to a heading that does not exist. Slugs follow GitHub rules (underscores kept), including CJK headings and `-1` suffixes for duplicates. MkDocs custom ids (`## Title { #id }`, `[](){#id}`) count as anchors, and a page containing a mkdocstrings `::: module` directive accepts any anchor. Explicit ids in raw HTML (`<a id="x">`, `<a name="x">`, `<h2 id="x">`) count as anchors. In reStructuredText the slugs are docutils ids, `.. _label:` targets are global to the documentation set (any page may `:ref:` them), `:ref:` misses are warnings because intersphinx may own the label (info when a `conf.py` maps inventories), and Sphinx's own `genindex`/`modindex`/`search` never count. AsciiDoc ids are `[[id]]`, `[#id]` and the `_section_title` defaults. Line anchors (`#L10-L20`) are ignored. |
| `broken-url` | url | Only with `--net`: an external URL returned 4xx/5xx or failed to connect. Local, private and `example.*` hosts are skipped. |
| `missing-command` | command | In a shell code block, the script or path a command refers to (`./scripts/verify.py`, `go run ./cmd/docrot`, `python scripts/demo.py`, `odin build dir`) does not exist. Output arguments (`-o dist/app`, `> out.txt`, `cp`/`mv` destinations) are never checked. In plain ```` ```text ```` blocks only lines with a shell prompt (`$ cmd`) count. |
| `missing-import` | import | In a Go code block, an import path under this module's path does not correspond to a package directory. Imports outside the module (stdlib, third-party) are ignored. In a C/C++ block, `#include "x.h"` must exist next to the document, at the root or under an `include/`, `src/` or `lib/` directory (error); `#include <x.h>` is checked the same way but is a system header when nothing matches. In a JavaScript/TypeScript block, `import … from 'name/sub'` or `require('name/sub')` where `name` is `package.json`'s own name must match an `exports` entry (`./sub`, `./sub/*`) or a file or directory (`sub`, `src/sub`, `lib/sub`, with the usual extensions and index files): error. A relative import (`./lib/x`) is tried next to the document, at the root and under `src/`, `lib/`, `app/`: info, since examples describe the reader's tree as often as this one. Other packages are ignored. |
| `missing-route` | route | An HTTP path the document names is registered by no handler. Registrations come from Go (`mux.Handle`/`HandleFunc` including Go 1.22 `"GET /x"` and host-qualified patterns, `r.Get`/`Post`/…, chi `Method`, gin `Handle("GET", …)`, `Mount`/`Group`/`Route`/`PathPrefix` prefixes) and Python (`@app.get`/`@router.post`/`@app.route(methods=…)` with the path on the next line too, `add_api_route`/`add_url_rule`, Starlette `Route`/`Mount`, Django `path()`, `APIRouter(prefix=…)`) and Rust (axum `.route("/x", get(h).post(h2))`, `.nest("/api", …)`, actix-web and rocket `#[get("/x")]`/`#[route("/x", method = "PUT")]`, `web::resource`/`web::scope`, `.mount("/v1", …)`, tide `.at("/x")`) and JavaScript (express/koa/fastify/hono `app.get('/x', …)` on a router-like receiver — `app`, `router`, `server`, `fastify`, `*Router` — `app.use('/api', sub)`, `fastify.register(p, { prefix: '/v1' })`, `fastify.route({ method, url })`, hono `.basePath`, `.route`, `.on([...], '/x')`, NestJS `@Controller('cats')` + `@Get(':id')`) and C# (minimal APIs `app.MapGet("/x", …)`, `Map`, `MapMethods`, `MapGroup("/todos")` prefixes followed through the group variable, attribute-routed controllers `[Route("api/[controller]")]` + `[HttpGet("{id}")]`). Parameters normalise (`{id}`, `:id`, `<int:id>`, `<id>`, `{path...}`, `<path..>`, `*`), a literal segment in the document matches a parameter in the code, and a path matches by its trailing segments when the router is mounted under a prefix the index cannot see. Claims: `` `GET /x` `` spans, `METHOD \| /path` table rows and ```` ```http ```` request lines are errors; prose `GET /x`, plain-text listings and `curl localhost:8080/x` examples are warnings; a bare `` `/x` `` span is info (often a mention, not a claim). A path that exists only for other methods is reported as "registered for GET, not DELETE". Repositories that register no route are not checked; a path that exists as a string literal (registered through a constant or a config default) is accepted. |
| `install-mismatch` | install | `go get` / `go install` of a path under this module whose package directory does not exist, or of a path whose last segment is this module's but the rest differs (the README still installs the old path); `pip install` / `uv add` / `poetry add` / `pipx install` of a name within two edits of `pyproject.toml`'s name (PEP 503 normalisation, extras and version specifiers stripped); `npm install` / `yarn add` of a name close to `package.json`'s (also `import x from 'name'` in a code block: warning); `cargo add` / `cargo install` of a name within two edits of `Cargo.toml`'s `[package] name` (`-` and `_` are equivalent); `dotnet add package` / `dotnet tool install` of a name within two edits of a `.csproj` package id (case-insensitive). Other packages are dependencies and never reported. Shell blocks are errors, inline spans warnings. |
| `toolchain-mismatch` | toolchain | A sentence with a requirement word (requires, needs, minimum, at least, or later, supports, 需要, 以上…) names `Go 1.21` / `Python 3.9` / `Rust 1.70` / `Node 18` / `.NET 6` / `CMake 3.16` while `go.mod`'s `go` directive / `requires-python` (or Poetry's `python`) / `Cargo.toml`'s `rust-version` / `package.json`'s `engines.node` / the lowest `TargetFramework` of the `.csproj` files / `cmake_minimum_required` says otherwise. Warning when the document promises less than the manifest requires (users on that version cannot build), info when it asks for more. Negated sentences ("no longer supports Python 3.8") are skipped, and so are the historical documents of `stale.exclude` (changelogs, release notes). | <!-- docrot:ignore toolchain-mismatch -->
| `missing-target` | target | `make x`, `npm run x`, `just x`, `task x`, `cmake --build … --target x` (shell blocks: error; inline spans: warning) names a target that the root Makefile / package.json `scripts` / justfile / Taskfile / CMakeLists.txt (`add_executable`, `add_library`, `add_custom_target`) does not define. Without a Makefile (and without autotools files), `make x` is checked against the CMake targets plus `all`, `clean`, `install`, `test`, `package`. A Makefile pattern rule (`test-%`) defines every name it matches (`make test-full`). Not checked when the runner file is absent, or when the document sits inside a directory that has its own runner file (a monorepo package). |
| `default-mismatch` | default | A line that names exactly one flag / config key / environment variable and states a default ("`--port` defaults to `8080`", "(default: `info`)", the Default column of a table) is compared with the code: `flag.*` literal arguments (numbers, strings, booleans, `n*time.Unit` durations), `default:"…"` struct tags under their dotted key, `typer.Option(...)`/`typer.Argument(...)` literals by parameter name, `click.option`/`add_argument` `default=`, `os.getenv(NAME, default)`, clap `default_value = "…"` / `default_value_t = …` attributes and `.default_value("…")` builder calls, `env::var("X").unwrap_or("…")`, commander's third `.option()` argument, yargs `default:`, `process.env.X || 'v'` / `?? 'v'`, System.CommandLine `getDefaultValue: () => v`, `GetEnvironmentVariable("X") ?? "v"`, CLI11 `->default_val(v)`, cxxopts `->default_value("v")`. Quotes, booleans, numbers and durations are normalised (`30s` equals `30000ms`). Names the code declares no default for are skipped. Warning, with the code's value as the suggestion. |

Letter case is compared exactly on every platform: `docs/foo.md` is not
`Docs/Foo.md`, even on Windows, and the finding says "differs only by
letter case". For Python, module and package names are valid symbols,
names imported at the top of a module count as that module's names (so
`fastapi.status` resolves), dotted names starting with a standard-library
module (`typing.Annotated`) or an example object (`app.routes`, `client.get`)
are skipped, and symbols defined under `tests/`, `docs/`, `docs_src/`,
`examples/` or `scripts/` are info rather than errors.

For Rust, names are qualified with `::` as rustdoc writes them:
`mycrate::io::read_all`, `crate::Config::new()`, `Config::new()`,
`io::read_all` (any `::`-boundary suffix of the full path), `Mode::Fast`
(enum variants are indexed) and `shout!()`. Module paths follow the file
tree (`src/lib.rs` is the crate root, `src/net/tcp.rs` is `net::tcp`), the <!-- docrot:ignore missing-path -->
crate name comes from the nearest `Cargo.toml`, `impl` methods attach to
their type, and `pub use` re-exports — single names, nested trees and
`pub use other::*` globs — make the re-exported names valid under the
re-exporting module, so `clap::Command` resolves through
`pub use clap_builder::*`. rustdoc intra-doc links (`[x](crate::extract)`)
are symbol claims. Never reported: `std::`/`core::`/`alloc::` paths, a
`::` path whose first segment is neither a crate nor a module of the
repository (`hyper::Body`: a dependency), a type the document's own code
examples import from another crate (`ServiceBuilder::layer` after
`use tower::ServiceBuilder;`), and `S::Error`-style generic parameters.
Bare `snake_case()` calls are info in Rust, as in Python, because they are
usually methods. Symbols under `tests/`, `examples/` and `benches/` are
info.

For JavaScript and TypeScript (`.js .mjs .cjs .jsx .ts .tsx`, outside
`node_modules/`, `dist/` and `build/`), a module is its file stem (an
index file takes its directory's name) and documents may write
`client.fetchAll`, `Client.fetchAll()`, `fetchAll()`, `.parse()` (a member
spelled with its dot) or `Mode.Fast`. Declarations are `function`,
`class`, `const`/`let`/`var`, `interface`, `type`, `enum` and `namespace`,
class members (`#private` and `private` ones excluded from the exported
surface), object-literal keys — also of an object built inside a factory
function, as fastify's instance is — `X.prototype.m = …`, `X.m = …`,
`Object.defineProperty`/`defineProperties`, `export { a as b }`,
`export * from './x'`, `module.exports = { … }` and `exports.name = …`.
`module.member` also matches a member of any class or object of that
module (`reply.send` when `reply.js` declares `class Reply`). The
document's own code blocks decide what a prefix means: after `import * as
z from "zod"` in zod's repository, `z.string()` is a claim about zod's
exports; after `import express from "express"`, `express.json()` is not a
claim. Named imports from this package (`import { Client } from
"fixture-web"`) are claims that each name is exported. An unknown member
of a class that `extends` another is not reported (inherited members are
not indexed); an unknown member of a class without a parent or of an
object literal is. Runtime globals (`console`, `Math`, `process`…) are
never claims, and a lower-case tail after a module name (`server.port`)
is read as a configuration key, not as a member.

For C# (`.cs` outside `bin/` and `obj/`, generated `*.Designer.cs` and
`*.g.cs` skipped), file-scoped and block namespaces, classes, structs,
interfaces, enums, records and delegates, their methods, properties,
fields, events and enum members are indexed under `Namespace.Type.Member`;
documents may write that in full, `Type.Member`, `Type`, `Member()`,
`Namespace.Type` or a type by an outer namespace (`Polly.RetryStrategyOptions`
for a type of `Polly.Retry`). Constructors are not members. `public` and
`protected` members (and every interface member) form the exported
surface. An unknown member of a type that inherits (`class A : B`) is not
reported; of a type without a base it is. `Type.Member` where the type is
declared nowhere in the repository (`TimeSpan.Zero`, `HttpClient.GetAsync`,
a type of another assembly) is never a claim, nor are members of the base
class library (`System`, `Microsoft`, `Console`, `Task`…). A dotted name
that equals a project's package id (`Humanizer.Core` when a `.csproj`
declares that `PackageId`) is accepted.

For C and C++ (`.c .h .cc .cpp .cxx .hpp .hh .hxx .inl .ipp` outside
`build/`, `third_party/`, `vendor/`, `external/` and `deps/`), the index
holds macros (`#define NAME`), prototypes and definitions (`RET name(`,
also `name(` at column 0 in the K&R style and `RET Type::method(` out of
line), `struct`/`class`/`union` types with their methods and fields under
`Type::member` (access labels honoured: `private:` members are not part
of the exported surface, `static` functions in sources neither), `enum`
values (`typedef enum { CURLOPT(CURLOPT_URL, …) } CURLoption;` included,
written `CURLOPT_URL` or `CURLoption::CURLOPT_URL`), `typedef` and `using`
aliases, and C++ namespaces (`namespace a::b {` and `X_NAMESPACE_BEGIN`
macro pairs). Documents may write `curl_easy_perform()`,
`curl_easy_perform(3)`, `CURLOPT_URL(3)`, `json::parse`,
`basic_json::dump`, `nlohmann::json` or any `::`-boundary suffix of a full
name. A bare `snake_case()` or `ALL_CAPS()` call is a claim at full
severity, because C declarations are top-level. Never a claim: `std::`
and `boost::` paths, a `::` path whose first segment is neither a
namespace nor a type of the repository (`GooFit::Application`), an
unknown member of an alias, a typedef or a type that inherits, and a
name that is a CMake target (`CLI11::CLI11`). `#include "x.h"` in a code
block must exist next to the document, at the root or under any
`include/`, `src/` or `lib/` (error); `#include <x.h>` that matches
nothing is a system header.

Things the extractor does not treat as references at all: slash-separated
prose (`health/ready`, `net/http`), lists of top-level directories
(`errx/logx/timex`), host-like first segments (`ghcr.io/org/image`), paths
with a `path/to`, `foo`, `x` placeholder segment, illustrative identifiers
(`Type.Method`, `Class.method`, `--flag`, `UPPER_SNAKE`), receiver variables
that collide with a package name (`cfg.Addr` becomes info), and anything
matched by `.gitignore` (build artifacts).

The string literals of the code base are the last resort before any of the
`missing-*` / `unknown-*` rules fires: an identifier-like literal (`"request_id"`,
`"X-Request-ID"`, `"/openapi.json"`, `"server.port"`, `"--dry-run"`) or a
struct-tag value that spells the documented name exactly makes it count as
existing. This is what keeps log fields, header names, metric names, flags
defined by libraries the index does not parse (pflag, argparse) and routes
registered through constants out of the report.

## Staleness

| Rule | Meaning |
|---|---|
| `stale-section` | Requires git. The section (heading → next heading) was last edited at time *T* (from `git blame`), but a **file** it references has `minChurn` or more commits after *T*, or at least one commit and `minDays` days have passed. Directory references do not count, and neither does a link to the document's own translation (a detected pair): that drift is `pair-lag`'s job. The message lists the most-changed files. Tune `stale.minChurn` / `stale.minDays`, exclude dated documents with `stale.exclude` (changelogs, release notes, research notes, specs and plans are excluded by default; the same list is skipped by `toolchain-mismatch`), or disable with `stale.enabled: false` or `--no-git`. |
| `stale-symbol` | Requires git. A section names a Go/Python/Odin declaration whose body lines (from a whitespace-insensitive blame of the source file) were last changed by `stale.minChurn` or more distinct commits after the section's edit time, or by one commit `stale.minDays` later. The finding sits on the reference and names `file:line`; when a section gets one, its coarser `stale-section` finding is dropped. Severity via `stale-symbol` (default warning). |

## Bilingual pairs

Pairs come from `pairs` in `.docrot.json` plus automatic patterns <!-- docrot:ignore -->
(`README-zh.md`, `README.zh-TW.md`, `docs/en/…` ↔ `docs/zh/…`). <!-- docrot:ignore -->
Findings are reported on the translation file.

| Rule | Meaning |
|---|---|
| `pair-heading` | The heading structure (count and level sequence) differs. |
| `pair-code` | Code block *n* differs between source and translation, or the number of code blocks differs. Code is expected to be identical across translations. |
| `pair-link` | A link target appears in only one of the two files (links between the pair itself are exempt). |
| `pair-table` | A table has a different row/column shape. |
| `pair-number` | A number or version string appears in only one of the two files (info). |
| `pair-lag` | Requires git. The source has commits newer than the translation's last change. The message lists them. |
| `pair-missing` | In a directory translation tree (`docs/en/` ↔ `docs/zh/`, any depth, any language of the convention) a source page has no counterpart in a language the tree does translate into. Info, on the source page; the message names the missing file. Suffix pairs (`README-zh.md`) form no tree, so one untranslated README is never a gap. |
| `pair-orphan` | The reverse: a page under `docs/zh/` whose source under `docs/en/` no longer exists. Warning, on the translation. Both need at least one detected pair in the tree, so a lone `docs/zh/` directory produces nothing. |

## Code comments

These run "on the side": only for the declarations that some document
referred to (`docrot check`), or for every exported declaration with
`docrot comments`. Go doc comments, Python docstrings, Odin `//` blocks,
Rust `///` comments, JSDoc `/** … */` blocks, C# `/// <summary>` XML
comments and C/C++ `/* … */`, `//` and Doxygen `///` comments are
supported.

| Rule | Meaning |
|---|---|
| `stale-comment` | Requires git. The comment attached to a function/type was last edited at time *T*; the body has `comments.minChurn` (default 2) or more distinct commits after *T*, or one commit that rewrote `comments.minFrac` (default 50%) of it. Whitespace-only changes never count. Info by default: it means "re-read this comment", not "this comment is wrong". |
| `comment-mentions-missing` | The comment names something code-like — a backticked token, `snake_case`, `camelCase`, `pkg.Name`, `--flag`, a path — that appears neither in the declaration, nor elsewhere in the file, nor anywhere in the index. Plain English words are never candidates. Typical hit: a parameter that was renamed while the comment kept the old name. |

## Coverage

| Rule | Meaning |
|---|---|
| `undocumented` | Only when `coverage.report` is true (or with `docrot coverage`): an exported Go symbol, flag, or environment variable is not mentioned by any document. Only references docrot extracted count, so a flag that appears solely inside a ```` ```text ```` block is "undocumented" until it is mentioned in prose or a code span. |

## Checking only what changed

`docrot check --changed` keeps the documents that differ from HEAD in the
work tree or index plus untracked ones; `--since origin/main` adds the
documents changed on the branch since its merge base with that ref. Every
document is still parsed so cross-document anchors resolve, but only the
changed ones are extracted, resolved and analysed; pair checks run for
pairs with a changed side; coverage is skipped (it needs every document).
A clean tree checks nothing and exits 0. Requires git.

## Git cache

Blame and log answers are kept in `<outDir>/git-cache.json` between runs:
blame by the blob hash of the file at HEAD (never for a file with
uncommitted changes), log by HEAD, and only the entries a run used are
written back. Findings are identical with or without it; the difference is
time. `--no-out` disables the cache along with the reports.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | No new finding at or above `--fail-on` (default `error`). |
| 1 | At least one new finding at or above `--fail-on`. Baselined findings never count. |
| 2 | Usage error, bad config, or internal error. |
