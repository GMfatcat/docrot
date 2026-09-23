// Package odin builds a lightweight, regex-based index of Odin source files
// (*.odin) so that docrot can check whether a documentation reference to an
// Odin package, procedure, type or constant is still valid.
//
// The index is intentionally shallow: it does not parse Odin, it recognises
// a handful of declaration shapes at line start (after optional leading
// whitespace and attribute annotations such as "@(private)"):
//
//	package NAME
//	NAME :: proc ...            (also "#force_inline proc", `proc "c"`, "proc(")
//	NAME :: struct|enum|union|distinct|bit_set|bit_field|#type ...
//	NAME :: <anything else>     (constant or alias, e.g. "FOO :: 42")
//	NAME: TYPE                  (top-level global, zero indentation only)
//	NAME := VALUE               (top-level global, zero indentation only)
//
// Once built, an Index is safe for concurrent read-only use.
package odin

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"docrot/internal/index/defaults"
	"docrot/internal/index/lang"
	"docrot/internal/index/literals"
	"docrot/internal/index/routes"
)

var _ lang.Index = (*Index)(nil)

// Stats summarises what Build found.
type Stats struct {
	Files    int // number of .odin files read
	Packages int // number of distinct package names
	Symbols  int // number of recognised declarations
}

// symbol is one recognised declaration.
type symbol struct {
	name string // bare identifier
	pkg  string // owning package name
	file string // relative file path (forward slashes)
	line int    // 1-based line number
	proc bool   // true when this is a "NAME :: proc" declaration
	// span is the comment/body geometry of a proc or type declaration; nil
	// for constants, aliases and globals.
	span *odinSpan
}

// Index is a queryable snapshot of every recognised Odin declaration under a
// root directory. Build it once with Build; every method is read-only.
type Index struct {
	stats    Stats
	packages []string // sorted, unique
	lits     *literals.Set

	byQual map[string]symbol   // "pkg.name" -> first declaration
	byName map[string][]symbol // "name" -> declarations across packages, insertion order

	all       []symbol // every recognised declaration, deduplicated by pkg+name, for Similar
	procNames []string // sorted, unique bare proc names
}

var (
	rePackageLine = regexp.MustCompile(`^package\s+(\w+)\s*$`)
	reAttrPrefix  = regexp.MustCompile(`^@\([^)]*\)\s*`)
	reDoubleColon = regexp.MustCompile(`^(\w+)\s*::\s*(.*)$`)
	reSingleColon = regexp.MustCompile(`^(\w+)\s*:\s*\S.*$`)
	reProcDecl    = regexp.MustCompile(`^(#force_inline\s+)?proc\b`)
)

// Build walks root, reads every *.odin file (skipping .git, vendor,
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
		if strings.HasSuffix(rel, ".odin") {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	type fileResult struct {
		pkg  string
		syms []symbol
		lits []string
		ok   bool
	}
	results := make([]fileResult, len(files))

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
				pkg, syms := parseFile(string(data), rel)
				var lits []string
				for _, l := range strings.Split(string(data), "\n") {
					lits = append(lits, literals.Scan(l)...)
				}
				results[i] = fileResult{pkg: pkg, syms: syms, lits: lits, ok: true}
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	ix := &Index{
		byQual: make(map[string]symbol),
		byName: make(map[string][]symbol),
		lits:   literals.New(),
	}
	pkgSet := make(map[string]bool)
	seenAll := make(map[string]bool)
	procSet := make(map[string]bool)

	for i, r := range results {
		if !r.ok {
			continue
		}
		ix.stats.Files++
		ix.lits.AddAll(r.lits)
		pkgName := r.pkg
		if pkgName == "" {
			dir := filepath.ToSlash(filepath.Dir(files[i]))
			if dir == "." {
				pkgName = "main"
			} else {
				pkgName = filepath.Base(dir)
			}
		}
		pkgSet[pkgName] = true
		for _, s := range r.syms {
			s.pkg = pkgName
			ix.stats.Symbols++
			qk := pkgName + "." + s.name
			if _, exists := ix.byQual[qk]; !exists {
				ix.byQual[qk] = s
			}
			ix.byName[s.name] = append(ix.byName[s.name], s)
			if !seenAll[qk] {
				seenAll[qk] = true
				ix.all = append(ix.all, s)
			}
			if s.proc && !procSet[s.name] {
				procSet[s.name] = true
			}
		}
	}

	for pkg := range pkgSet {
		ix.packages = append(ix.packages, pkg)
	}
	sort.Strings(ix.packages)
	ix.stats.Packages = len(ix.packages)

	for name := range procSet {
		ix.procNames = append(ix.procNames, name)
	}
	sort.Strings(ix.procNames)

	return ix, nil
}

