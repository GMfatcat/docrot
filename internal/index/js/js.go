// Package js builds a lightweight, line-based index of JavaScript and
// TypeScript source files (*.js, *.mjs, *.cjs, *.jsx, *.ts, *.tsx) so that
// docrot can check whether a documentation reference to a module, function,
// class, method or constant is still valid.
//
// The index does not parse JavaScript. It recognises declaration lines:
//
//	[export] [default] [async] function NAME / class NAME / const|let|var NAME
//	[export] interface|type|enum|namespace NAME
//	export { A, B as C } [from './x'];  export * from './x'
//	module.exports = { A, B };  exports.NAME = ...;  NAME.prototype.method = ...
//	class NAME { method() {} static m() {} get x() {} field = 1 }   → NAME.method
//	const NAME = { key: ..., method() {} }                          → NAME.key
//
// Brace depth is tracked so that class members, object-literal keys, enum
// members and namespace bodies attach to their owner. A module is named
// by its file stem (index files by their directory), so a document may
// write `client.fetchAll`, `Client.fetchAll` or `fetchAll()`.
//
// Once built, an Index is safe for concurrent read-only use.
package js

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"docrot/internal/fuzzy"
	"docrot/internal/index/defaults"
	"docrot/internal/index/lang"
	"docrot/internal/index/literals"
	"docrot/internal/index/routes"
	"docrot/internal/model"
)

var _ lang.Index = (*Index)(nil)

// Stats summarises what Build found.
type Stats struct {
	Files   int // source files read
	Modules int // distinct module names
	Symbols int // recognised declarations
}

// symbol is one recognised declaration.
type symbol struct {
	module    string // file stem ("client"), or the directory of an index file
	qualified string // name within the module: "fetchAll", "Client.fetchAll", "Mode.Fast"
	name      string // last segment
	file      string
	line      int
	kind      string // function, class, const, interface, type, enum, namespace, method, field, member, export
	exported  bool
	span      *jsSpan
}

// Index is a queryable snapshot of every recognised declaration under a
// root directory. Build it once with Build; every method is read-only.
type Index struct {
	stats   Stats
	modules []string
	modSet  map[string]bool
	example map[string]bool // module → lives under tests/, docs/, examples/…

	all           []symbol
	byFull        map[string]int
	byClassMethod map[string][]int // "Owner.member" → declarations across modules
	byName        map[string][]int
	byLen         map[int][]int
	// globs maps a module to the modules it re-exports wholesale with
	// `export * from './x'`.
	globs map[string][]string

	routes []routes.Route
	lits   *literals.Set
	defs   *defaults.Set
	flags  []string
	envs   []string
}

var sourceExts = map[string]bool{".js": true, ".mjs": true, ".cjs": true, ".jsx": true, ".ts": true, ".tsx": true, ".mts": true, ".cts": true}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "dist": true, "build": true, "out": true, ".next": true, ".nuxt": true, ".svelte-kit": true, "coverage": true, "vendor": true, ".turbo": true, ".cache": true}

