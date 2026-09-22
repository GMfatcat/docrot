// Package gosym builds a read-only index of the Go source under a repository
// root, so that documentation references such as "httpx.WriteData",
// "--config", "DOCROT_DEBUG" or "server.addr" can be checked against what the
// code actually declares.
//
// The index is built once with Build and is safe for concurrent use
// afterwards. Only the Go standard library is used; files are parsed
// concurrently and a file that fails to parse is reported as an error but
// never aborts the build.
package gosym

import (
	"fmt"
	"go/ast"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"docrot/internal/model"
)

// Options configures Build.
type Options struct {
	// Exclude lists relative directory or file paths (forward slashes) that
	// must not be indexed. Directory entries match as prefixes, file entries
	// match exactly. Globs are expected to be expanded by the caller.
	//
	// "vendor", "testdata", ".git" and any directory whose name starts with
	// "_" or "." are always skipped, whether listed here or not.
	Exclude []string

	// IncludeTests folds symbols declared in _test.go files into the main
	// symbol set (and indexes their flags, environment variables and struct
	// tags as well). When false, test symbols are kept in a separate set that
	// HasSymbol only consults when a name is not found anywhere else.
	IncludeTests bool
}

// Stats summarises the contents of an Index.
type Stats struct {
	Files       int // Go files parsed successfully (test files included)
	Packages    int // distinct package names
	Symbols     int // distinct qualified symbol names, excluding test-only ones
	Flags       int // distinct command line flag names
	Envs        int // distinct environment variable names
	JSONKeys    int // distinct dotted struct tag paths
	ParseErrors int // files that could not be parsed
}

// symbol is where a name is declared.
type symbol struct {
	file string
	line int
}

// Index answers questions about the Go code under a root directory.
// All methods are safe for concurrent use.
type Index struct {
	root       string
	modulePath string

	pkgNames  []string            // sorted, unique
	pkgDirs   map[string][]string // package name -> sorted dirs
	importDir map[string]string   // full import path -> dir

	syms     map[string]symbol // every lookup form from non-test files
	testSyms map[string]symbol // the same forms, from _test.go files
	types    map[string]bool   // bare names of declared types

	entries []entry // counted declarations, in file order (fuzzy candidates)

	flags    map[string]bool
	envs     map[string]bool
	jsonKeys map[string]bool

	flagList []string
	envList  []string
	jsonList []string

	exported []exportedItem // sorted by Qualified

	stats Stats
}

// exportedItem is one exported declaration plus whether it lives under an
// internal/ directory, so Exported can filter cheaply.
type exportedItem struct {
	item     model.Exported
	internal bool
}

