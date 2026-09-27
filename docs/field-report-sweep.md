<!-- docrot:ignore-file -->
<!-- This report quotes paths and symbols from other repositories; they are
     not claims about docrot itself, so docrot skips the whole file. -->

# Field report: the meowbase sweep

docrot 1.0.1 was run over the five repositories of the meowbase family
(`meowbase`, `meowbase-sqlite`, `meowbase-rpc`, `meowbase-web`,
`meowshare`: 175 documents, about 18,000 references) with the intent of
fixing every real lie the reports named. Two things came out of it: one
documentation commit per repository, and the list of noise below, each
class of which 1.1.0 removes. The numbers are the findings on the living
documents after the dated records (`docs/superpowers/`, review transcripts,
PRDs, numbered patch proposals) had been excluded in each `.docrot.json`.

| repository | 1.0.1 | after the sweep, 1.0.1 | same documents, 1.1.0 |
|---|---|---|---|
| meowbase | 29 errors / 77 warnings / 211 info | 0 / 5 / 64 | 0 / 7 / 31 |
| meowbase-sqlite | 49 / 30 / 281 | 0 / 16 / 133 | 0 / 17 / 57 |
| meowbase-rpc | 158 / 72 / 416 | 0 / 25 / 100 | 0 / 20 / 28 |
| meowbase-web | 177 / 203 / 1208 | 0 / 31 / 122 | 0 / 30 / 78 |
| meowshare | 15 / 52 / 189 | 0 / 3 / 16 | 0 / 3 / 10 |

The middle column is what a person had to read. The lies it contained were
real and worth the trip: a quick start still building a bare `http.Server`
a month after the repository moved to its own builder; a README asserting
a config loader does not recurse into slices when the upstream fix had
landed three weeks earlier; a login example ending in a session API that
exists nowhere; a retry example wrapping with a code constant that lives
in another package; a status file saying "none of these proposals have
merged" while the sibling's CHANGELOG lists the merge. None of them would
have been found by a link checker.

## What was noise, and what 1.1.0 does about it

**Sentences that name a thing to say it does not exist.**
"不提供 `backup.NewTask()`", "No `retention.NewTask()`.", "曾經存在的
`Codec` 介面（`codec.Proto`／`codec.Raw`）", "**刪除**：`core/errs`、
`core/log`" — nine errors across three repositories, every one a document
being precise about an absence. The extractor now reads the clause around a
span: a strong cue (removed, deleted, no longer, does not provide, 不提供,
不存在, 已刪除, 刪除, 移除, 曾經, 原本…) within a clause before the span, a
weak cue (no, not, never, old, legacy, 不是) immediately before it, or "was
removed" / "no longer exists" / "不存在" after it, turns off the existence
claim — symbols, paths, commands and imports — for that span. The reach
ends at clause punctuation and, once another span has intervened, at a
colon, so that in "不提供 `X`：`Y` 本來就是那個抽象" the alternative `Y` is
still checked. Flags, routes and config keys are unaffected.

**Capitalized "extensions".** `servicex.Service`, `fs.FS`, `Options.FS`
were paths because `.service` (systemd units) and `.fs` (F#) are file
extensions. An extension is lower-case; a capitalized tail after the dot is
a member, and the span goes to the symbol classifier instead — which is
also why `servicex.Service` now resolves and two more `stale-section`
warnings appear in meowbase: sections that reference it are now tracked.

**Bare extensions and suffixes.** `.tmp`, `.json`, `.pb.go`, `.tar.gz`,
`_test.go` name a kind of file. They are no longer paths; `.env`,
`.gitignore` and the other real dotfiles still are.

**SQL and built-in calls.** `MIN(c)`, `MAX(typeof(c))`, `typeof()`,
`wal_checkpoint(TRUNCATE)`, `pragma_table_info(?)`, `julianday()` — 40
info findings in meowbase-sqlite alone — plus `make([]byte, n)`,
`close(items)`, `recover()`, `cancel()`, `fn(line, item)`. A bare call whose
name is a Go built-in, a SQL function, an ALL-CAPS word or one of the
placeholder names a document gives to "some function" is not a claim.

**Bare method names and the short method form.** `Ready()`, `Names()`,
`Close()` resolve when any type has a method by that name. `db.VacuumInto`
for `(*db.DB).VacuumInto` — the way Go documents habitually name methods —
was an error; it is now an info note that spells out the full name, since
no function `db.VacuumInto` exists and a reader may want to know.

**A sibling's package of the same name.** `httpx.Response` in
meowbase-rpc, whose own `httpx` has no `Response`, meant meowbase's. With
`siblings` configured, the resolver indexes the exported names of the
siblings' packages (lazily, `vendor/` and tests skipped) and turns the
error into an info finding that names the sibling. This is the
cross-repository symbol resolution the roadmap listed under tier 3, in its
cheap form: package-name equality, no type information.

**Flags of other tools in prose.** `--read-only`, `--platform`, `--target`
next to a `docker run`, `-race`, `-count=5` in a sentence about `go test`,
`--amend` about git. Flags in a command line after an external program were
already skipped; in prose there is no program to look at, so a curated list
of docker, `go test`/`go build` and git flags is skipped when the code does
not define the name itself. Names a program commonly owns (`--verbose`,
`--force`, `--all`, `--json`) are deliberately not on it.

**Brace patterns and file names with spaces.** `scripts/smoke.{sh,ps1}`
stands for two files and is checked as two; `docs/Local Artifact Relay
Service PRD v0.1.md` in a code span is one file, recognised when the first
word carries a slash and no extension, the last word a known extension,
and nothing in between looks like an option or a call.

**Placeholders in commands.** `./cmd/<your-service>` in a `go build` line
was a missing command. A path holding `<`, `>`, `{`, `}` or `$` is a
placeholder or shell syntax and never a claim.

**Comment mentions.** `NOT_FOUND` and `isValidRequestID` live in meowbase,
which meowbase-rpc builds against; `natsx.Request` is `(*Conn).Request`;
`Envelope.content_type` is a wire field; `gRPC` is a noun. The comment
check now consults the identifiers and string words of the siblings' Go
sources (`vendor/` excluded — `SQLITE_LOCKED` stays reported), accepts
`pkg.Method` when a type in that package has the method, accepts
`Type.field` when the field is a JSON key or literal, and skips a short
list of proper nouns spelled with an inner capital.

**`<!-- docrot:ignore -->` before a fence.** The directive ignored the
next non-blank line, which for a fenced block is the fence itself. When
that line opens a fence, the directive now covers the block through its
closing fence (rule-scoped too); the `-start`/`-end` pair keeps working.

## What stays

`Msg.Header` (nats.Msg), `App.Run` (servicex.App), `Backoff.Delay`
(timex.Backoff) — types of a sibling or a dependency named without their
package — remain info findings: resolving them needs the sibling's types,
not just its names. `SetMaxOpenConns(1)`, `Rollback()` (database/sql
methods), `/metrics` in a sentence saying there is no such endpoint, and
`config.json` written relative to a directory the document does not name,
are info too, and are read as such. The stale-section warnings that
remained after the sweep were each reviewed against the current code and
left standing; a section that is still true is not edited to silence a
heuristic.