// Build walks root, reads every JavaScript/TypeScript file (skipping
// dependency and build directories and any path in exclude — prefix match
// on directories, exact match on files) and returns the resulting Index.
func Build(root string, exclude []string) (*Index, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || isExcludedDir(rel, exclude) {
				return filepath.SkipDir
			}
			return nil
		}
		if isExcludedFile(rel, exclude) {
			return nil
		}
		base := d.Name()
		if !sourceExts[strings.ToLower(path.Ext(base))] || strings.Contains(base, ".min.") || strings.HasSuffix(base, ".bundle.js") || strings.HasSuffix(base, ".config.js") || strings.HasSuffix(base, ".config.ts") {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	ix := &Index{
		modSet:        map[string]bool{},
		example:       map[string]bool{},
		byFull:        map[string]int{},
		byClassMethod: map[string][]int{},
		byName:        map[string][]int{},
		byLen:         map[int][]int{},
		globs:         map[string][]string{},
		lits:          literals.New(),
		defs:          defaults.New(),
	}
	if len(files) == 0 {
		return ix, nil
	}

	type parsed struct {
		module string
		syms   []decl
		globs  []string
		routes []routes.Route
		lits   []string
		defs   [][3]string
		flags  []string
		envs   []string
		ok     bool
	}
	results := make([]parsed, len(files))
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(files) {
		workers = len(files)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				rel := files[i]
				data, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
				if rerr != nil {
					continue
				}
				lines := strings.Split(string(data), "\n")
				masked := maskAll(lines)
				r := parsed{module: moduleOf(rel), ok: true}
				r.syms, r.globs = parseFile(lines, masked, rel)
				r.routes = parseRoutes(lines, rel)
				r.defs, r.flags, r.envs = parseDefaults(lines)
				for _, l := range lines {
					r.lits = append(r.lits, literals.Scan(templateToPlain(l))...)
				}
				results[i] = r
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	flagSet := map[string]bool{}
	envSet := map[string]bool{}
	type rawGlob struct{ module, target, file string }
	var rawGlobs []rawGlob
	for i, r := range results {
		if !r.ok {
			continue
		}
		ix.stats.Files++
		ix.modSet[r.module] = true
		if isExamplePath(files[i]) {
			ix.example[r.module] = true
		}
		ix.routes = append(ix.routes, r.routes...)
		ix.lits.AddAll(r.lits)
		for _, d := range r.defs {
			ix.defs.Add(d[0], d[1], d[2])
		}
		for _, f := range r.flags {
			if !flagSet[f] {
				flagSet[f] = true
				ix.flags = append(ix.flags, f)
			}
		}
		for _, e := range r.envs {
			if !envSet[e] {
				envSet[e] = true
				ix.envs = append(ix.envs, e)
			}
		}
		for _, g := range r.globs {
			rawGlobs = append(rawGlobs, rawGlob{r.module, g, files[i]})
		}
		for _, d := range r.syms {
			full := r.module + "." + d.path
			ix.stats.Symbols++
			if _, exists := ix.byFull[full]; exists {
				continue
			}
			s := symbol{module: r.module, qualified: d.path, name: d.name, file: d.file, line: d.line, kind: d.kind, exported: d.exported, span: d.span}
			idx := len(ix.all)
			ix.byFull[full] = idx
			ix.byName[d.name] = append(ix.byName[d.name], idx)
			if i := strings.LastIndex(d.path, "."); i >= 0 {
				parts := strings.Split(d.path, ".")
				key := parts[len(parts)-2] + "." + parts[len(parts)-1]
				ix.byClassMethod[key] = append(ix.byClassMethod[key], idx)
			}
			n := len(strings.ToLower(d.name))
			ix.byLen[n] = append(ix.byLen[n], idx)
			ix.all = append(ix.all, s)
		}
	}
	for _, g := range rawGlobs {
		if target := moduleOfSpec(g.file, g.target); target != "" && target != g.module && ix.modSet[target] {
			ix.globs[g.module] = append(ix.globs[g.module], target)
		}
	}
	for m := range ix.modSet {
		ix.modules = append(ix.modules, m)
	}
	sort.Strings(ix.modules)
	sort.Strings(ix.flags)
	sort.Strings(ix.envs)
	ix.stats.Modules = len(ix.modules)
	return ix, nil
}

// moduleOf names the module of a source file: its stem, or for an index
// file the directory it sits in. ".d", ".test" and ".spec" suffixes stay
// part of the stem so that a declaration file and its implementation can
// coexist ("fastify.d" next to "fastify").
func moduleOf(rel string) string {
	base := path.Base(rel)
	stem := strings.TrimSuffix(base, path.Ext(base))
	if stem == "index" || stem == "index.d" {
		dir := path.Dir(rel)
		if dir == "." || dir == "" {
			return "index"
		}
		return path.Base(dir)
	}
	return stem
}

// moduleOfSpec names the module an `export * from './x'` line refers to,
// relative to the file that wrote it; "" for a package.
func moduleOfSpec(file, spec string) string {
	if !strings.HasPrefix(spec, ".") {
		return ""
	}
	target := path.Join(path.Dir(file), spec)
	base := path.Base(target)
	if ext := path.Ext(base); sourceExts[ext] {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "index" {
		return path.Base(path.Dir(target))
	}
	if base == "." || base == "" {
		return path.Base(target)
	}
	return base
}

func isExamplePath(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, seg := range segs[:len(segs)-1] {
		switch strings.ToLower(seg) {
		case "test", "tests", "__tests__", "testing", "spec", "docs", "doc", "examples", "example", "samples", "scripts", "benchmarks", "bench", "benchmark", "demo", "demos", "e2e", "fixtures", "__mocks__":
			return true
		}
	}
	stem := strings.TrimSuffix(segs[len(segs)-1], path.Ext(segs[len(segs)-1]))
	return strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec") || strings.HasSuffix(stem, ".stories")
}

func isExcludedDir(rel string, exclude []string) bool {
	for _, e := range exclude {
		e = cleanExclude(e)
		if e != "" && (rel == e || strings.HasPrefix(rel, e+"/")) {
			return true
		}
	}
	return false
}

func isExcludedFile(rel string, exclude []string) bool {
	for _, e := range exclude {
		if cleanExclude(e) == rel {
			return true
		}
	}
	return false
}

func cleanExclude(e string) string {
	return strings.Trim(strings.TrimSpace(filepath.ToSlash(e)), "/")
}

// stripParens removes a trailing call suffix and a leading dot:
// "fetchAll()" → "fetchAll", ".addSchema" → "addSchema".
func stripParens(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '<'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(s, ".")
}

// lookup resolves a reference to matching declarations. Accepted forms:
// "module.name", "Owner.member" in any module, "module.Owner.member", a
// module name, and a bare name, with a trailing call stripped.
func (ix *Index) lookup(qualified string) []symbol {
	q := stripParens(qualified)
	if q == "" {
		return nil
	}
	if out := ix.find(q, 0); len(out) > 0 {
		return out
	}
	if ix.modSet[q] {
		return []symbol{{module: q, name: q, kind: "module"}}
	}
	parts := strings.Split(q, ".")
	if len(parts) >= 2 {
		key := parts[len(parts)-2] + "." + parts[len(parts)-1]
		if m := ix.byClassMethod[key]; len(m) > 0 {
			return ix.symbols(m)
		}
		if ix.modSet[parts[0]] {
			// "reply.send" where reply.js declares class Reply with send:
			// documents name the instance after the module. A same-named
			// declaration in another module does not count.
			var out []symbol
			for _, i := range ix.byName[parts[len(parts)-1]] {
				if ix.all[i].module == parts[0] {
					out = append(out, ix.all[i])
				}
			}
			return out
		}
	}
	return ix.symbols(ix.byName[parts[len(parts)-1]])
}

// find matches q exactly and then through the `export *` re-exports of
// its module prefix.
func (ix *Index) find(q string, depth int) []symbol {
	if i, ok := ix.byFull[q]; ok {
		return []symbol{ix.all[i]}
	}
	if depth > 3 {
		return nil
	}
	if i := strings.IndexByte(q, '.'); i > 0 {
		for _, target := range ix.globs[q[:i]] {
			if found := ix.find(target+q[i:], depth+1); len(found) > 0 {
				return found
			}
		}
	}
	return nil
}

func (ix *Index) symbols(idx []int) []symbol {
	out := make([]symbol, 0, len(idx))
	for _, i := range idx {
		out = append(out, ix.all[i])
	}
	return out
}

// Empty reports whether no source file was found under root.
func (ix *Index) Empty() bool { return ix.stats.Files == 0 }

// Stats returns index-wide counters.
func (ix *Index) Stats() Stats { return ix.stats }

// Counts returns the counters in the shape the composite index reports.
func (ix *Index) Counts() lang.Stats {
	return lang.Stats{Files: ix.stats.Files, Namespaces: ix.stats.Modules, Symbols: ix.stats.Symbols}
}

// Namespaces returns every module name, sorted.
func (ix *Index) Namespaces() []string { return append([]string(nil), ix.modules...) }

// IsNamespace reports whether qualified names a module.
func (ix *Index) IsNamespace(qualified string) bool { return ix.modSet[stripParens(qualified)] }

// IsExample reports whether a module lives under a tests, docs or
// examples tree, or is a test file.
func (ix *Index) IsExample(module string) bool { return ix.example[module] }

// Has reports whether qualified exists (see lookup for the accepted forms).
func (ix *Index) Has(qualified string) bool { return len(ix.lookup(qualified)) > 0 }

// Opaque reports whether qualified is a class that extends another (its
// inherited members are not indexed) or a plain value whose properties
// the index cannot see; a class without a parent and an object literal
// list all their members.
func (ix *Index) Opaque(qualified string) bool {
	for _, s := range ix.lookup(qualified) {
		switch s.kind {
		case "class", "object", "enum", "namespace", "module":
			return false
		case "class:open":
			return true
		}
	}
	return true
}

// File returns the declaring file (relative, forward slashes) and 1-based
// line of qualified, if known.
func (ix *Index) File(qualified string) (string, int, bool) {
	m := ix.lookup(qualified)
	if len(m) == 0 || m[0].file == "" {
		return "", 0, false
	}
	return m[0].file, m[0].line, true
}

// Symbols lists every declaration as "module.qualified", sorted.
func (ix *Index) Symbols() []string {
	out := make([]string, 0, len(ix.all))
	for _, s := range ix.all {
		out = append(out, s.module+"."+s.qualified)
	}
	sort.Strings(out)
	return out
}

// Literals returns the identifier-like string literals of the tree.
func (ix *Index) Literals() *literals.Set { return ix.lits }

// Defaults returns the commander/yargs option defaults and process.env
// fallbacks found.
func (ix *Index) Defaults() *defaults.Set { return ix.defs }

// Routes returns every HTTP route registration found (express, koa,
// fastify, hono, NestJS), in file order.
func (ix *Index) Routes() []routes.Route { return append([]routes.Route(nil), ix.routes...) }

// Flags returns the long option names declared through commander or
// yargs, sorted.
func (ix *Index) Flags() []string { return append([]string(nil), ix.flags...) }

// Envs returns the environment variables the code reads, sorted.
func (ix *Index) Envs() []string { return append([]string(nil), ix.envs...) }

// Similar returns up to n existing "module.qualified" candidates whose
// last segment is close (Damerau-Levenshtein distance <= max(2, len/4),
// case-insensitive) to the last segment of qualified. Best first; ties
// broken alphabetically.
func (ix *Index) Similar(qualified string, n int) []string {
	if n <= 0 {
		return nil
	}
	q := stripParens(qualified)
	parts := strings.Split(q, ".")
	last := strings.ToLower(parts[len(parts)-1])
	threshold := len([]rune(last)) / 4
	if threshold < 2 {
		threshold = 2
	}
	type cand struct {
		dist int
		full string
	}
	var cands []cand
	for l := len(last) - threshold; l <= len(last)+threshold; l++ {
		for _, i := range ix.byLen[l] {
			s := ix.all[i]
			if d := fuzzy.Distance(last, strings.ToLower(s.name)); d <= threshold {
				cands = append(cands, cand{d, s.module + "." + s.qualified})
			}
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].full < cands[j].full
	})
	if len(cands) > n {
		cands = cands[:n]
	}
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.full
	}
	return out
}

