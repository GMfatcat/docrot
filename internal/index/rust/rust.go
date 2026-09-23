// Package rust builds a lightweight, line-based index of Rust source files
// (*.rs) so that docrot can check whether a documentation reference to a
// crate, module, function, type, constant or method is still valid.
//
// The index does not parse Rust. It recognises declaration lines after
// their visibility and qualifiers ("pub(crate) async unsafe fn"):
//
//	fn NAME / struct NAME / enum NAME / union NAME / trait NAME / type NAME
//	const NAME / static NAME / mod NAME { / macro_rules! NAME
//	impl [Trait for] TYPE {      → the fns inside become TYPE::fn
//	pub use path::{A, B as C};   → A and C become names of this module
//
// Brace depth is tracked so that methods attach to their impl or trait and
// inline modules add a path segment. Module paths follow the file tree:
// src/lib.rs and src/main.rs are the crate root, src/io.rs and src/io/mod.rs
// are io, src/net/tcp.rs is net::tcp; tests/, examples/ and benches/ are
// example modules. The crate name comes from the nearest Cargo.toml
// ([package] name, with "-" read as "_").
//
// Once built, an Index is safe for concurrent read-only use.
package rust

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
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
	Files   int // number of .rs files read
	Crates  int // number of crates (Cargo.toml packages, or one implicit crate)
	Modules int // number of distinct module paths, crates included
	Symbols int // number of recognised declarations
}

// symbol is one recognised declaration.
type symbol struct {
	full     string // crate::module::Item, crate::module::Type::method
	name     string // last segment
	module   string // owning module path
	file     string // relative file path (forward slashes)
	line     int    // 1-based line number
	kind     string // fn, struct, enum, union, trait, type, const, static, macro, use
	exported bool
	span     *rustSpan
}

// Index is a queryable snapshot of every recognised Rust declaration under
// a root directory. Build it once with Build; every method is read-only.
type Index struct {
	stats     Stats
	crates    []string
	modules   []string        // sorted, unique full module paths
	modSet    map[string]bool // full module paths and crate names
	modSuffix map[string]bool // every "::"-boundary suffix of a module path
	example   map[string]bool // module path → lives under tests/, examples/, benches/

	all    []symbol
	byFull map[string]int
	byName map[string][]int
	byLen  map[int][]int
	// globs maps a module to the modules it re-exports wholesale with
	// `pub use target::*` (clap re-exports clap_builder that way).
	globs map[string][]string

	routes []routes.Route
	lits   *literals.Set
	defs   *defaults.Set
	flags  []string
	envs   []string
}

var (
	reCargoName = regexp.MustCompile(`^name\s*=\s*"([^"]+)"`)
	reTOMLSect  = regexp.MustCompile(`^\[([^\]]+)\]`)
)

