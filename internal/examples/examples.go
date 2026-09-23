// Package examples checks that the code examples a document shows would at
// least parse. Only Go is covered: go/parser is in the standard library and
// a README's ```go block with a missing brace or a syntax from an older
// draft is a lie a reader copies before finding out. Nothing is compiled or
// run.
package examples

import (
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"

	"docrot/internal/markdown"
	"docrot/internal/model"
)

// Options tunes the check.
type Options struct {
	// Severity of example-syntax findings. Empty means model.SevWarning
	// for a syntax error inside the block and model.SevInfo for a block
	// that merely ends before its braces close (an excerpt as often as a
	// mistake); a non-empty value applies to both.
	Severity model.Severity
}

// skipWords in a fence's info string mark a block the author does not
// want parsed: ```go ignore, ```go no-check, ```go pseudo.
var skipWords = map[string]bool{
	"ignore": true, "skip": true, "no-check": true, "nocheck": true, "norun": true, "no-run": true,
	"no-parse": true, "noparse": true, "pseudo": true, "output": true, "diff": true, "invalid": true, "bad": true,
}

// Go returns one example-syntax finding per ```go block of doc that parses
// under none of the shapes a documentation snippet takes: a whole file, a
// file without its package clause, a statement list (wrapped in a
// function), statements followed by declarations, switch cases, a list of
// composite-literal elements, struct fields or interface methods. Blocks
// that are visibly elided ("..." or "…"), templated ("{{"), empty, go.mod
// content, or whose info string carries a skip word are left alone, as are
// blocks under a docrot:ignore directive.
func Go(doc *markdown.Doc, opts Options) []model.Finding {
	if doc == nil || doc.IgnoreFile {
		return nil
	}
	var out []model.Finding
	for _, f := range doc.Fences {
		lang := strings.ToLower(f.Lang)
		if lang != "go" && lang != "golang" {
			continue
		}
		if doc.Ignored(f.StartLine) || skipInfo(f.Info) {
			continue
		}
		src := strings.Join(f.Content, "\n")
		if strings.TrimSpace(src) == "" || elided(src) || isGoMod(f.Content) {
			continue
		}
		e := parseAny(f.Content)
		if e == nil {
			continue
		}
		sev, msg := model.SevWarning, "Go example does not parse: "+e.msg
		if e.unclosed {
			sev, msg = model.SevInfo, "Go example ends before its braces close ("+e.msg+"): an excerpt, or a missing brace"
		}
		if opts.Severity != "" {
			sev = opts.Severity
		}
		out = append(out, model.Finding{
			Rule:        model.RuleExampleSyntax,
			Severity:    sev,
			Message:     msg,
			Loc:         model.Location{File: doc.Path, Line: f.StartLine + e.line, Col: e.col},
			Fingerprint: model.Fingerprint(model.RuleExampleSyntax, doc.Path, src),
			Data:        map[string]any{"lang": "go", "fence": f.StartLine},
		})
	}
	return out
}

// skipInfo reports whether the info string ("go ignore", "go,no-check",
// "go {ignore}") carries a skip word after the language.
func skipInfo(info string) bool {
	fields := strings.FieldsFunc(strings.ToLower(info), func(r rune) bool {
		return r == ' ' || r == ',' || r == '{' || r == '}' || r == '\t'
	})
	if len(fields) < 2 {
		return false
	}
	for _, w := range fields[1:] {
		if skipWords[w] {
			return true
		}
	}
	return false
}

// elided reports whether the block shows an omission or a template: such
// a snippet is never meant to parse as written.
func elided(src string) bool {
	return strings.Contains(src, "...") || strings.Contains(src, "…") || strings.Contains(src, "{{")
}

// isGoMod reports whether the block is go.mod content ("module x", "require
// (") that a document labelled go; it is not an example and would never
// parse as one.
func isGoMod(content []string) bool {
	for _, raw := range content {
		l := strings.TrimSpace(raw)
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		return strings.HasPrefix(l, "module ") || strings.HasPrefix(l, "require ") ||
			strings.HasPrefix(l, "go 1.") || strings.HasPrefix(l, "toolchain ")
	}
	return false
}

// parseError is the first syntax error of the shape that fits the block
// best, with its line relative to the block (1-based) and byte column.
// unclosed is set when the only problem is that the block ends early.
type parseError struct {
	line, col int
	msg       string
	unclosed  bool
}

// srcLine is one line of a candidate file: text, and the block line it
// came from (0 for wrapper text).
type srcLine struct {
	text string
	num  int
}

// shape is one way to wrap a snippet into a parseable file.
type shape []srcLine

func (s shape) source() string {
	var b strings.Builder
	for _, l := range s {
		b.WriteString(l.text)
		b.WriteByte('\n')
	}
	return b.String()
}