// Build indexes every Go file under root. The returned errors are per-file
// parse failures (and directory walk failures); they are informational and the
// returned Index is always usable.
func Build(root string, opts Options) (*Index, []error) {
	ix := &Index{
		root:      root,
		pkgDirs:   map[string][]string{},
		importDir: map[string]string{},
		syms:      map[string]symbol{},
		testSyms:  map[string]symbol{},
		types:     map[string]bool{},
		flags:     map[string]bool{},
		envs:      map[string]bool{},
		jsonKeys:  map[string]bool{},
	}
	ix.modulePath = readModulePath(root)

	files, errs := collectFiles(root, opts)
	results := parseAll(root, files)

	dirPkg := map[string]string{}
	for _, r := range results { // non-test files decide a directory's package
		if r.err == nil && !r.test {
			if _, ok := dirPkg[r.dir]; !ok {
				dirPkg[r.dir] = r.pkg
			}
		}
	}
	for _, r := range results {
		if r.err == nil && r.test {
			if _, ok := dirPkg[r.dir]; !ok {
				dirPkg[r.dir] = r.pkg
			}
		}
	}

	structsByDir := map[string]map[string]*ast.StructType{}
	quals := map[string]bool{}

	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			ix.stats.ParseErrors++
			continue
		}
		ix.stats.Files++
		counted := !r.test || opts.IncludeTests

		for _, name := range r.types {
			ix.types[name] = true
		}
		target := ix.syms
		if !counted {
			target = ix.testSyms
		}
		for _, e := range r.entries {
			ix.addEntry(target, e)
			if counted {
				ix.entries = append(ix.entries, e)
				quals[e.qual()] = true
			}
		}
		if !counted {
			continue
		}
		for _, f := range r.flags {
			ix.flags[f] = true
		}
		for _, e := range r.envs {
			ix.envs[e] = true
		}
		if len(r.structs) > 0 {
			m := structsByDir[r.dir]
			if m == nil {
				m = map[string]*ast.StructType{}
				structsByDir[r.dir] = m
			}
			for name, st := range r.structs {
				if _, ok := m[name]; !ok {
					m[name] = st
				}
			}
		}
	}

	for _, structs := range structsByDir {
		collectJSONKeys(structs, ix.jsonKeys)
	}

	ix.buildPackages(dirPkg)
	ix.buildExported()

	ix.flagList = sortedKeys(ix.flags)
	ix.envList = sortedKeys(ix.envs)
	ix.jsonList = sortedKeys(ix.jsonKeys)

	ix.stats.Packages = len(ix.pkgNames)
	ix.stats.Symbols = len(quals)
	ix.stats.Flags = len(ix.flagList)
	ix.stats.Envs = len(ix.envList)
	ix.stats.JSONKeys = len(ix.jsonList)

	return ix, errs
}

// addEntry records every lookup form of e into m. The first declaration seen
// for a form wins, which is deterministic because files are visited in sorted
// order.
func (ix *Index) addEntry(m map[string]symbol, e entry) {
	s := symbol{file: e.file, line: e.line}
	put := func(k string) {
		if k == "" {
			return
		}
		if _, ok := m[k]; !ok {
			m[k] = s
		}
	}
	put(e.qual())
	if e.owner != "" {
		put(e.owner + "." + e.name) // Type.Member, package omitted
		return
	}
	put(e.name) // bare set: top level identifiers and types only
}

// buildPackages fills the package name and import path tables.
func (ix *Index) buildPackages(dirPkg map[string]string) {
	for dir, name := range dirPkg {
		if name == "" {
			continue
		}
		ix.pkgDirs[name] = append(ix.pkgDirs[name], dir)
		ix.importDir[ix.importPathOf(dir)] = dir
	}
	for name, dirs := range ix.pkgDirs {
		sort.Strings(dirs)
		ix.pkgDirs[name] = dirs
		ix.pkgNames = append(ix.pkgNames, name)
	}
	sort.Strings(ix.pkgNames)
}

// importPathOf returns the import path a directory would have in this module.
// Without a go.mod the directory itself is used as the key.
func (ix *Index) importPathOf(dir string) string {
	if ix.modulePath == "" {
		return dir
	}
	if dir == "." {
		return ix.modulePath
	}
	return ix.modulePath + "/" + dir
}

// buildExported collects the exported API surface, sorted by qualified name.
func (ix *Index) buildExported() {
	seen := map[string]bool{}
	for _, e := range ix.entries {
		if e.test || e.pkg == "main" || e.kind == kindField {
			continue
		}
		if !ast.IsExported(e.name) {
			continue
		}
		if e.owner != "" && !ast.IsExported(e.owner) {
			continue
		}
		q := e.qual()
		if seen[q] {
			continue
		}
		seen[q] = true
		ix.exported = append(ix.exported, exportedItem{
			item: model.Exported{
				Package:   e.pkg,
				Qualified: q,
				Kind:      model.KindGoSymbol,
				File:      e.file,
				Line:      e.line,
			},
			internal: isInternalPath(e.file),
		})
	}
	sort.Slice(ix.exported, func(i, j int) bool {
		return ix.exported[i].item.Qualified < ix.exported[j].item.Qualified
	})
}

// ModulePath returns the module path from go.mod, or "" when there is none.
func (ix *Index) ModulePath() string { return ix.modulePath }

