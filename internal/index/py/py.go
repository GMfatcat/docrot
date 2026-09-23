// Package py builds a lightweight, regex-based index of Python source files
// (*.py) so that docrot can check whether a documentation reference to a
// Python module, class, method, function or constant is still valid.
//
// The index does not parse Python; it recognises, per line:
//
//	def NAME(  / async def NAME(     (indentation tracked)
//	class NAME(  / class NAME:       (indentation tracked)
//	NAME = ...                       (top-level ALL_CAPS constant)
//
// A def indented under a class becomes "Class.method" using an indentation
// stack that tracks the innermost enclosing class. Module names come from
// the file stem; files under a directory chain of __init__.py packages get
// dotted names ("pkg", "pkg.sub", "pkg.mod").
//
// Once built, an Index is safe for concurrent read-only use.
package py

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"docrot/internal/index/defaults"
	"docrot/internal/index/literals"
	"docrot/internal/index/routes"
)

// Stats summarises what Build found.
type Stats struct {
	Files   int // number of .py files read
	Modules int // number of distinct module names
	Symbols int // number of recognised declarations
}

// symbol is one recognised declaration.
type symbol struct {
	module    string // dotted module name
	qualified string // name within the module, e.g. "Class.method" or "CONST"
	name      string // bare last-part identifier, e.g. "method" or "CONST"
	file      string // relative file path (forward slashes)
	line      int    // 1-based line number
	// span is the docstring/body geometry of a def or class declaration;
	// nil for constants and imported names.
	span *pySpan
	// assign marks a module-level assignment that is not an ALL_CAPS
	// constant (a type alias, a proxy object): it names something in its
	// own module but is not treated as re-exported by parent packages.
	assign bool
}

// Index is a queryable snapshot of every recognised Python declaration under
// a root directory. Build it once with Build; every method is read-only.
type Index struct {
	stats   Stats
	modules []string // sorted, unique
	modSet  map[string]bool
	example map[string]bool // modules whose files live under tests/, docs/, examples/…

	byFull        map[string]symbol   // "module.qualified" -> first declaration
	byClassMethod map[string][]symbol // "Class.method" -> declarations across modules
	byName        map[string][]symbol // bare last-part name -> declarations

	all []symbol // every recognised declaration, deduplicated by "module.qualified"
	// byLen groups indexes into all by the byte length of the lower-cased
	// bare name, so Similar only scores names whose length is within the
	// edit-distance budget.
	byLen map[int][]int

	routes []routes.Route // HTTP route registrations, in file order
	lits   *literals.Set  // identifier-like string literals
	defs   *defaults.Set  // typer/click/argparse option defaults, os.getenv defaults
}

var (
	reClass = regexp.MustCompile(`^class\s+(\w+)\s*[:(]`)
	reDef   = regexp.MustCompile(`^(?:async\s+)?def\s+(\w+)\s*\(`)
	reConst = regexp.MustCompile(`^([A-Za-z_]\w*)\s*(?::[^=]*)?=[^=]`) // module-level name (any case), annotated or not
	reFrom  = regexp.MustCompile(`^from\s+[\w.]+\s+import\s+(.+)$`)
	reImp   = regexp.MustCompile(`^import\s+(.+)$`)
)

