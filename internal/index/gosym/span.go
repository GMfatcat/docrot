package gosym

import (
	"go/ast"
	"go/token"
	"sort"
	"strings"

	"docrot/internal/model"
)

// declSpan is the comment and body geometry of one declaration, captured
// while the file is parsed so that Span can answer without re-reading the
// source. Line numbers are 1-based; zero means "none".
type declSpan struct {
	docStart  int
	docEnd    int
	declLine  int
	bodyStart int
	bodyEnd   int
	doc       []string
	params    []string
}

// funcDeclSpan captures a func or method declaration.
func funcDeclSpan(d *ast.FuncDecl, fset *token.FileSet) *declSpan {
	sp := &declSpan{
		declLine:  lineOf(fset, d.Pos()),
		bodyStart: lineOf(fset, d.Pos()),
		bodyEnd:   lineOf(fset, d.End()),
		params:    funcParams(d.Recv, d.Type),
	}
	sp.setDoc(d.Doc, fset)
	return sp
}

// ifaceMethodSpan captures one method of an interface. Such a method has no
// body, so its "body" is the signature itself.
func ifaceMethodSpan(f *ast.Field, fset *token.FileSet) *declSpan {
	sp := &declSpan{
		declLine:  lineOf(fset, f.Pos()),
		bodyStart: lineOf(fset, f.Pos()),
		bodyEnd:   lineOf(fset, f.End()),
	}
	if ft, ok := f.Type.(*ast.FuncType); ok {
		sp.params = funcParams(nil, ft)
	}
	sp.setDoc(f.Doc, fset)
	return sp
}

// specSpan captures one spec of a type, const or var declaration. The spec's
// own doc comment wins; a spec inside a group that has none inherits the
// group's doc (which is also where the doc of an ungrouped declaration
// lives).
func specSpan(groupDoc, specDoc *ast.CommentGroup, pos, end token.Pos, fset *token.FileSet) *declSpan {
	sp := &declSpan{
		declLine:  lineOf(fset, pos),
		bodyStart: lineOf(fset, pos),
		bodyEnd:   lineOf(fset, end),
	}
	doc := specDoc
	if doc == nil {
		doc = groupDoc
	}
	sp.setDoc(doc, fset)
	return sp
}

// setDoc records the position and stripped text of a comment group.
func (sp *declSpan) setDoc(cg *ast.CommentGroup, fset *token.FileSet) {
	if cg == nil {
		return
	}
	sp.docStart = lineOf(fset, cg.Pos())
	sp.docEnd = lineOf(fset, cg.End())
	sp.doc = commentLines(cg)
}

// commentLines renders a comment group as one string per source line with the
// "//", "/*" and "*/" markers and one leading space removed. Blank lines
// inside the comment are kept; blank lines at either end are dropped.
func commentLines(cg *ast.CommentGroup) []string {
	var out []string
	for _, c := range cg.List {
		switch {
		case strings.HasPrefix(c.Text, "//"):
			out = append(out, stripOneSpace(strings.TrimRight(c.Text[2:], " \t\r")))
		case strings.HasPrefix(c.Text, "/*"):
			inner := strings.TrimSuffix(strings.TrimPrefix(c.Text, "/*"), "*/")
			for _, ln := range strings.Split(inner, "\n") {
				out = append(out, stripOneSpace(strings.TrimRight(ln, " \t\r")))
			}
		default:
			out = append(out, c.Text)
		}
	}
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// stripOneSpace removes a single leading space.
func stripOneSpace(s string) string {
	if strings.HasPrefix(s, " ") {
		return s[1:]
	}
	return s
}

// funcParams lists the receiver, parameter and named result identifiers of a
// signature, in that order, without duplicates. Blank names are skipped, as
// are type parameters.
func funcParams(recv *ast.FieldList, ft *ast.FuncType) []string {
	var out []string
	seen := map[string]bool{}
	addList := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				if n.Name == "" || n.Name == "_" || seen[n.Name] {
					continue
				}
				seen[n.Name] = true
				out = append(out, n.Name)
			}
		}
	}
	addList(recv)
	if ft != nil {
		addList(ft.Params)
		addList(ft.Results)
	}
	return out
}