// Packages returns every package name found, sorted and unique. The name
// "main" is included; documents rarely reference it.
func (ix *Index) Packages() []string { return append([]string(nil), ix.pkgNames...) }

// IsPackage reports whether name is a known package name.
func (ix *Index) IsPackage(name string) bool {
	_, ok := ix.pkgDirs[name]
	return ok
}

// PackageDirs returns the directories declaring a package with this name.
// A name may legitimately exist in several directories.
func (ix *Index) PackageDirs(name string) []string {
	return append([]string(nil), ix.pkgDirs[name]...)
}

// PackageDir maps a full import path (the module path, or the module path plus
// "/" plus a directory) to that directory relative to the root. The root
// package is ".". When there is no go.mod, directories are their own keys.
func (ix *Index) PackageDir(importPath string) (string, bool) {
	dir, ok := ix.importDir[strings.TrimSuffix(strings.TrimSpace(importPath), "/")]
	return dir, ok
}

// HasSymbol reports whether a qualified name exists. Accepted forms are
// "pkg.Name", "pkg.Type.Member", "Type.Member" and a bare "Name"; a leading
// "*"/"&", a trailing call "(...)" and generic instantiations "[T]" are
// ignored, and an import path prefix is reduced to its last element.
// Symbols declared only in _test.go files are consulted last.
func (ix *Index) HasSymbol(qualified string) bool {
	_, _, ok := ix.SymbolFile(qualified)
	return ok
}

// SymbolFile returns the file (relative to the root, forward slashes) and
// 1-based line where a qualified symbol is declared.
func (ix *Index) SymbolFile(qualified string) (string, int, bool) {
	key := normalizeSymbol(qualified)
	if key == "" || strings.Count(key, ".") > 2 {
		return "", 0, false
	}
	if s, ok := ix.syms[key]; ok {
		return s.file, s.line, true
	}
	if s, ok := ix.testSyms[key]; ok {
		return s.file, s.line, true
	}
	return "", 0, false
}

// IsType reports whether name is declared as a type in any package.
func (ix *Index) IsType(name string) bool { return ix.types[normalizeSymbol(name)] }

// HasFlag reports whether a command line flag with this name is defined.
// Leading dashes are ignored.
func (ix *Index) HasFlag(name string) bool {
	return ix.flags[strings.TrimLeft(strings.TrimSpace(name), "-")]
}

// Flags returns every flag name found, sorted, without leading dashes.
func (ix *Index) Flags() []string { return append([]string(nil), ix.flagList...) }

// HasEnv reports whether an environment variable with this name is read or
// written by the code.
func (ix *Index) HasEnv(name string) bool { return ix.envs[strings.TrimSpace(name)] }

// Envs returns every environment variable name found, sorted.
func (ix *Index) Envs() []string { return append([]string(nil), ix.envList...) }

// HasJSONKey reports whether a dotted configuration path appears in a struct
// tag (json, yaml or toml) or as an untagged struct field name.
func (ix *Index) HasJSONKey(dotted string) bool { return ix.jsonKeys[strings.TrimSpace(dotted)] }

// JSONKeys returns every dotted key path found, sorted.
func (ix *Index) JSONKeys() []string { return append([]string(nil), ix.jsonList...) }

// Exported lists the exported API surface for coverage: funcs, types, consts,
// vars and methods of exported types, excluding package main, _test.go files
// and (unless includeInternal) anything under an internal/ directory. Struct
// fields are not included. The result is sorted by qualified name.
func (ix *Index) Exported(includeInternal bool) []model.Exported {
	out := make([]model.Exported, 0, len(ix.exported))
	for _, e := range ix.exported {
		if e.internal && !includeInternal {
			continue
		}
		out = append(out, e.item)
	}
	return out
}

// Stats returns a summary of the index contents.
func (ix *Index) Stats() Stats { return ix.stats }

