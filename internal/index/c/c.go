// Package c builds a lightweight, line-based index of C and C++ source
// files (*.c *.h *.cc *.cpp *.cxx *.hpp *.hh *.hxx) so that docrot can
// check whether a documentation reference to a function, macro, type,
// enum value, namespace or class member is still valid.
//
// The index does not parse C or C++. It recognises, with brace depth
// tracked:
//
//	#define NAME                    macros
//	RET name(…)                     prototypes and definitions, also "name(" at column 0 (K&R style)
//	RET Type::method(…)             out-of-line C++ definitions
//	struct|class|union NAME {       types, with their methods and fields
//	enum [class] NAME {             enums, with their values (also CURLOPT(NAME, …) macro lists)
//	typedef … NAME;  using NAME = … aliases
//	namespace a::b {                C++ namespaces (also X_NAMESPACE_BEGIN/END macro pairs)
//
// A document may write `curl_easy_perform()`, `CURLOPT_URL`, `json::parse`,
// `basic_json::dump`, `nlohmann::json` or any "::"-boundary suffix of a full
// name. Headers are the public surface; `static` functions in sources are
// private.
//
// Once built, an Index is safe for concurrent read-only use.
package c

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
	full     string // ns::Type::member, ns::func, func, MACRO
	name     string // last segment
	file     string
	line     int
	kind     string // function, method, macro, struct, class, union, enum, member, field, alias, typedef
	exported bool
	span     *cSpan
}

// Index is a queryable snapshot of every recognised declaration under a
// root directory. Build it once with Build; every method is read-only.
type Index struct {
	stats      Stats
	namespaces []string
	nsSet      map[string]bool // namespaces, their prefixes and "::"-boundary suffixes
	example    map[string]bool // namespace → lives under tests/, examples/, docs/

	all    []symbol
	byFull map[string]int
	byName map[string][]int
	byLen  map[int][]int

	lits  *literals.Set
	defs  *defaults.Set
	flags []string
	envs  []string
}

var sourceExts = map[string]bool{".c": true, ".h": true, ".cc": true, ".cpp": true, ".cxx": true, ".hpp": true, ".hh": true, ".hxx": true, ".inl": true, ".ipp": true}

var skipDirs = map[string]bool{".git": true, "build": true, "out": true, "third_party": true, "thirdparty": true, "vendor": true, "external": true, "deps": true, "_deps": true, "node_modules": true, ".cache": true}