// parseFile scans one Odin source file's content and returns its package
// name (empty if none declared) and every recognised declaration.
func parseFile(content, rel string) (pkgName string, syms []symbol) {
	lines := strings.Split(content, "\n")
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		leftTrimmed := strings.TrimLeft(line, " \t")
		indent := len(line) - len(leftTrimmed)
		trimmed := strings.TrimRight(leftTrimmed, " \t")
		for {
			m := reAttrPrefix.FindStringSubmatch(trimmed)
			if m == nil {
				break
			}
			trimmed = strings.TrimSpace(trimmed[len(m[0]):])
		}
		if trimmed == "" {
			continue
		}
		if pkgName == "" {
			if m := rePackageLine.FindStringSubmatch(trimmed); m != nil {
				pkgName = m[1]
				continue
			}
		}
		if m := reDoubleColon.FindStringSubmatch(trimmed); m != nil {
			name := m[1]
			rest := strings.TrimSpace(m[2])
			isProc := reProcDecl.MatchString(rest)
			sym := symbol{
				name: name,
				file: rel,
				line: i + 1,
				proc: isProc,
			}
			if isProc || reTypeDecl.MatchString(rest) {
				sym.span = declSpan(lines, i, isProc)
			}
			syms = append(syms, sym)
			continue
		}
		if indent == 0 {
			if m := reSingleColon.FindStringSubmatch(trimmed); m != nil {
				syms = append(syms, symbol{name: m[1], file: rel, line: i + 1})
			}
		}
	}
	return pkgName, syms
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

// lookup resolves a qualified reference ("pkg.name" or "name") to every
// matching declaration.
func (ix *Index) lookup(qualified string) []symbol {
	q := stripParens(qualified)
	if q == "" {
		return nil
	}
	if i := strings.LastIndex(q, "."); i >= 0 {
		pkg, name := q[:i], q[i+1:]
		if s, ok := ix.byQual[pkg+"."+name]; ok {
			return []symbol{s}
		}
		return nil
	}
	return ix.byName[q]
}

// Empty reports whether no .odin files were found under root.
func (ix *Index) Empty() bool { return ix.stats.Files == 0 }

// Packages returns every recognised package name, sorted.
func (ix *Index) Packages() []string {
	out := make([]string, len(ix.packages))
	copy(out, ix.packages)
	return out
}

// ProcNames returns every recognised bare procedure name, sorted and
// deduplicated. It exists mainly to make Odin indexing behaviour easy to
// assert in tests.
func (ix *Index) ProcNames() []string {
	out := make([]string, len(ix.procNames))
	copy(out, ix.procNames)
	return out
}

// Has reports whether qualified ("pkg.name" or "name") exists. A trailing
// "(...)" or "()" call suffix is stripped before lookup. A bare "name"
// matches in any package.
func (ix *Index) Has(qualified string) bool {
	return len(ix.lookup(qualified)) > 0
}

// File returns the declaring file (relative, forward slashes) and 1-based
// line of qualified, if known.
func (ix *Index) File(qualified string) (string, int, bool) {
	m := ix.lookup(qualified)
	if len(m) == 0 {
		return "", 0, false
	}
	return m[0].file, m[0].line, true
}

// Stats returns index-wide counters.
func (ix *Index) Stats() Stats { return ix.stats }

// Counts returns the counters in the shape the composite index reports.
func (ix *Index) Counts() lang.Stats {
	return lang.Stats{Files: ix.stats.Files, Namespaces: ix.stats.Packages, Symbols: ix.stats.Symbols}
}

// Namespaces is Packages under the lang.Index name.
func (ix *Index) Namespaces() []string { return ix.Packages() }

// IsNamespace reports whether qualified is a package name of the tree.
func (ix *Index) IsNamespace(qualified string) bool {
	q := stripParens(qualified)
	i := sort.SearchStrings(ix.packages, q)
	return i < len(ix.packages) && ix.packages[i] == q
}

// IsExample is always false: Odin trees keep no examples/ convention docrot
// recognises.
func (ix *Index) IsExample(string) bool { return false }

// Symbols lists every declaration as "pkg.name", sorted.
func (ix *Index) Symbols() []string {
	out := make([]string, 0, len(ix.all))
	for _, s := range ix.all {
		out = append(out, s.pkg+"."+s.name)
	}
	sort.Strings(out)
	return out
}

// Routes returns nil: no Odin HTTP framework is recognised.
func (ix *Index) Routes() []routes.Route { return nil }

// Defaults returns an empty set: Odin option defaults are not indexed.
func (ix *Index) Defaults() *defaults.Set { return defaults.New() }

// Literals returns the identifier-like string literals of the tree.
func (ix *Index) Literals() *literals.Set { return ix.lits }

// Similar returns up to n existing "pkg.name" candidates whose bare name is
// close (Damerau-Levenshtein distance <= max(2, len/4), case-insensitive)
// to the last dot-separated part of qualified. Best (smallest distance)
// first; ties broken alphabetically.
func (ix *Index) Similar(qualified string, n int) []string {
	if n <= 0 {
		return nil
	}
	q := stripParens(qualified)
	last := q
	if i := strings.LastIndex(q, "."); i >= 0 {
		last = q[i+1:]
	}
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
	for _, s := range ix.all {
		dist := damerauLevenshtein(lastLower, strings.ToLower(s.name))
		if dist <= threshold {
			cands = append(cands, cand{dist: dist, formatted: s.pkg + "." + s.name})
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