// collectFiles walks root and returns the relative paths of every Go file that
// should be parsed, sorted for determinism.
func collectFiles(root string, opts Options) ([]string, []error) {
	excl := normalizeExcludes(opts.Exclude)
	var out []string
	var errs []error

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel := relSlash(root, p)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if isSkippedDirName(d.Name()) || excluded(excl, rel) {
				return fs.SkipDir
			}
			return nil
		}
		base := d.Name()
		if !strings.HasSuffix(base, ".go") {
			return nil
		}
		if strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".") {
			return nil // ignored by the go tool as well
		}
		if excluded(excl, rel) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	sort.Strings(out)
	return out, errs
}

// isSkippedDirName reports whether a directory is always skipped.
func isSkippedDirName(name string) bool {
	switch name {
	case "vendor", "testdata", ".git", "node_modules":
		return true
	}
	return strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".")
}

// normalizeExcludes cleans user supplied exclude entries.
func normalizeExcludes(in []string) []string {
	out := make([]string, 0, len(in))
	for _, e := range in {
		e = strings.TrimSpace(filepath.ToSlash(e))
		e = strings.TrimPrefix(e, "./")
		e = strings.Trim(e, "/")
		if e != "" && e != "." {
			out = append(out, e)
		}
	}
	return out
}

// excluded reports whether rel is, or is inside, an excluded entry.
func excluded(excl []string, rel string) bool {
	for _, e := range excl {
		if rel == e || strings.HasPrefix(rel, e+"/") {
			return true
		}
	}
	return false
}

// relSlash returns p relative to root using forward slashes.
func relSlash(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

// isInternalPath reports whether a relative file path lies under an internal
// directory.
func isInternalPath(rel string) bool {
	parts := strings.Split(rel, "/")
	for _, part := range parts[:max(len(parts)-1, 0)] {
		if part == "internal" {
			return true
		}
	}
	return false
}

// path4Dir returns the directory part of a relative slash path, "." for a
// file directly under the root.
func path4Dir(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return "."
}

// readModulePath extracts the module path from root/go.mod, "" when absent.
func readModulePath(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		rest, ok := strings.CutPrefix(line, "module")
		if !ok || (rest != "" && !isSpace(rest[0])) {
			continue
		}
		rest = strings.TrimSpace(rest)
		rest = strings.Trim(rest, "\"")
		if rest != "" && rest != "(" {
			return rest
		}
	}
	return ""
}

// isSpace reports whether c is an ASCII blank.
func isSpace(c byte) bool { return c == ' ' || c == '\t' }

// parseAll parses every file with a worker pool of runtime.NumCPU() workers
// and returns the results in the same order as files.
func parseAll(root string, files []string) []*fileResult {
	results := make([]*fileResult, len(files))
	if len(files) == 0 {
		return results
	}
	workers := runtime.NumCPU()
	if workers > len(files) {
		workers = len(files)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = parseFile(root, files[i])
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

// sortedKeys returns the sorted keys of a set.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// normalizeSymbol reduces a documentation spelling of a symbol to the plain
// lookup key: a leading "*"/"&", a trailing call, generic instantiations and
// an import path prefix are all removed.
func normalizeSymbol(q string) string {
	q = strings.TrimSpace(q)
	for q != "" && (q[0] == '*' || q[0] == '&') {
		q = strings.TrimSpace(q[1:])
	}
	if strings.HasSuffix(q, ")") {
		if i := strings.LastIndex(q, "("); i >= 0 {
			q = strings.TrimSpace(q[:i])
		}
	}
	q = stripBrackets(q)
	if i := strings.LastIndex(q, "/"); i >= 0 {
		q = q[i+1:]
	}
	return strings.TrimSpace(strings.Trim(q, "."))
}

// stripBrackets removes every bracketed run, so "Stack[K, V].Push" becomes
// "Stack.Push".
func stripBrackets(s string) string {
	if !strings.ContainsRune(s, '[') {
		return s
	}
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// wrapParseError decorates a parse failure with the relative file name.
func wrapParseError(rel string, err error) error {
	return fmt.Errorf("gosym: parse %s: %w", rel, err)
}