// Build walks root, reads every *.py file (skipping .git, vendor,
// node_modules and any path in exclude — prefix match on directories, exact
// match on files) and returns the resulting Index.
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
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || isExcludedDir(rel, exclude) {
				return filepath.SkipDir
			}
			return nil
		}
		if isExcludedFile(rel, exclude) {
			return nil
		}
		if strings.HasSuffix(rel, ".py") {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	// First pass: find every directory that is a Python package
	// (contains __init__.py), needed to compute dotted module names.
	initDirs := make(map[string]bool)
	for _, rel := range files {
		if path.Base(rel) == "__init__.py" {
			initDirs[path.Dir(rel)] = true
		}
	}

	type parsed struct {
		module string
		syms   []symbol
		routes []routes.Route
		lits   []string
		defs   [][3]string
		ok     bool
	}
	results := make([]parsed, len(files))

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(files) && len(files) > 0 {
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
				mod := moduleName(rel, initDirs)
				content := string(data)
				syms := parseFile(content, rel)
				lines := strings.Split(content, "\n")
				rts := parseRoutes(lines, rel)
				var lits []string
				for _, l := range lines {
					lits = append(lits, literals.Scan(l)...)
				}
				results[i] = parsed{module: mod, syms: syms, routes: rts, lits: lits, defs: parseDefaults(lines), ok: true}
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	ix := &Index{
		byFull:        make(map[string]symbol),
		byClassMethod: make(map[string][]symbol),
		byName:        make(map[string][]symbol),
		lits:          literals.New(),
		defs:          defaults.New(),
	}
	modSet := make(map[string]bool)
	seenAll := make(map[string]bool)

	for _, r := range results {
		if !r.ok {
			continue
		}
		ix.stats.Files++
		modSet[r.module] = true
		ix.routes = append(ix.routes, r.routes...)
		ix.lits.AddAll(r.lits)
		for _, d := range r.defs {
			ix.defs.Add(d[0], d[1], d[2])
		}
		for _, s := range r.syms {
			s.module = r.module
			ix.stats.Symbols++

			fullKey := s.module + "." + s.qualified
			if _, exists := ix.byFull[fullKey]; !exists {
				ix.byFull[fullKey] = s
			}
			if strings.Contains(s.qualified, ".") {
				parts := strings.Split(s.qualified, ".")
				cmKey := parts[len(parts)-2] + "." + parts[len(parts)-1]
				ix.byClassMethod[cmKey] = append(ix.byClassMethod[cmKey], s)
			}
			ix.byName[s.name] = append(ix.byName[s.name], s)

			if !seenAll[fullKey] {
				seenAll[fullKey] = true
				ix.all = append(ix.all, s)
			}
		}
	}

	ix.byLen = map[int][]int{}
	for i, s := range ix.all {
		n := len(strings.ToLower(s.name))
		ix.byLen[n] = append(ix.byLen[n], i)
	}
	ix.modSet = modSet
	ix.example = map[string]bool{}
	for _, sym := range ix.all {
		if isExamplePath(sym.file) {
			ix.example[sym.module] = true
		}
	}
	for m := range modSet {
		ix.modules = append(ix.modules, m)
	}
	sort.Strings(ix.modules)
	ix.stats.Modules = len(ix.modules)

	return ix, nil
}

// moduleName computes the dotted module name for a relative .py file path,
// given the set of directories (relative, forward slashes) that contain an
// __init__.py.
func moduleName(rel string, initDirs map[string]bool) string {
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	base := strings.TrimSuffix(path.Base(rel), ".py")

	var parts []string
	if base != "__init__" {
		parts = append(parts, base)
	}
	for dir != "" && initDirs[dir] {
		parts = append([]string{path.Base(dir)}, parts...)
		next := path.Dir(dir)
		if next == "." {
			next = ""
		}
		dir = next
	}
	if len(parts) == 0 {
		return base
	}
	return strings.Join(parts, ".")
}

// parseFile scans one Python source file's content and returns every
// recognised declaration, qualified within the module (e.g. "func",
// "Class.method", "CONST").
func parseFile(content, rel string) []symbol {
	type frame struct {
		indent int
		name   string
	}
	var stack []frame
	var syms []symbol

	lines := strings.Split(content, "\n")
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		leftTrimmed := strings.TrimLeft(line, " \t")
		indent := len(line) - len(leftTrimmed)
		trimmed := strings.TrimRight(leftTrimmed, " \t")
		if trimmed == "" {
			continue
		}

		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}

		if m := reClass.FindStringSubmatch(trimmed); m != nil {
			name := m[1]
			qualified := name
			if len(stack) > 0 {
				qualified = stack[len(stack)-1].name + "." + name
			}
			syms = append(syms, symbol{
				qualified: qualified, name: name, file: rel, line: i + 1,
				span: declSpan(lines, i, indent, false),
			})
			stack = append(stack, frame{indent: indent, name: name})
			continue
		}

		if m := reDef.FindStringSubmatch(trimmed); m != nil {
			name := m[1]
			qualified := name
			if len(stack) > 0 {
				qualified = stack[len(stack)-1].name + "." + name
			}
			syms = append(syms, symbol{
				qualified: qualified, name: name, file: rel, line: i + 1,
				span: declSpan(lines, i, indent, true),
			})
			continue
		}

		if indent == 0 {
			if m := reConst.FindStringSubmatch(trimmed); m != nil {
				name := m[1]
				syms = append(syms, symbol{qualified: name, name: name, file: rel, line: i + 1, assign: strings.ToUpper(name) != name})
				continue
			}
			// top-level imports become names of this module ("from starlette
			// import status" in fastapi/__init__.py makes fastapi.status real)
			if m := reFrom.FindStringSubmatch(trimmed); m != nil {
				list := m[1]
				for j := i + 1; strings.Contains(list, "(") && !strings.Contains(list, ")") && j < len(lines); j++ {
					list += " " + strings.TrimSpace(strings.TrimRight(lines[j], "\r"))
				}
				for _, name := range importedNames(list) {
					syms = append(syms, symbol{qualified: name, name: name, file: rel, line: i + 1})
				}
				continue
			}
			if m := reImp.FindStringSubmatch(trimmed); m != nil {
				for _, name := range importedNames(m[1]) {
					syms = append(syms, symbol{qualified: name, name: name, file: rel, line: i + 1})
				}
			}
		}
	}
	return syms
}

