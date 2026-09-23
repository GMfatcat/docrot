// Package csharp builds a lightweight, line-based index of C# source files
// (*.cs) so that docrot can check whether a documentation reference to a
// namespace, type, method, property or enum member is still valid.
//
// The index does not parse C#. It recognises, with brace depth tracked:
//
//	namespace A.B;  /  namespace A.B {            → the namespace of what follows
//	[modifiers] class|struct|interface|enum|record NAME
//	[modifiers] RET Name(…)  / RET Name { get; }  / RET Name => …  / RET Name;   → Type.Name
//	enum members: Name, Name = 1
//
// A document may write `Namespace.Type.Member`, `Type.Member`, `Type`,
// `Member()` or `Namespace.Type`. XML doc comments (`/// <summary>`) are
// the declaration's comment for the comment checks.
//
// Once built, an Index is safe for concurrent read-only use.
package csharp

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
	Files      int
	Namespaces int
	Symbols    int
}

// symbol is one recognised declaration.
type symbol struct {
	namespace string // "Polly.Retry" ("" at the top level)
	qualified string // "RetryStrategyOptions", "RetryStrategyOptions.MaxRetryAttempts"
	name      string
	file      string
	line      int
	kind      string // class, struct, interface, enum, record, delegate, method, property, field, event, member
	exported  bool
	span      *csSpan
}

// Index is a queryable snapshot of every recognised declaration under a
// root directory. Build it once with Build; every method is read-only.
type Index struct {
	stats      Stats
	namespaces []string        // sorted, unique, every prefix included
	nsSet      map[string]bool // namespaces and their prefixes
	example    map[string]bool // namespace → lives under tests/, samples/, benchmarks/

	all           []symbol
	byFull        map[string]int   // "Namespace.Type.Member"
	byClassMethod map[string][]int // "Type.Member"
	byName        map[string][]int
	byLen         map[int][]int

	routes []routes.Route
	lits   *literals.Set
	defs   *defaults.Set
	flags  []string
	envs   []string
}

var skipDirs = map[string]bool{".git": true, "bin": true, "obj": true, "node_modules": true, "packages": true, "TestResults": true, ".vs": true, "artifacts": true}