// Build walks root, reads every *.rs file (skipping .git, target, vendor,
// node_modules and any path in exclude — prefix match on directories, exact
// match on files) and returns the resulting Index.
func Build(root string, exclude []string) (*Index, error) {
	var files, manifests []string
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
			name := d.Name()
			if name == ".git" || name == "target" || name == "vendor" || name == "node_modules" || isExcludedDir(rel, exclude) {
				return filepath.SkipDir
			}
			return nil
		}
		if isExcludedFile(rel, exclude) {
			return nil
		}
		switch {
		case strings.HasSuffix(rel, ".rs"):
			files = append(files, rel)
		case path.Base(rel) == "Cargo.toml":
			manifests = append(manifests, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	ix := &Index{
		modSet:    map[string]bool{},
		modSuffix: map[string]bool{},
		example:   map[string]bool{},
		byFull:    map[string]int{},
		byName:    map[string][]int{},
		byLen:     map[int][]int{},
		globs:     map[string][]string{},
		lits:      literals.New(),
		defs:      defaults.New(),
	}
	if len(files) == 0 {
		return ix, nil
	}

	// crate directories: the directory of every Cargo.toml with a [package]
	crateDirs := map[string]string{}
	for _, m := range manifests {
		if name := cargoPackageName(filepath.Join(root, filepath.FromSlash(m))); name != "" {
			dir := path.Dir(m)
			if dir == "." {
				dir = ""
			}
			crateDirs[dir] = name
		}
	}
	fallback := crateName(filepath.Base(root))
	if fallback == "" || fallback == "." {
		fallback = "crate"
	}

	type parsed struct {
		module  string
		example bool
		syms    []decl
		globs   []glob
		routes  []routes.Route
		lits    []string
		defs    [][3]string
		flags   []string
		envs    []string
		ok      bool
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
				crate, inCrate := crateOf(rel, crateDirs, fallback)
				module, example := moduleOf(crate, inCrate)
				lines := strings.Split(string(data), "\n")
				masked := maskAll(lines)
				r := parsed{module: module, example: example, ok: true}
				r.syms, r.globs = parseFile(lines, masked, rel)
				r.routes = parseRoutes(lines, rel)
				r.defs, r.flags, r.envs = parseDefaults(lines)
				for _, l := range lines {
					r.lits = append(r.lits, literals.Scan(stripLifetimes(l))...)
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

	crateSet := map[string]bool{}
	type rawGlob struct{ module, target string }
	var rawGlobs []rawGlob
	type rawAlias struct{ full, module, src string }
	var rawAliases []rawAlias
	flagSet := map[string]bool{}
	envSet := map[string]bool{}
	for _, r := range results {
		if !r.ok {
			continue
		}
		ix.stats.Files++
		crate, _, _ := strings.Cut(r.module, "::")
		crateSet[crate] = true
		ix.addModule(r.module)
		if r.example {
			ix.example[r.module] = true
		}
		for _, g := range r.globs {
			module := r.module
			if g.mods != "" {
				module += "::" + g.mods
			}
			rawGlobs = append(rawGlobs, rawGlob{module, g.target})
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
		for _, d := range r.syms {
			module := r.module
			if d.mods != "" {
				module += "::" + d.mods
				ix.addModule(module)
			}
			full := module + "::" + d.path
			ix.stats.Symbols++
			if _, exists := ix.byFull[full]; exists {
				continue
			}
			s := symbol{full: full, name: d.name, module: module, file: d.file, line: d.line, kind: d.kind, exported: d.exported, span: d.span}
			if d.kind == "use" && d.src != "" {
				rawAliases = append(rawAliases, rawAlias{full, module, d.src})
			}
			ix.byFull[full] = len(ix.all)
			ix.byName[d.name] = append(ix.byName[d.name], len(ix.all))
			ix.byLen[len(strings.ToLower(d.name))] = append(ix.byLen[len(strings.ToLower(d.name))], len(ix.all))
			ix.all = append(ix.all, s)
		}
	}
	for c := range crateSet {
		ix.crates = append(ix.crates, c)
	}
	sort.Strings(ix.crates)
	for _, g := range rawGlobs {
		target := ix.resolveUse(g.module, g.target, crateSet)
		if target != g.module {
			ix.globs[g.module] = append(ix.globs[g.module], target)
		}
	}
	// `pub use self::net::tcp;` makes my_crate::tcp another name of the
	// module my_crate::net::tcp
	for _, a := range rawAliases {
		if target := ix.resolveUse(a.module, a.src, crateSet); ix.modSet[target] && target != a.full {
			ix.globs[a.full] = append(ix.globs[a.full], target)
		}
	}
	sort.Strings(ix.flags)
	sort.Strings(ix.envs)
	for m := range ix.modSet {
		ix.modules = append(ix.modules, m)
	}
	sort.Strings(ix.modules)
	ix.stats.Crates = len(ix.crates)
	ix.stats.Modules = len(ix.modules)
	return ix, nil
}

// resolveUse turns a `use` path written inside module into a full module
// path: crate:: is the crate root, self:: the module, super:: its parent,
// a known crate name stands alone, and anything else is a child module of
// the writing module when one exists (else taken as an external crate).
func (ix *Index) resolveUse(module, target string, crates map[string]bool) string {
	crate, _, _ := strings.Cut(module, "::")
	first, rest, _ := strings.Cut(target, "::")
	join := func(a, b string) string {
		if b == "" {
			return a
		}
		return a + "::" + b
	}
	switch first {
	case "crate":
		return join(crate, rest)
	case "self":
		return join(module, rest)
	case "super":
		parent := module
		if i := strings.LastIndex(parent, "::"); i >= 0 {
			parent = parent[:i]
		}
		return join(parent, rest)
	}
	if crates[first] {
		return target
	}
	if ix.modSet[join(module, target)] {
		return join(module, target)
	}
	return target
}

// addModule records a module path and every suffix of it on a "::" boundary
// ("mycrate::net::tcp" also answers to "net::tcp" and "tcp").
func (ix *Index) addModule(m string) {
	if ix.modSet[m] {
		return
	}
	ix.modSet[m] = true
	parts := strings.Split(m, "::")
	for i := range parts {
		ix.modSuffix[strings.Join(parts[i:], "::")] = true
	}
}

// cargoPackageName reads [package] name from a Cargo.toml; "" for a
// workspace root or a virtual manifest.
func cargoPackageName(file string) string {
	data, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	sect := ""
	for _, raw := range strings.Split(string(data), "\n") {
		l := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if m := reTOMLSect.FindStringSubmatch(l); m != nil {
			sect = strings.TrimSpace(m[1])
			continue
		}
		if sect == "package" {
			if m := reCargoName.FindStringSubmatch(l); m != nil {
				return crateName(m[1])
			}
		}
	}
	return ""
}

// crateName is the identifier form of a package name: "-" becomes "_".
func crateName(name string) string { return strings.ReplaceAll(name, "-", "_") }

// crateOf finds the crate a file belongs to (the nearest ancestor with a
// package manifest) and the file's path relative to that crate directory.
func crateOf(rel string, crateDirs map[string]string, fallback string) (crate, inCrate string) {
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	for {
		if name, ok := crateDirs[dir]; ok {
			return name, strings.TrimPrefix(strings.TrimPrefix(rel, dir), "/")
		}
		if dir == "" {
			break
		}
		next := path.Dir(dir)
		if next == "." {
			next = ""
		}
		dir = next
	}
	return fallback, rel
}

// moduleOf maps a file path inside a crate to its module path.
func moduleOf(crate, inCrate string) (module string, example bool) {
	segs := strings.Split(strings.TrimSuffix(inCrate, ".rs"), "/")
	if len(segs) > 0 && segs[0] == "src" {
		segs = segs[1:]
	} else if len(segs) > 0 {
		switch segs[0] {
		case "tests", "examples", "benches", "test", "example":
			example = true
			segs = segs[1:]
		}
	}
	if len(segs) > 0 && segs[0] == "bin" {
		segs = segs[1:]
	}
	if n := len(segs); n > 0 {
		switch segs[n-1] {
		case "lib", "main", "mod":
			segs = segs[:n-1]
		}
	}
	parts := append([]string{crate}, segs...)
	return strings.Join(parts, "::"), example
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

// stripParens removes a trailing call or macro suffix: "f()" → "f",
// "m!(x)" → "m", "f::<T>()" → "f".
func stripParens(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "(!"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, "::<>")
	if i := strings.Index(s, "::<"); i >= 0 {
		s = s[:i]
	}
	return s
}

// lookup resolves a reference to matching declarations. Accepted forms:
// "crate::module::Item" (also with the literal "crate" prefix), any
// "::"-boundary suffix of that ("module::Item", "Type::method", "Item"),
// and a bare name, with a trailing call stripped.
func (ix *Index) lookup(qualified string) []symbol {
	q := stripParens(qualified)
	if q == "" {
		return nil
	}
	q = strings.TrimPrefix(q, "::")
	for _, p := range []string{"crate::", "self::", "super::"} {
		q = strings.TrimPrefix(q, p)
	}
	if out := ix.find(q, 0); len(out) > 0 {
		return out
	}
	if q != "" && ix.IsNamespace(q) {
		// a module is a valid thing to name
		parts := strings.Split(q, "::")
		return []symbol{{full: q, name: parts[len(parts)-1], module: q, kind: "mod"}}
	}
	return nil
}

// find matches q exactly, by any "::"-boundary suffix, and then through
// the glob re-exports of each of its prefixes (clap::Command is
// clap_builder::Command when clap says `pub use clap_builder::*`).
func (ix *Index) find(q string, depth int) []symbol {
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
	if len(out) > 0 || depth > 3 {
		return out
	}
	for n := len(parts) - 1; n >= 1; n-- {
		prefix := strings.Join(parts[:n], "::")
		for _, target := range ix.globs[prefix] {
			if found := ix.find(target+"::"+strings.Join(parts[n:], "::"), depth+1); len(found) > 0 {
				return found
			}
		}
	}
	return nil
}

// Empty reports whether no .rs files were found under root.
func (ix *Index) Empty() bool { return ix.stats.Files == 0 }

// Stats returns index-wide counters.
func (ix *Index) Stats() Stats { return ix.stats }

// Counts returns the counters in the shape the composite index reports.
func (ix *Index) Counts() lang.Stats {
	return lang.Stats{Files: ix.stats.Files, Namespaces: ix.stats.Modules, Symbols: ix.stats.Symbols}
}

// Crates returns every crate name, sorted.
func (ix *Index) Crates() []string { return append([]string(nil), ix.crates...) }

// Namespaces returns every full module path (crates included), sorted.
func (ix *Index) Namespaces() []string { return append([]string(nil), ix.modules...) }

// IsNamespace reports whether qualified names a crate or module, in full
// ("mycrate::net::tcp") or by any "::"-boundary suffix ("net::tcp",
// "tcp"), or is the literal "crate".
func (ix *Index) IsNamespace(qualified string) bool {
	q := strings.TrimPrefix(stripParens(qualified), "crate::")
	if q == "crate" {
		return true
	}
	return ix.isNamespace(q, 0)
}

func (ix *Index) isNamespace(q string, depth int) bool {
	if ix.modSet[q] || ix.modSuffix[q] || len(ix.globs[q]) > 0 {
		return true
	}
	if depth > 3 {
		return false
	}
	parts := strings.Split(q, "::")
	for n := len(parts) - 1; n >= 1; n-- {
		for _, target := range ix.globs[strings.Join(parts[:n], "::")] {
			if ix.isNamespace(target+"::"+strings.Join(parts[n:], "::"), depth+1) {
				return true
			}
		}
	}
	return false
}

// IsExample reports whether a module lives under tests/, examples/ or
// benches/.
func (ix *Index) IsExample(module string) bool {
	if ix.example[module] {
		return true
	}
	for m := range ix.example {
		if strings.HasSuffix(m, "::"+module) {
			return true
		}
	}
	return false
}

// Has reports whether qualified exists (see lookup for the accepted forms).
func (ix *Index) Has(qualified string) bool { return len(ix.lookup(qualified)) > 0 }

// Opaque is true for every declaration: derived and blanket trait
// methods (clone, to_string, into) are not in the index.
func (ix *Index) Opaque(string) bool { return true }

// File returns the declaring file (relative, forward slashes) and 1-based
// line of qualified, if known.
func (ix *Index) File(qualified string) (string, int, bool) {
	m := ix.lookup(qualified)
	if len(m) == 0 {
		return "", 0, false
	}
	return m[0].file, m[0].line, true
}

// Symbols lists every declaration by its full path, sorted.
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

// Defaults returns the clap and environment defaults found.
func (ix *Index) Defaults() *defaults.Set { return ix.defs }

// Routes returns every HTTP route registration found (axum, actix-web,
// rocket, tide), in file order.
func (ix *Index) Routes() []routes.Route { return append([]routes.Route(nil), ix.routes...) }

// Flags returns the long option names declared through clap (derive
// attributes or the builder), sorted.
func (ix *Index) Flags() []string { return append([]string(nil), ix.flags...) }

// Envs returns the environment variables the code reads, sorted.
func (ix *Index) Envs() []string { return append([]string(nil), ix.envs...) }

// Similar returns up to n existing full paths whose last segment is close
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

// Span returns the declaration span of a fn, type, trait or impl method:
// its "///" comment, the declaration line and the body its braces enclose.
func (ix *Index) Span(qualified string) (model.SymbolSpan, bool) {
	for _, s := range ix.lookup(qualified) {
		if s.span != nil {
			return s.symbolSpan(), true
		}
	}
	return model.SymbolSpan{}, false
}

// AllSpans returns one span per exported fn, method, struct, enum, union,
// trait and type alias outside example modules, sorted by full path.
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
		Qualified: s.full,
		Kind:      model.KindRustSym,
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