// importedNames returns the local names bound by an import list such as
// "a, b as c, (d,\n e)" or "os.path as p": the alias when present,
// otherwise the last dotted component. "*" is ignored.
func importedNames(list string) []string {
	list = strings.NewReplacer("(", " ", ")", " ", "\\", " ").Replace(list)
	if i := strings.Index(list, "#"); i >= 0 {
		list = list[:i]
	}
	var out []string
	for _, part := range strings.Split(list, ",") {
		f := strings.Fields(part)
		if len(f) == 0 || f[0] == "*" {
			continue
		}
		name := f[0]
		if len(f) == 3 && f[1] == "as" {
			name = f[2]
		} else if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		if name != "" && isIdent(name) {
			out = append(out, name)
		}
	}
	return out
}

func isIdent(s string) bool {
	for i, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return s != ""
}

func isExcludedDir(rel string, exclude []string) bool {
	for _, e := range exclude {
		e = cleanExclude(e)
		if e == "" {
			continue
		}
		if rel == e || strings.HasPrefix(rel, e+"/") {
			return true
		}
	}
	return false
}

func isExcludedFile(rel string, exclude []string) bool {
	for _, e := range exclude {
		e = cleanExclude(e)
		if e == "" {
			continue
		}
		if rel == e {
			return true
		}
	}
	return false
}

func cleanExclude(e string) string {
	e = filepath.ToSlash(e)
	return strings.Trim(e, "/")
}