// Build walks root, reads every *.cs file (skipping bin/, obj/, generated
// files and any path in exclude — prefix match on directories, exact match
// on files) and returns the resulting Index.
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
		if !strings.HasSuffix(base, ".cs") || strings.HasSuffix(base, ".Designer.cs") || strings.HasSuffix(base, ".g.cs") || strings.HasSuffix(base, ".generated.cs") || strings.HasSuffix(base, ".g.i.cs") || strings.HasPrefix(base, "TemporaryGeneratedFile") {
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
		nsSet:         map[string]bool{},
		example:       map[string]bool{},
		byFull:        map[string]int{},
		byClassMethod: map[string][]int{},
		byName:        map[string][]int{},
		byLen:         map[int][]int{},
		lits:          literals.New(),
		defs:          defaults.New(),
	}
	if len(files) == 0 {
		return ix, nil
	}

	type parsed struct {
		syms   []decl
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
				r := parsed{ok: true}
				r.syms = parseFile(lines, masked, rel)
				r.routes = parseRoutes(lines, rel)
				r.defs, r.flags, r.envs = parseDefaults(lines)
				for _, l := range lines {
					r.lits = append(r.lits, literals.Scan(l)...)
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
	for i, r := range results {
		if !r.ok {
			continue
		}
		ix.stats.Files++
		example := isExamplePath(files[i])
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
		for _, d := range r.syms {
			ix.addNamespace(d.namespace)
			if example && d.namespace != "" {
				ix.example[d.namespace] = true
			}
			full := d.path
			if d.namespace != "" {
				full = d.namespace + "." + d.path
			}
			ix.stats.Symbols++
			if _, exists := ix.byFull[full]; exists {
				continue
			}
			s := symbol{namespace: d.namespace, qualified: d.path, name: d.name, file: d.file, line: d.line, kind: d.kind, exported: d.exported, span: d.span}
			idx := len(ix.all)
			ix.byFull[full] = idx
			ix.byName[d.name] = append(ix.byName[d.name], idx)
			if parts := strings.Split(d.path, "."); len(parts) >= 2 {
				key := parts[len(parts)-2] + "." + parts[len(parts)-1]
				ix.byClassMethod[key] = append(ix.byClassMethod[key], idx)
			}
			ix.byLen[len(strings.ToLower(d.name))] = append(ix.byLen[len(strings.ToLower(d.name))], idx)
			ix.all = append(ix.all, s)
		}
	}
	for ns := range ix.nsSet {
		ix.namespaces = append(ix.namespaces, ns)
	}
	sort.Strings(ix.namespaces)
	sort.Strings(ix.flags)
	sort.Strings(ix.envs)
	ix.stats.Namespaces = len(ix.namespaces)
	return ix, nil
}

// addNamespace records a namespace and every prefix of it ("Polly" for
// "Polly.Retry").
func (ix *Index) addNamespace(ns string) {
	if ns == "" {
		return
	}
	parts := strings.Split(ns, ".")
	for i := 1; i <= len(parts); i++ {
		ix.nsSet[strings.Join(parts[:i], ".")] = true
	}
}

func isExamplePath(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, seg := range segs[:len(segs)-1] {
		switch strings.ToLower(seg) {
		case "test", "tests", "testing", "samples", "sample", "examples", "example", "benchmarks", "bench", "docs", "doc", "snippets", "demo", "demos":
			return true
		}
		if strings.HasSuffix(seg, ".Tests") || strings.HasSuffix(seg, ".Test") || strings.HasSuffix(seg, ".Benchmarks") || strings.HasSuffix(seg, ".Samples") {
			return true
		}
	}
	return false
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

// stripParens removes a trailing call suffix and a generic argument list:
// "Build()" → "Build", "Option<int>" → "Option".
func stripParens(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '<'); i >= 0 {
		s = s[:i]
	}
	return s
}

// lookup resolves a reference to matching declarations. Accepted forms:
// "Namespace.Type.Member", "Namespace.Type", "Type.Member" in any
// namespace, a namespace, "Type" and a bare member name, with a trailing
// call or generic list stripped.
func (ix *Index) lookup(qualified string) []symbol {
	q := stripParens(qualified)
	if q == "" {
		return nil
	}
	if i, ok := ix.byFull[q]; ok {
		return []symbol{ix.all[i]}
	}
	if ix.nsSet[q] {
		return []symbol{{namespace: q, name: path.Base(strings.ReplaceAll(q, ".", "/")), kind: "namespace"}}
	}
	parts := strings.Split(q, ".")
	if len(parts) >= 2 {
		key := parts[len(parts)-2] + "." + parts[len(parts)-1]
		if m := ix.byClassMethod[key]; len(m) > 0 {
			return ix.symbols(m)
		}
		if prefix := strings.Join(parts[:len(parts)-1], "."); ix.nsSet[prefix] {
			// "Polly.RetryStrategyOptions": a type of a nested namespace named
			// by an outer one — accepted when the type exists anywhere below
			// it; "Polly.Retry.Nope" stays missing
			var out []symbol
			for _, i := range ix.byName[parts[len(parts)-1]] {
				s := ix.all[i]
				if s.namespace == prefix || strings.HasPrefix(s.namespace, prefix+".") {
					out = append(out, s)
				}
			}
			return out
		}
	}
	return ix.symbols(ix.byName[parts[len(parts)-1]])
}

func (ix *Index) symbols(idx []int) []symbol {
	out := make([]symbol, 0, len(idx))
	for _, i := range idx {
		out = append(out, ix.all[i])
	}
	return out
}

// Empty reports whether no .cs file was found under root.
func (ix *Index) Empty() bool { return ix.stats.Files == 0 }

// Stats returns index-wide counters.
func (ix *Index) Stats() Stats { return ix.stats }

// Counts returns the counters in the shape the composite index reports.
func (ix *Index) Counts() lang.Stats {
	return lang.Stats{Files: ix.stats.Files, Namespaces: ix.stats.Namespaces, Symbols: ix.stats.Symbols}
}

// Namespaces returns every namespace and namespace prefix, sorted.
func (ix *Index) Namespaces() []string { return append([]string(nil), ix.namespaces...) }

// IsNamespace reports whether qualified is a namespace or a prefix of one.
func (ix *Index) IsNamespace(qualified string) bool { return ix.nsSet[stripParens(qualified)] }

// IsExample reports whether a namespace is declared under a tests, samples
// or benchmarks tree.
func (ix *Index) IsExample(namespace string) bool { return ix.example[namespace] }

// Has reports whether qualified exists (see lookup for the accepted forms).
func (ix *Index) Has(qualified string) bool { return len(ix.lookup(qualified)) > 0 }

// Opaque reports whether an unknown member of qualified is not worth a
// finding: true for a type that inherits (`class A : B`) and for
// namespaces; a type without a base lists all its members.
func (ix *Index) Opaque(qualified string) bool {
	for _, s := range ix.lookup(qualified) {
		switch s.kind {
		case "class", "struct", "interface", "record", "enum":
			return false
		case "class:open", "struct:open", "interface:open", "record:open":
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

// Symbols lists every declaration by its full name, sorted.
func (ix *Index) Symbols() []string {
	out := make([]string, 0, len(ix.all))
	for _, s := range ix.all {
		out = append(out, s.full())
	}
	sort.Strings(out)
	return out
}

func (s symbol) full() string {
	if s.namespace == "" {
		return s.qualified
	}
	return s.namespace + "." + s.qualified
}

// Literals returns the identifier-like string literals of the tree.
func (ix *Index) Literals() *literals.Set { return ix.lits }

// Defaults returns the System.CommandLine option defaults and environment
// fallbacks found.
func (ix *Index) Defaults() *defaults.Set { return ix.defs }

// Routes returns every HTTP route registration found (minimal APIs and
// attribute-routed controllers), in file order.
func (ix *Index) Routes() []routes.Route { return append([]routes.Route(nil), ix.routes...) }

// Flags returns the long option names declared through System.CommandLine,
// sorted.
func (ix *Index) Flags() []string { return append([]string(nil), ix.flags...) }

// Envs returns the environment variables the code reads, sorted.
func (ix *Index) Envs() []string { return append([]string(nil), ix.envs...) }

// Similar returns up to n existing full names whose last segment is close
// (Damerau-Levenshtein distance <= max(2, len/4), case-insensitive) to the
// last segment of qualified. Best first; ties broken alphabetically.
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
				cands = append(cands, cand{d, s.full()})
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

// Span returns the declaration span of a type, method or property: its
// XML doc comment, the declaration line and the body its braces enclose.
func (ix *Index) Span(qualified string) (model.SymbolSpan, bool) {
	for _, s := range ix.lookup(qualified) {
		if s.span != nil {
			return s.symbolSpan(), true
		}
	}
	return model.SymbolSpan{}, false
}

// AllSpans returns one span per public type, method and property outside
// test and sample trees, sorted by full name.
func (ix *Index) AllSpans() []model.SymbolSpan {
	var out []model.SymbolSpan
	for _, s := range ix.all {
		if s.span == nil || !s.exported || ix.example[s.namespace] {
			continue
		}
		out = append(out, s.symbolSpan())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified < out[j].Qualified })
	return out
}

func (s symbol) symbolSpan() model.SymbolSpan {
	return model.SymbolSpan{
		Qualified: s.full(),
		Kind:      model.KindCSharpSym,
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
