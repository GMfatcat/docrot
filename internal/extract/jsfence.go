package extract

import (
	"regexp"
	"strings"

	"docrot/internal/markdown"
	"docrot/internal/model"
)

type markdownFence = markdown.Fence

// JavaScript and TypeScript code blocks: the packages and paths they
// import are claims, and the names they bind decide how later spans read.

var jsLangs = map[string]bool{"js": true, "javascript": true, "ts": true, "typescript": true, "jsx": true, "tsx": true, "mjs": true, "cjs": true, "node": true, "vue": true, "svelte": true}

var (
	// import x from 'spec';  import { a, b as c } from "spec";  import * as ns from 'spec';  import 'spec';
	reJSImport = regexp.MustCompile(`^\s*import\s+(?:type\s+)?(?:(.+?)\s+from\s+)?['"]([^'"]+)['"]`)
	// const x = require('spec');  const { a, b } = require("spec");  require('spec')
	reJSRequire = regexp.MustCompile(`^\s*(?:(?:const|let|var)\s+(.+?)\s*=\s*)?(?:await\s+)?(?:require|import)\(\s*['"]([^'"]+)['"]\s*\)`)
)

// jsImport is one import statement of a code block.
type jsImport struct {
	spec    string   // './lib/foo', 'fastify', 'zod/v4'
	binds   []string // default and namespace bindings: the local names the whole module is known by
	named   []string // named imports, by their exported (not aliased) name
	line    int      // 1-based line inside the fence content
	fenceLn int      // 1-based document line of the fence
}

// jsImports parses every import and require of the document's JavaScript
// blocks.
func (x *extractor) jsImports() []jsImport {
	if x.jsImp != nil {
		return *x.jsImp
	}
	var out []jsImport
	for _, f := range x.doc.Fences {
		if !jsLangs[strings.ToLower(f.Lang)] {
			continue
		}
		for i, ln := range f.Content {
			var bind, spec string
			if m := reJSImport.FindStringSubmatch(ln); m != nil {
				bind, spec = m[1], m[2]
			} else if m := reJSRequire.FindStringSubmatch(ln); m != nil {
				bind, spec = m[1], m[2]
			} else {
				continue
			}
			imp := jsImport{spec: spec, line: i + 1, fenceLn: f.StartLine}
			imp.binds, imp.named = jsBindings(bind)
			out = append(out, imp)
		}
	}
	x.jsImp = &out
	return out
}

// jsBindings splits "def, * as ns, { a, b as c, type D }" into the names
// bound to the whole module and the named imports.
func jsBindings(bind string) (binds, named []string) {
	bind = strings.TrimSpace(bind)
	if bind == "" {
		return nil, nil
	}
	if i := strings.IndexByte(bind, '{'); i >= 0 {
		inner := bind[i+1:]
		if j := strings.IndexByte(inner, '}'); j >= 0 {
			inner = inner[:j]
		}
		for _, it := range strings.Split(inner, ",") {
			it = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(it), "type "))
			if k := strings.Index(it, " as "); k >= 0 {
				it = strings.TrimSpace(it[:k])
			}
			if k := strings.IndexByte(it, ':'); k >= 0 {
				it = strings.TrimSpace(it[:k]) // const { a: b } = require(...)
			}
			if it != "" && it != "default" && isIdent(it) {
				named = append(named, it)
			}
		}
		bind = strings.TrimSpace(bind[:i])
	}
	for _, it := range strings.Split(bind, ",") {
		it = strings.TrimSpace(it)
		if k := strings.Index(it, " as "); k >= 0 {
			it = strings.TrimSpace(it[k+4:]) // * as ns
		}
		if it != "" && isIdent(it) {
			binds = append(binds, it)
		}
	}
	return binds, named
}

// jsPackageOf splits an import specifier into its package name and the
// sub-path: "zod/v4" → ("zod", "v4"), "@scope/pkg/x" → ("@scope/pkg", "x").
// A relative or absolute specifier has no package.
func jsPackageOf(spec string) (pkg, sub string) {
	if strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") || strings.Contains(spec, ":") {
		return "", spec
	}
	parts := strings.SplitN(spec, "/", 3)
	if strings.HasPrefix(spec, "@") {
		if len(parts) < 2 {
			return spec, ""
		}
		pkg = parts[0] + "/" + parts[1]
		if len(parts) == 3 {
			sub = parts[2]
		}
		return pkg, sub
	}
	pkg = parts[0]
	if len(parts) > 1 {
		sub = strings.Join(parts[1:], "/")
	}
	return pkg, sub
}

// jsNames classifies the names the document's JavaScript blocks bind:
// aliases of this package (`import * as z from "zod"` in zod's own docs)
// and names imported from other packages (`import express from
// "express"`), so that `z.string()` is a claim about this package and
// `express.json()` is not.
func (x *extractor) jsNames() (self, external map[string]bool) {
	if x.jsSelf != nil {
		return x.jsSelf, x.jsExt
	}
	x.jsSelf, x.jsExt = map[string]bool{}, map[string]bool{}
	own := x.hints.Project().NPMName
	for _, imp := range x.jsImports() {
		pkg, _ := jsPackageOf(imp.spec)
		switch {
		case pkg == "" && strings.HasPrefix(imp.spec, "."):
			continue // a relative path: could be anything of this repository
		case own != "" && pkg == own:
			for _, b := range imp.binds {
				x.jsSelf[b] = true
			}
		default:
			for _, b := range imp.binds {
				x.jsExt[b] = true
			}
			for _, n := range imp.named {
				x.jsExt[n] = true
			}
		}
	}
	return x.jsSelf, x.jsExt
}

// jsFenceRefs returns the claims of one JavaScript block: relative
// imports (low: examples describe the reader's tree as often as this
// one), sub-paths of this package (high: `zod/v4` must exist) and the
// names imported from this package (high: each must be exported).
func (x *extractor) jsFenceRefs(f markdownFence) []model.Reference {
	var out []model.Reference
	own := x.hints.Project().NPMName
	hasJS := false
	for _, k := range x.hints.Languages() {
		if k == model.KindJSSym {
			hasJS = true
		}
	}
	for _, imp := range x.jsImports() {
		if imp.fenceLn != f.StartLine {
			continue
		}
		pkg, sub := jsPackageOf(imp.spec)
		ln := f.StartLine + imp.line
		switch {
		case pkg == "" && strings.HasPrefix(imp.spec, "."):
			if hasJS {
				out = append(out, model.Reference{Kind: model.KindImport, Text: imp.spec, Norm: "js:" + imp.spec, Confidence: model.Low, Loc: model.Location{Line: ln}})
			}
		case own != "" && pkg == own:
			if sub != "" {
				out = append(out, model.Reference{Kind: model.KindImport, Text: imp.spec, Norm: "js:" + imp.spec, Confidence: model.High, Loc: model.Location{Line: ln}})
			} else {
				out = append(out, model.Reference{Kind: model.KindInstall, Text: imp.spec, Norm: "npm:" + pkg, Confidence: model.Medium, Loc: model.Location{Line: ln}})
			}
			if hasJS {
				for _, n := range imp.named {
					out = append(out, model.Reference{Kind: model.KindJSSym, Text: n, Norm: n, Confidence: model.High, Loc: model.Location{Line: ln}})
				}
			}
		}
	}
	return out
}
