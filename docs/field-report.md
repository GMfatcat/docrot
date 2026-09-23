<!-- docrot:ignore-file -->
<!-- This report quotes paths and symbols from other repositories; they are
     not claims about docrot itself, so docrot skips the whole file. -->
# Field report: docrot on eight real repositories

A companion report covers seven open-source Python projects of different
sizes, five documented in Markdown and two in Sphinx:
`docs/field-report-python.md`.

Date: 2026-09-23. Binary: `docrot 0.1.0`, default `.docrot.json` unless
noted, git enabled. All eight repositories live side by side under one
`project/` directory; they share a library (`meowbase`) and two of them are
written in Odin rather than Go.

## Numbers

| repo | language | docs | references | errors | warnings | info | time |
|---|---|---|---|---|---|---|---|
| meowbase | Go | 51 | 2,305 | 49 | 52 | 213 | 1.4 s |
| meowbase-web | Go | 57 | 7,370 | 192 | 181 | 1,156 | 1.8 s |
| meowbase-sqlite | Go | 21 | 2,593 | 50 | 28 | 262 | 0.8 s |
| meowbase-rpc | Go | 27 | 3,458 | 165 | 61 | 402 | 1.0 s |
| meowshare | Go | 19 | 1,278 | 19 | 54 | 184 | 0.2 s |
| meowtrace | Odin | 86 | 339 | 3 | 8 | 54 | 0.7 s |
| meowboard | Odin + Python | 45 | 4,782 | 58 | 88 | 1,280 | 1.5 s |
| meowbase-distribute | docs only | 3 | 316 | 25 | 10 | 14 | 0.02 s |

Time is wall-clock including `git blame` and `git log` for every document,
on a Windows laptop. Nothing crashed; nothing needed per-repo configuration
to run.

## What it found (verified by hand)

**Renamed or moved API still documented under the old name.**

- meowbase `CHANGELOG.md:91` says `httpx.Retry`; the code has `Retry` as a
  field of `httpx.ClientConfig`. docrot's suggestion: `httpx.ClientConfig.Retry`.
- meowbase `docs/superpowers/...` and `CLAUDE.md` reference `servicex.Watch`,
  which no longer exists; the closest thing is the unexported
  `servicex.watchWaiters`.
- meowbase-sqlite documents `backup.NewTask()` in four places (`backup/README.md`,
  `docs/llms-reference.md`, the design spec) and `retention.NewTask()`
  once; neither function exists.
- meowbase-sqlite `docs/superpowers/plans/2026-08-02-m2-migrate.md` says
  `sqlitetest.Migrations`; it is `sqlitetest.Options.Migrations`.
- meowboard documents `gbench/harness.odin` eleven times; the directory
  contains `gate.odin`, `main.odin`, `scene.odin` — no harness.

**Documents describing a layout that was reorganised.**

- meowbase-rpc: 18 references to `core/errs`, 14 to `core/log`, 14 to
  `core/metric`, plus 19 Go import paths under those names. The `core`
  directory now holds `lifecycle`, `retry`, `rpcerr` and `coretest`.
- meowbase-web: 10 references to `internal/ui`, 9 to `x/ldapauth`, 4 to
  `x/oidcauth`; none exist. The `starter/plain/internal/auth` package is
  imported in four Go examples and does not exist either.
- meowbase `docs/meowbase_PRD_largan.md:1558` links to `docs/design.md`,
  which was never written.

**Dead anchors.**

- meowtrace: `README.md`, `README-zh.md` and `INSTRUCT.md` all link to
  `INSTRUCT.md#mtrace-storage`; there is no such heading. Three findings,
  one cause.

**Sections whose code moved on.**

- meowbase `README.md` "🚀 快速上手" was last edited 2026-08-04;
  `servicex/app.go` has four commits since. The same section pattern
  repeats in every template README ("怎麼用這個模板") and in `llms.txt`
  ("Packages": `servicex/README.md` 3 commits, `servicex/app.go` 4).
- A reviewer report in meowbase (`gpt-inspect/gpt_56_sol_inspect.md`)
  contains a section literally titled "M2. example README 已落後於實作";
  docrot flags that section as stale for the same reason the reviewer did.

**Bilingual drift.**

- meowbase-sqlite README ↔ README-zh and meowtrace README ↔ README-zh
  are in sync (no `pair-*` findings). docrot's own README pair was written
  with the checker running and is also clean.

**Cross-repository claims.**