func stripParens(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Empty reports whether no .py files were found under root.
func (ix *Index) Empty() bool { return ix.stats.Files == 0 }

// Modules returns every recognised module name, sorted.
func (ix *Index) Modules() []string {
	out := make([]string, len(ix.modules))
	copy(out, ix.modules)
	return out
}

// Stats returns index-wide counters.
func (ix *Index) Stats() Stats { return ix.stats }

// Has reports whether qualified exists. Accepted forms: "module.name",
// "Class.method", "module.Class.method", "name". A trailing "(...)" or "()"
// call suffix is stripped before lookup.
func (ix *Index) Has(qualified string) bool {
	return len(ix.resolve(qualified)) > 0
}

// File returns the declaring file (relative, forward slashes) and 1-based
// line of qualified, if known.
func (ix *Index) File(qualified string) (string, int, bool) {
	m := ix.resolve(qualified)
	if len(m) == 0 {
		return "", 0, false
	}
	return m[0].file, m[0].line, true
}

// resolve tries, in order: exact "module.qualified", "Class.method" in any
// module, then bare name in any module/class.
func (ix *Index) resolve(qualified string) []symbol {
	q := stripParens(qualified)
	if q == "" {
		return nil
	}
	if s, ok := ix.byFull[q]; ok {
		return []symbol{s}
	}
	if ix.modSet[q] {
		// "fastapi.responses": a module or package is a valid reference
		return []symbol{{module: q, name: q}}
	}
	parts := strings.Split(q, ".")
	if len(parts) >= 2 {
		cmKey := parts[len(parts)-2] + "." + parts[len(parts)-1]
		if m := ix.byClassMethod[cmKey]; len(m) > 0 {
			return m
		}
		// package re-export: "fastapi.FastAPI" when FastAPI is declared in
		// fastapi/applications.py (the package's __init__ re-exports it)
		pkg := strings.Join(parts[:len(parts)-1], ".")
		if ix.modSet[pkg] {
			for _, sym := range ix.byName[parts[len(parts)-1]] {
				if sym.module == pkg || strings.HasPrefix(sym.module, pkg+".") && !sym.assign {
					return []symbol{sym}
				}
			}
			// the prefix is a module of this tree and the name is not in it
			// (nor re-exported from below): a same-named object elsewhere
			// does not make "pkg.Name" true
			return nil
		}
	}
	last := parts[len(parts)-1]
	return ix.byName[last]
}

// IsExampleModule reports whether module is defined under a tests, docs,
// examples, scripts or benchmarks tree rather than in the library itself.
func (ix *Index) IsExampleModule(module string) bool { return ix.example[module] }

func isExamplePath(rel string) bool {
	first, _, _ := strings.Cut(rel, "/")
	switch strings.ToLower(first) {
	case "tests", "test", "testing", "docs", "doc", "docs_src", "examples", "example", "samples", "scripts", "benchmarks", "bench", "demo", "demos":
		return true
	}
	return false
}

// Literals returns the identifier-like string literals of the tree.
func (ix *Index) Literals() *literals.Set { return ix.lits }

// Defaults returns the option and environment-variable defaults found.
func (ix *Index) Defaults() *defaults.Set { return ix.defs }

// Routes returns every HTTP route registration found (FastAPI/Flask
// decorators, add_api_route, Starlette Route/Mount, Django path), in file
// order.
func (ix *Index) Routes() []routes.Route { return append([]routes.Route(nil), ix.routes...) }

// IsModule reports whether qualified names a module or package of the tree.
func (ix *Index) IsModule(qualified string) bool { return ix.modSet[stripParens(qualified)] }

// Similar returns up to n existing candidates (formatted "module.qualified")
// whose bare name is close (Damerau-Levenshtein distance <= max(2, len/4),
// case-insensitive) to the last dot-separated part of qualified. Best
// (smallest distance) first; ties broken alphabetically.
func (ix *Index) Similar(qualified string, n int) []string {
	if n <= 0 {
		return nil
	}
	q := stripParens(qualified)
	parts := strings.Split(q, ".")
	last := parts[len(parts)-1]
	lastLower := strings.ToLower(last)
	threshold := len([]rune(last)) / 4
	if threshold < 2 {
		threshold = 2
	}

	type cand struct {
		dist      int
		formatted string
	}
	var cands []cand
	qLen := len(lastLower)
	for n := qLen - threshold; n <= qLen+threshold; n++ {
		for _, i := range ix.byLen[n] {
			s := ix.all[i]
			dist := damerauLevenshtein(lastLower, strings.ToLower(s.name))
			if dist <= threshold {
				cands = append(cands, cand{dist: dist, formatted: s.module + "." + s.qualified})
			}
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].formatted < cands[j].formatted
	})
	if len(cands) > n {
		cands = cands[:n]
	}
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.formatted
	}
	return out
}

// damerauLevenshtein computes the optimal-string-alignment edit distance
// between a and b (insert, delete, substitute, adjacent transposition).
func damerauLevenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := d[i-1][j] + 1
			ins := d[i][j-1] + 1
			sub := d[i-1][j-1] + cost
			best := del
			if ins < best {
				best = ins
			}
			if sub < best {
				best = sub
			}
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				if t := d[i-2][j-2] + cost; t < best {
					best = t
				}
			}
			d[i][j] = best
		}
	}
	return d[la][lb]
}