// Span returns the declaration span of a function, class, method or
// constant: its JSDoc or `//` comment, the declaration line and the body
// its braces enclose.
func (ix *Index) Span(qualified string) (model.SymbolSpan, bool) {
	for _, s := range ix.lookup(qualified) {
		if s.span != nil {
			return s.symbolSpan(), true
		}
	}
	return model.SymbolSpan{}, false
}

// AllSpans returns one span per exported function, class, method and
// constant outside example modules, sorted by qualified name.
func (ix *Index) AllSpans() []model.SymbolSpan {
	var out []model.SymbolSpan
	for _, s := range ix.all {
		if s.span == nil || !s.exported || ix.example[s.module] {
			continue
		}
		out = append(out, s.symbolSpan())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified < out[j].Qualified })
	return out
}

func (s symbol) symbolSpan() model.SymbolSpan {
	return model.SymbolSpan{
		Qualified: s.module + "." + s.qualified,
		Kind:      model.KindJSSym,
		File:      s.file,
		DocStart:  s.span.docStart,
		DocEnd:    s.span.docEnd,
		DeclLine:  s.span.declLine,
		BodyStart: s.span.declLine,
		BodyEnd:   s.span.bodyEnd,
		Doc:       append([]string(nil), s.span.doc...),
		Params:    append([]string(nil), s.span.params...),
		Exported:  s.exported,
	}
}