- meowbase-web references 12 files of `meowbase` by their in-repo path
  (`httpx/README.md`, `servicex/app.go`, `fsx/hash.go`). With
  `"siblings": ["../meowbase", "../meowbase-rpc", "../meowbase-sqlite"]`
  the error count drops from 223 to 146 and the remaining ones are the
  repository's own rot. meowshare and meowbase-sqlite have the same
  pattern (`meowbase/testx/README.md`, `docs/contracts.md`).

## What was noise, and what changed because of it

Six rounds of running on these repositories reshaped the heuristics. The
first run on meowbase reported 214 errors and 938 warnings; the final one
reports 49 and 52. Every change below is in `internal/extract` or
`internal/resolve` and has a test.

| Symptom | Fix |
|---|---|
| `health/ready`, `net/http`, `json/text`, `Start/Stop`, `401/403` reported as missing paths (475 warnings) | `a/b` without an extension is only a path when its first segment is a real top-level directory or it is written `./a/b` |
| `main.go`, `config.json`, `app.log` reported as missing (bare file names) | bare file names are info with a "did you mean" suggestion |
| `cfg.API`, `cfg.Addr`, `cfg.File` reported as missing from package `cfg` (75 errors) | when the first part is a short receiver-like name that is also a package and the member exists on some type, the finding is info |
| `OUT_OF_RANGE`, `DATA_LOSS`, `YYYYMMDD_HHMMSS` reported as unknown env vars | `UPPER_SNAKE` only counts on a line that mentions an environment |
| `-race`, `-benchmem`, `-count=N` reported as unknown flags | flags after external programs are ignored (except after `go run ./cmd/x`) |
| `secret/token/password`, `counter/gauge/histogram` reported as missing commands | plain ```` ```text ```` blocks only yield commands from prompt lines |
| every CHANGELOG section and every "package overview" section stale (193 warnings) | dated documents excluded from staleness by default; only file-level references count, not directories |
| `internal/api` reported as error though `examples/service/internal/api` exists | downgraded to a warning that names the sub-tree |
| `kernel/target/release/x.dll`, `dist/app` reported as missing | paths matched by `.gitignore` are dropped (`git check-ignore`, one batch call) |
| `ghcr.io/ggml-org/llama.cpp`, `github.com/nats-io/nats.go` reported as missing paths | host-like first segments are not paths |
| vendored llama.cpp READMEs under `third_party/` produced 128 missing commands | `third_party/**`, `3rdparty/**`, `external/**`, `.*/**` excluded by default |
| `Draw_List.verts` (Odin struct field) reported as missing | `Type.field` where `Type` is a known Odin/Python declaration is accepted |
| `Envelope.request_id` (JSON wire field on a Go type) reported as missing | a lower-case member that is a known JSON key is accepted |
| `stale.minChurn`, `coverage.includeInternal` reported as missing Go symbols | a package-named prefix that is also a config section is checked as a config key first |
| `Type.Method`, `--flag`, `path/to/file`, `docs/foo.md` in explanatory docs | placeholder identifiers and path segments are ignored |
| `*net/http.timeoutError` reported as a glob path | leading `*`/`&` are stripped before classification |
| 214 info lines drowning 49 errors | the text report hides info unless `--info` |

## What is still noisy

- Documents that describe *another* repository (a review of meowbase kept in
  meowbase-sqlite, deep-research notes in meowbase-distribute) produce
  genuine-looking errors for paths that exist elsewhere. `siblings` fixes
  the common case; fully external repositories need `docrot:ignore-file`.
- Design specs and plans use illustrative paths by nature. Excluding
  `docs/superpowers/**` from `docs` is reasonable for repositories that
  keep dated design documents; docrot does that for itself.
- `Class.method` in a repository that has both Go and Python is ambiguous;
  docrot assumes Python only for snake_case methods.
- Suggestions for symbols that moved into a struct are sometimes the wrong
  member (`backup.NewTask` → `backup.Config.newFilename`).

## Code comments

The comment checks run for every Go/Odin/Python declaration a document
refers to (`docrot check`) or for every exported declaration (`docrot
comments`). On this corpus, with full history available:

| repo | declarations | stale-comment | comment-mentions-missing |
|---|---|---|---|
| meowbase | 179 | 0 | 0 |
| meowshare | 16 | 3 | 0 |

meowshare's three `stale-comment` findings are `client.Client.Pull`,
`PullFile` and `PullDir`: comments last edited 2026-09-09, bodies changed in
2–6 commits since. Reading them, the comments still describe the contract
(the bodies gained retry budgets and error mapping), which is why the rule
is info by default — it says "re-read this", not "this is wrong". A spike
over *all* 288 documented functions of meowbase (not just exported ones)
found 4 with a newer body and none with two or more newer commits, so
mature repositories stay quiet.

The first version of the mention check reported 8 findings per repository
that were all noise (`errors.Is`, `logger.Info`, `VERIFYING/COMPLETED/FAILED`,
`env:"NAME"`, `golang.org`, the same grouped `var` comment four times); the
filters that removed them are described in `docs/rules.md`.

## 0.3.0 update

Same repositories, `docrot 0.3.0`, git enabled. The stale-section set is
unchanged by design (route, target and toolchain claims do not feed it).

| repo | docs | references | errors | warnings | info | time |
|---|---|---|---|---|---|---|
| meowbase | 51 | 2,554 | 50 | 56 | 212 | 1.7 s |
| meowbase-web | 57 | 7,701 | 193 | 189 | 1,172 | 2.2 s |
| meowbase-sqlite | 21 | 2,684 | 50 | 31 | 270 | 0.9 s |
| meowbase-rpc | 27 | 3,476 | 159 | 70 | 413 | 1.2 s |
| meowshare | 19 | 1,441 | 18 | 49 | 187 | 0.4 s |
| meowtrace | 86 | 339 | 0 | 8 | 49 | 0.7 s |
| meowboard | 45 | 4,782 | 42 | 86 | 1,250 | 2.0 s |
| meowbase-distribute | 3 | 319 | 25 | 10 | 14 | 0.02 s |

References grew by about ten percent (routes, JSON example keys, install
lines, targets, version sentences); errors fell on meowbase-rpc and
meowboard because the string-literal index now vouches for names the code
spells only as strings (`LLM_API_KEY` in a template, wire keys, header
names). What the new rules found, verified by hand:

- **Routes.** meowbase registers 20 routes (`servicex`, the example
  service, tests). `docs/contracts.md`'s `/healthz` and `/readyz` resolve;
  `/openapi.json` and `/docs/` resolve through the string-literal index
  (they are `openapix` config defaults, not registrations); the one finding
  is `POST /v1/jobs`, which `CLAUDE.md`, the PRD and the example README
  all describe and nothing implements — the example README even says it is
  deferred. meowshare's PRD writes `GET /artifact/{id}/content` for a
  route the code registers as `/v1/artifacts/{id}/content`; the suggestion
  names it. meowbase-web's design documents mention Gitea's own OAuth
  routes, which are warnings docrot cannot tell from local ones.
- **JSON examples.** `timex/README.md` shows `retry_backoff` and
  `shutdown_timeout` in a config example whose struct fields are
  `Backoff` and `ShutdownTimeout` without tags — the example never
  round-tripped. buildx's `buildx.Info` example matches all ten tags.
- **Toolchain.** meowshare's `docker/README.md` said Go 1.14 in a sentence
  about vendoring behaviour; the claim reads as a requirement and is
  reported against `go 1.26.4`. Research notes under `docs/deep-research/`
  compare other libraries' Go requirements, so that directory joined the
  historical defaults.
- **Targets and install lines.** No findings: these repositories have no
  Makefiles and install through their own module paths.

## 0.4.0 update

- **git cache.** meowbase with git: 1.7 s cold, 0.2 s warm, identical
  findings (48 KB cache).
- **stale-symbol.** At the default thresholds (3 commits, or 90 days) no
  declaration these repositories document has churned enough since its
  section was written; with `minChurn: 1` meowbase reports eight, e.g.
  `api.NewServer` (examples/service/internal/api/server.go:77) changed once
  after the PRD section naming it was edited. Sections that get a symbol
  finding lose their file-level one, so the total does not grow.
- **default-mismatch.** meowbase declares 58 defaults (`default:"…"` struct
  tags in `cfg`, `openapix`, `servicex`); no document states one that the
  code contradicts.

## How to reproduce

```sh
go build -o dist/docrot ./cmd/docrot
python scripts/demo.py ../meowbase ../meowbase-web ../meowbase-sqlite ../meowbase-rpc ../meowshare ../meowtrace ../meowboard ../meowbase-distribute --out reports
```

`reports/<repo>.html` is a self-contained page with filters; `<repo>.json`
is the machine-readable form; `<repo>.txt` is the terminal output.
