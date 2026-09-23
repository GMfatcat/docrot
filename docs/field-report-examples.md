<!-- docrot:ignore-file -->
<!-- This report quotes snippets from other repositories; they are not
     claims about docrot itself, so docrot skips the whole file. -->
# Field report: the Go example check on five Go repositories

Date: 2026-09-24. Binary: `docrot 0.9.0`, default `.docrot.json`, `--no-git`.
The `example-syntax` rule parses every ```` ```go ```` block of a document
with `go/parser` under the shapes a snippet can take and reports the blocks
that fit none. The corpus is the meowbase family: five Go repositories whose
documentation is written in Traditional Chinese with Go examples on almost
every page.

## Numbers

| repository | documents | ```go blocks | first run | after tuning |
|---|---|---|---|---|
| meowbase | 42 | 240 | 2 | 0 |
| meowbase-rpc | 41 | 268 | 5 | 3 info |
| meowbase-sqlite | 29 | 209 | 2 | 0 |
| meowbase-web | 84 | 670 | 4 | 0 |
| meowshare | 27 | 183 | 0 | 0 |

1,570 blocks; the first run reported 13, all of them shapes a document uses
on purpose rather than broken examples. None of the five repositories has
a Go example that does not parse.

## What the first run reported, and the shape that absorbed it

1. **go.mod content in a go fence** (7 of 13). `module largan.local/meowbase`
   under ```` ```go ```` is the module path being shown, not an example. A
   block whose first line is `module`, `require`, `go 1.x` or `toolchain`
   is go.mod content and is skipped.
2. **Truncated excerpts** (3). A review document quotes eight lines of a
   function from the source tree, with a `// core/tls.go:43` comment, and
   stops before the closing brace. A block whose only error is that it
   ends before its braces close is reported at info level with the words
   "an excerpt, or a missing brace"; a syntax error inside the block stays
   a warning.
3. **Statements followed by declarations** (2). "Build the config, then
   here is your handler": five statements and then a `func`. Neither the
   statement wrapper (no nested named functions in Go) nor the top-level
   shape accepts it, so a mixed shape puts the leading statements into a
   function and leaves the declarations at the top level.
4. **Switch cases** (1). A patch description lists the `case` clauses to
   add to a `switch`. A `func _() { switch { … } }` wrapper accepts them.

Shapes that were needed from the start: a whole file, a file without its
package clause, a statement list, composite-literal elements
(`Addr: ":8080",`), struct fields (`Name string`), interface methods
(`Start() error`). Imports written inside a snippet are hoisted above the
wrapper. Blocks with `...`, `…` or `{{` are elided or templated and are
never parsed; ```` ```go ignore ```` (also `skip`, `no-check`, `pseudo`,
`output`, `diff`) opts a block out.

## How to reproduce

```sh
docrot check ../meowbase-rpc --no-git --no-out --fail-on none --info | grep example-syntax
```