// symbolSpan converts a parsed entry into the shared model type. ok is false
// for declarations that carry no span (struct fields).
func (e entry) symbolSpan() (model.SymbolSpan, bool) {
	if e.span == nil {
		return model.SymbolSpan{}, false
	}
	return model.SymbolSpan{
		Qualified: e.qual(),
		Kind:      model.KindGoSymbol,
		File:      e.file,
		DocStart:  e.span.docStart,
		DocEnd:    e.span.docEnd,
		DeclLine:  e.span.declLine,
		BodyStart: e.span.bodyStart,
		BodyEnd:   e.span.bodyEnd,
		Doc:       e.span.doc,
		Params:    e.span.params,
		Exported:  e.exported(),
	}, true
}

// exported reports whether the declaration is part of the package's exported
// surface: the name is exported and, for a member, so is its owning type.
func (e entry) exported() bool {
	if !ast.IsExported(e.name) {
		return false
	}
	return e.owner == "" || ast.IsExported(e.owner)
}

// addSpan records the span of e under every lookup form, mirroring addEntry:
// the first declaration seen for a form wins.
func addSpan(m map[string]model.SymbolSpan, e entry) {
	sp, ok := e.symbolSpan()
	if !ok {
		return
	}
	put := func(k string) {
		if k == "" {
			return
		}
		if _, exists := m[k]; !exists {
			m[k] = sp
		}
	}
	put(e.qual())
	if e.owner != "" {
		put(e.owner + "." + e.name)
		return
	}
	put(e.name)
}

// cloneSpan copies the slices of a span so that callers cannot mutate the
// index.
func cloneSpan(sp model.SymbolSpan) model.SymbolSpan {
	out := sp
	out.Doc = append([]string(nil), sp.Doc...)
	out.Params = append([]string(nil), sp.Params...)
	return out
}

// Span returns the declaration span of a qualified name: its doc comment, the
// declaration line and the body it documents. It accepts the same spellings
// as HasSymbol ("pkg.Name", "pkg.Type.Method", "Type.Method", "Name", with a
// leading "*"/"&", a trailing call and generic instantiations ignored) and
// covers funcs, methods, types, consts and vars. When a bare name matches
// several declarations the first one indexed is returned. Declarations found
// only in _test.go files are consulted last.
func (ix *Index) Span(qualified string) (model.SymbolSpan, bool) {
	key := normalizeSymbol(qualified)
	if key == "" || strings.Count(key, ".") > 2 {
		return model.SymbolSpan{}, false
	}
	if sp, ok := ix.spans[key]; ok {
		return cloneSpan(sp), true
	}
	if sp, ok := ix.testSpans[key]; ok {
		return cloneSpan(sp), true
	}
	return model.SymbolSpan{}, false
}

// AllSpans returns one span per exported func, method or type (methods and
// fields of unexported types excluded), skipping package main, _test.go files
// and — unless includeInternal — anything under an internal/ directory. The
// result is sorted by qualified name. Consts and vars are not included.
func (ix *Index) AllSpans(includeInternal bool) []model.SymbolSpan {
	seen := map[string]bool{}
	var out []model.SymbolSpan
	for _, e := range ix.entries {
		if e.test || e.pkg == "main" {
			continue
		}
		switch e.kind {
		case kindFunc, kindMethod, kindType:
		default:
			continue
		}
		sp, ok := e.symbolSpan()
		if !ok || !sp.Exported {
			continue
		}
		if !includeInternal && isInternalPath(e.file) {
			continue
		}
		if seen[sp.Qualified] {
			continue
		}
		seen[sp.Qualified] = true
		out = append(out, cloneSpan(sp))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified < out[j].Qualified })
	return out
}