// Build walks root, reads every C/C++ file (skipping build and
// third-party directories and any path in exclude — prefix match on
// directories, exact match on files) and returns the resulting Index.
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
			if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), "cmake-build") || isExcludedDir(rel, exclude) {
				return filepath.SkipDir
			}
			return nil
		}
		if isExcludedFile(rel, exclude) {
			return nil
		}
		if sourceExts[strings.ToLower(path.Ext(d.Name()))] {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	ix := &Index{
		nsSet:   map[string]bool{},
		example: map[string]bool{},
		byFull:  map[string]int{},
		byName:  map[string][]int{},
		byLen:   map[int][]int{},
		lits:    literals.New(),
		defs:    defaults.New(),
	}
	if len(files) == 0 {
		return ix, nil
	}

	type parsed struct {
		syms  []decl
		lits  []string
		defs  [][3]string
		flags []string
		envs  []string
		ok    bool
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
				r.syms = parseFile(lines, masked, rel, isHeader(rel))
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
			if d.namespace != "" {
				ix.addNamespace(d.namespace)
				if example {
					ix.example[d.namespace] = true
				}
			}
			full := d.path
			if d.namespace != "" {
				full = d.namespace + "::" + d.path
			}
			ix.stats.Symbols++
			if j, exists := ix.byFull[full]; exists {
				// a definition after its prototype: keep the one with a body
				if ix.all[j].span == nil && d.span != nil || ix.all[j].span != nil && d.span != nil && d.span.bodyEnd > d.span.declLine && ix.all[j].span.bodyEnd == ix.all[j].span.declLine {
					ix.all[j].file, ix.all[j].line, ix.all[j].span = d.file, d.line, d.span
				}
				continue
			}
			s := symbol{full: full, name: d.name, file: d.file, line: d.line, kind: d.kind, exported: d.exported, span: d.span}
			idx := len(ix.all)
			ix.byFull[full] = idx
			ix.byName[d.name] = append(ix.byName[d.name], idx)
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

// addNamespace records a namespace, every prefix of it and every
// "::"-boundary suffix ("nlohmann::detail" also answers to "detail").
func (ix *Index) addNamespace(ns string) {
	parts := strings.Split(ns, "::")
	for i := 1; i <= len(parts); i++ {
		ix.nsSet[strings.Join(parts[:i], "::")] = true
	}
	for i := 1; i < len(parts); i++ {
		ix.nsSet[strings.Join(parts[i:], "::")] = true
	}
}

func isHeader(rel string) bool {
	switch strings.ToLower(path.Ext(rel)) {
	case ".h", ".hpp", ".hh", ".hxx", ".inl", ".ipp":
		return true
	}
	return false
}

func isExamplePath(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, seg := range segs[:len(segs)-1] {
		switch strings.ToLower(seg) {
		case "test", "tests", "testing", "examples", "example", "samples", "sample", "docs", "doc", "benchmarks", "bench", "benchmark", "fuzz", "fuzzing", "demo", "demos", "scripts":
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

// stripParens removes a trailing call, man-page section or template
// argument list: "curl_easy_init()" → "curl_easy_init", "CURLOPT_URL(3)"
// → "CURLOPT_URL", "basic_json<>::parse" → "basic_json::parse".
func stripParens(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + s[i+j+1:]
	}
	return strings.TrimPrefix(s, "::")
}

// lookup resolves a reference to matching declarations: the full name,
// any "::"-boundary suffix of a full name, or a bare name.
func (ix *Index) lookup(qualified string) []symbol {
	q := stripParens(qualified)
	if q == "" {
		return nil
	}
	if i, ok := ix.byFull[q]; ok {
		return []symbol{ix.all[i]}
	}
	parts := strings.Split(q, "::")
	last := parts[len(parts)-1]
	var out []symbol
	for _, i := range ix.byName[last] {
		s := ix.all[i]
		if s.full == q || strings.HasSuffix(s.full, "::"+q) {
			out = append(out, s)
		}
	}
	if len(out) > 0 {
		return out
	}
	if len(parts) == 1 {
		return ix.symbols(ix.byName[last])
	}
	if ix.nsSet[q] {
		return []symbol{{full: q, name: last, kind: "namespace"}}
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
	return lang.Stats{Files: ix.stats.Files, Namespaces: ix.stats.Namespaces, Symbols: ix.stats.Symbols}
}

// Namespaces returns every C++ namespace, prefix and suffix, sorted.
func (ix *Index) Namespaces() []string { return append([]string(nil), ix.namespaces...) }

// IsNamespace reports whether qualified names a C++ namespace (in full, by
// a prefix or by a "::"-boundary suffix).
func (ix *Index) IsNamespace(qualified string) bool { return ix.nsSet[stripParens(qualified)] }

// IsExample reports whether a namespace is declared under a tests, examples
// or docs tree.
func (ix *Index) IsExample(namespace string) bool { return ix.example[namespace] }

// Has reports whether qualified exists (see lookup for the accepted forms).
func (ix *Index) Has(qualified string) bool { return len(ix.lookup(qualified)) > 0 }

// Opaque reports whether an unknown member of qualified is not worth a
// finding: true for aliases (`using json = basic_json<>`), typedefs,
// namespaces and types that inherit; a struct or class without a base
// lists its members.
func (ix *Index) Opaque(qualified string) bool {
	for _, s := range ix.lookup(qualified) {
		switch s.kind {
		case "struct", "class", "union", "enum":
			return false
		case "struct:open", "class:open":
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
		out = append(out, s.full)
	}
	sort.Strings(out)
	return out
}

// Literals returns the identifier-like string literals of the tree.
func (ix *Index) Literals() *literals.Set { return ix.lits }

// Defaults returns the CLI11 and getenv defaults found.
func (ix *Index) Defaults() *defaults.Set { return ix.defs }

// Routes returns nil: no C or C++ web framework is recognised.
func (ix *Index) Routes() []routes.Route { return nil }

// Flags returns the long option names declared through getopt_long
// tables, CLI11 or cxxopts, sorted.
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
	parts := strings.Split(q, "::")
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
				cands = append(cands, cand{d, s.full})
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

// Span returns the declaration span of a function, method or type: its
// comment, the declaration line and the body its braces enclose.
func (ix *Index) Span(qualified string) (model.SymbolSpan, bool) {
	for _, s := range ix.lookup(qualified) {
		if s.span != nil {
			return s.symbolSpan(), true
		}
	}
	return model.SymbolSpan{}, false
}

// AllSpans returns one span per public function, method and type outside
// test and example trees, sorted by full name.
func (ix *Index) AllSpans() []model.SymbolSpan {
	var out []model.SymbolSpan
	for _, s := range ix.all {
		if s.span == nil || !s.exported || isExamplePath(s.file) {
			continue
		}
		out = append(out, s.symbolSpan())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified < out[j].Qualified })
	return out
}

func (s symbol) symbolSpan() model.SymbolSpan {
	return model.SymbolSpan{
		Qualified: s.full,
		Kind:      model.KindCSym,
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