// parseAny tries every shape, the most natural one first, and returns nil
// as soon as one parses. When none does, the error of the first shape is
// reported: it is the one the author most plausibly meant.
func parseAny(content []string) *parseError {
	var first *parseError
	for _, s := range shapesFor(content) {
		e := parseShape(s)
		if e == nil {
			return nil
		}
		if first == nil {
			first = e
		}
	}
	return first
}

func parseShape(s shape) *parseError {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "example.go", s.source(), parser.SkipObjectResolution)
	if err == nil {
		return nil
	}
	pe := &parseError{msg: err.Error(), line: lastBlockLine(s)}
	list, ok := err.(scanner.ErrorList)
	if !ok || len(list) == 0 {
		return pe
	}
	e := list[0]
	pe.msg = e.Msg
	pe.unclosed = strings.HasSuffix(e.Msg, "found 'EOF'")
	if l := e.Pos.Line; l >= 1 && l <= len(s) && s[l-1].num > 0 {
		pe.line = s[l-1].num
		pe.col = e.Pos.Column
	}
	// an error on wrapper text (the closing brace) means the block ended
	// early: it is reported on the block's last line, without a column
	return pe
}

// lastBlockLine returns the highest block line the shape covers.
func lastBlockLine(s shape) int {
	last := 0
	for _, l := range s {
		if l.num > last {
			last = l.num
		}
	}
	return last
}

// lit makes wrapper lines.
func lit(texts ...string) shape {
	out := make(shape, len(texts))
	for i, t := range texts {
		out[i] = srcLine{text: t}
	}
	return out
}

// blk makes block lines with their numbers.
func blk(body []string, nums []int) shape {
	out := make(shape, len(body))
	for i, t := range body {
		out[i] = srcLine{text: t, num: nums[i]}
	}
	return out
}

// wrap concatenates parts into one shape.
func wrap(parts ...shape) shape {
	var out shape
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// shapesFor builds the candidate wrappings of a block in the order they
// are tried. Import declarations are hoisted above a wrapper, because they
// are only legal at the top level.
func shapesFor(content []string) []shape {
	hasPackage, hasDecl := false, false
	for _, raw := range content {
		l := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(l, "package "):
			hasPackage = true
		case strings.HasPrefix(l, "func ") || strings.HasPrefix(l, "type ") ||
			strings.HasPrefix(l, "var (") || strings.HasPrefix(l, "const (") || strings.HasPrefix(l, "//go:"):
			hasDecl = true
		}
	}
	nums := make([]int, len(content))
	for i := range nums {
		nums[i] = i + 1
	}
	whole := blk(content, nums)
	file := whole
	decls := wrap(lit("package main"), whole)
	imports, body, bodyNums := hoistImports(content, nums)
	header := wrap(lit("package main"), lit(imports...))
	rest := blk(body, bodyNums)
	stmts := wrap(header, lit("func _() {"), rest, lit("}"))
	cases := wrap(header, lit("func _() {", "switch {"), rest, lit("}", "}"))
	elems := wrap(header, lit("var _ = T{"), rest, lit("}"))
	fields := wrap(header, lit("type T struct {"), rest, lit("}"))
	methods := wrap(header, lit("type T interface {"), rest, lit("}"))
	out := []shape{stmts, decls, file}
	switch {
	case hasPackage:
		out = []shape{file, decls, stmts}
	case hasDecl:
		out = []shape{decls, file, stmts}
	}
	// statements first, then declarations ("build the config, then here is
	// your handler"): the statements go into a function, the rest stays
	// at the top level
	if k := firstDecl(body); k > 0 {
		out = append(out, wrap(header, lit("func _() {"), rest[:k], lit("}"), rest[k:]))
	}
	return append(out, cases, elems, fields, methods)
}

// firstDecl returns the index of the first top-level declaration line in
// body, or -1.
func firstDecl(body []string) int {
	for i, raw := range body {
		if strings.HasPrefix(raw, "func ") || strings.HasPrefix(raw, "type ") {
			return i
		}
	}
	return -1
}

// hoistImports splits a block into its import declarations and the
// remaining lines with their block line numbers.
func hoistImports(content []string, nums []int) (imports, rest []string, restNums []int) {
	inBlock := false
	for i, raw := range content {
		l := strings.TrimSpace(raw)
		switch {
		case inBlock:
			imports = append(imports, raw)
			if l == ")" {
				inBlock = false
			}
			continue
		case strings.HasPrefix(l, "import ("):
			inBlock = !strings.HasSuffix(l, ")")
			imports = append(imports, raw)
			continue
		case strings.HasPrefix(l, "import "):
			imports = append(imports, raw)
			continue
		}
		rest = append(rest, raw)
		restNums = append(restNums, nums[i])
	}
	return imports, rest, restNums
}
