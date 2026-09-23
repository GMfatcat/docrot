// Package index composes the per-language and per-artifact indexes into a
// single model.Index. Building runs the leaf indexes concurrently.
package index

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"docrot/internal/fuzzy"
	"docrot/internal/globx"
	"docrot/internal/index/anchors"
	"docrot/internal/index/config"
	"docrot/internal/index/files"
	"docrot/internal/index/gosym"
	"docrot/internal/index/odin"
	"docrot/internal/index/project"
	"docrot/internal/index/py"
	"docrot/internal/index/routes"
	"docrot/internal/markdown"
	"docrot/internal/model"
)

// Options controls what gets indexed.
type Options struct {
	Exclude         []string // glob patterns relative to root
	ConfigSamples   []string // glob patterns for JSON sample files
	IncludeInternal bool     // include internal/ packages in GoExported
	IncludeTests    bool     // index symbols from _test.go files as first-class
	// MaxFileSize caps the size of any file whose contents are read (Go,
	// Odin, Python, JSON samples). Larger files stay in the path index but
	// are not parsed. 0 means no cap.
	MaxFileSize int64
}

// Stats summarises the built index for the report.
type Stats struct {
	Files       int
	Routes      int
	Literals    int
	GoFiles     int
	GoPackages  int
	GoSymbols   int
	Flags       int
	Envs        int
	JSONKeys    int
	OdinFiles   int
	OdinSymbols int
	PyFiles     int
	PySymbols   int
	ConfigFiles int
	ConfigKeys  int
	ParseErrors int
	// SkippedLarge lists files (relative) not parsed because they exceed
	// Options.MaxFileSize.
	SkippedLarge []string
	Duration     time.Duration
}

// Index implements model.Index.
type Index struct {
	root    string
	opts    Options
	files   *files.Index
	gos     *gosym.Index
	od      *odin.Index
	pys     *py.Index
	cfg     *config.Index
	anch    *anchors.Index
	rts     *routes.Set
	proj    model.Project
	flagSet map[string]bool
	pkgSet  map[string]bool
	stats   Stats
}

var _ model.Index = (*Index)(nil)

// Build indexes root. Per-file parse errors are returned as warnings and
// never abort the build.
func Build(root string, opts Options) (*Index, []error, error) {
	start := time.Now()
	ix := &Index{root: root, opts: opts, anch: anchors.New(), rts: routes.New()}
	excl, large := expandExcludes(root, opts.Exclude, opts.MaxFileSize)
	ix.stats.SkippedLarge = large
	// content indexers never open oversized files; the path index still lists them
	excl = append(excl, large...)

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		warns []error
		fatal error
	)
	addWarn := func(errs ...error) {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range errs {
			if e != nil {
				warns = append(warns, e)
			}
		}
	}
	setFatal := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil && fatal == nil {
			fatal = err
		}
	}

	wg.Add(6)
	go func() {
		defer wg.Done()
		f, err := files.Build(root, opts.Exclude)
		setFatal(err)
		ix.files = f
	}()
	go func() {
		defer wg.Done()
		ix.proj = project.Build(root)
	}()
	go func() {
		defer wg.Done()
		g, errs := gosym.Build(root, gosym.Options{Exclude: excl, IncludeTests: opts.IncludeTests})
		addWarn(errs...)
		ix.gos = g
	}()
	go func() {
		defer wg.Done()
		o, err := odin.Build(root, excl)
		addWarn(err)
		ix.od = o
	}()
	go func() {
		defer wg.Done()
		p, err := py.Build(root, excl)
		addWarn(err)
		ix.pys = p
	}()
	go func() {
		defer wg.Done()
		c, err := config.Build(root, opts.ConfigSamples, excl)
		addWarn(err)
		ix.cfg = c
	}()
	wg.Wait()
	if fatal != nil {
		return nil, warns, fatal
	}

	ix.flagSet = map[string]bool{}
	ix.pkgSet = map[string]bool{}
	if ix.gos != nil {
		for _, r := range ix.gos.Routes() {
			ix.rts.Add(r)
		}
	}
	if ix.pys != nil {
		for _, r := range ix.pys.Routes() {
			ix.rts.Add(r)
		}
	}
	ix.stats.Routes = ix.rts.Len()
	ix.stats.Literals = ix.literalCount()
	if ix.gos != nil {
		for _, f := range ix.gos.Flags() {
			ix.flagSet[normFlag(f)] = true
		}
		for _, p := range ix.gos.Packages() {
			ix.pkgSet[p] = true
		}
		gs := ix.gos.Stats()
		ix.stats.GoFiles, ix.stats.GoPackages, ix.stats.GoSymbols = gs.Files, gs.Packages, gs.Symbols
		ix.stats.Flags, ix.stats.Envs, ix.stats.JSONKeys = gs.Flags, gs.Envs, gs.JSONKeys
		ix.stats.ParseErrors = gs.ParseErrors
	}
	if ix.od != nil {
		s := ix.od.Stats()
		ix.stats.OdinFiles, ix.stats.OdinSymbols = s.Files, s.Symbols
	}
	if ix.pys != nil {
		s := ix.pys.Stats()
		ix.stats.PyFiles, ix.stats.PySymbols = s.Files, s.Symbols
	}
	if ix.cfg != nil {
		s := ix.cfg.Stats()
		ix.stats.ConfigFiles, ix.stats.ConfigKeys = s.Files, s.Keys
	}
	ix.stats.Files = ix.files.Len()
	ix.stats.Duration = time.Since(start)
	return ix, warns, nil
}

// parsedExt are the extensions whose contents an indexer would read; only
// these are subject to the size cap (a 4 GB model file is never opened).
var parsedExt = map[string]bool{".go": true, ".odin": true, ".py": true, ".json": true, ".jsonc": true}

// expandExcludes walks root and returns the relative paths (files and
// directories) matched by the glob patterns, so leaf indexes that only
// understand concrete paths can prune them, plus the parsed-type files that
// exceed maxSize (0 = unlimited).
func expandExcludes(root string, patterns []string, maxSize int64) (excluded, large []string) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if len(patterns) > 0 && globx.MatchAny(patterns, rel) {
			excluded = append(excluded, rel)
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if maxSize > 0 && !d.IsDir() && parsedExt[strings.ToLower(filepath.Ext(rel))] {
			if info, ierr := d.Info(); ierr == nil && info.Size() > maxSize {
				large = append(large, rel)
			}
		}
		return nil
	})
	return excluded, large
}

func normFlag(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimLeft(name, "-"), "_", "-"))
}

// Stats returns build statistics.
func (ix *Index) Stats() Stats { return ix.stats }

// Root returns the indexed root.
func (ix *Index) Root() string { return ix.root }

// AddDoc registers a parsed Markdown document for anchor lookups. Not safe
// for concurrent use with itself; call before resolving.
func (ix *Index) AddDoc(rel string, d *markdown.Doc) { ix.anch.Add(rel, d) }

// --- files ---

func (ix *Index) FileExists(rel string) bool              { return ix.files.FileExists(rel) }
func (ix *Index) DirExists(rel string) bool               { return ix.files.DirExists(rel) }
func (ix *Index) Glob(pattern string) []string            { return ix.files.Glob(pattern) }
func (ix *Index) SimilarPaths(rel string, n int) []string { return ix.files.SimilarPaths(rel, n) }
func (ix *Index) TopLevelDirs() []string                  { return ix.files.TopLevelDirs() }

// --- Go ---

func (ix *Index) ModulePath() string {
	if ix.gos == nil {
		return ""
	}
	return ix.gos.ModulePath()
}

func (ix *Index) GoPackages() []string {
	if ix.gos == nil {
		return nil
	}
	return ix.gos.Packages()
}

func (ix *Index) IsGoPackage(name string) bool { return ix.pkgSet[name] }

func (ix *Index) GoPackageDir(importPath string) (string, bool) {
	if ix.gos == nil {
		return "", false
	}
	return ix.gos.PackageDir(importPath)
}

func (ix *Index) HasGoSymbol(q string) bool {
	if ix.gos == nil {
		return false
	}
	return ix.gos.HasSymbol(q)
}

func (ix *Index) GoSymbolFile(q string) (string, bool) {
	if ix.gos == nil {
		return "", false
	}
	f, _, ok := ix.gos.SymbolFile(q)
	return f, ok
}

func (ix *Index) SimilarGoSymbols(q string, n int) []string {
	if ix.gos == nil {
		return nil
	}
	return ix.gos.Similar(q, n)
}

func (ix *Index) IsGoType(name string) bool {
	if ix.gos == nil {
		return false
	}
	return ix.gos.IsType(name)
}

func (ix *Index) HasGoMember(name string) bool {
	if ix.gos == nil {
		return false
	}
	return ix.gos.HasMember(name)
}

func (ix *Index) HasFlag(name string) bool { return ix.flagSet[normFlag(name)] }

func (ix *Index) Flags() []string {
	if ix.gos == nil {
		return nil
	}
	return ix.gos.Flags()
}

func (ix *Index) HasEnv(name string) bool {
	if ix.gos == nil {
		return false
	}
	return ix.gos.HasEnv(name)
}

func (ix *Index) Envs() []string {
	if ix.gos == nil {
		return nil
	}
	return ix.gos.Envs()
}

func (ix *Index) HasJSONKey(d string) bool {
	if ix.gos == nil {
		return false
	}
	return ix.gos.HasJSONKey(d)
}

func (ix *Index) JSONKeys() []string {
	if ix.gos == nil {
		return nil
	}
	return ix.gos.JSONKeys()
}

func (ix *Index) GoExported() []model.Exported {
	if ix.gos == nil {
		return nil
	}
	out := ix.gos.Exported(ix.opts.IncludeInternal)
	for _, f := range ix.gos.Flags() {
		out = append(out, model.Exported{Qualified: "--" + f, Kind: model.KindFlag})
	}
	for _, e := range ix.gos.Envs() {
		out = append(out, model.Exported{Qualified: e, Kind: model.KindEnv})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Qualified < out[j].Qualified
	})
	return out
}

// --- Odin / Python ---

func (ix *Index) HasOdin() bool { return ix.od != nil && !ix.od.Empty() }

func (ix *Index) OdinPackages() []string {
	if ix.od == nil {
		return nil
	}
	return ix.od.Packages()
}

func (ix *Index) HasOdinSymbol(q string) bool { return ix.od != nil && ix.od.Has(q) }

func (ix *Index) SimilarOdinSymbols(q string, n int) []string {
	if ix.od == nil {
		return nil
	}
	return ix.od.Similar(q, n)
}

func (ix *Index) HasPython() bool { return ix.pys != nil && !ix.pys.Empty() }

func (ix *Index) PyModules() []string {
	if ix.pys == nil {
		return nil
	}
	return ix.pys.Modules()
}

func (ix *Index) HasPySymbol(q string) bool { return ix.pys != nil && ix.pys.Has(q) }

func (ix *Index) PyModuleIsExample(m string) bool { return ix.pys != nil && ix.pys.IsExampleModule(m) }

func (ix *Index) PyIsModule(q string) bool { return ix.pys != nil && ix.pys.IsModule(q) }

func (ix *Index) SimilarPySymbols(q string, n int) []string {
	if ix.pys == nil {
		return nil
	}
	return ix.pys.Similar(q, n)
}

// --- declaration spans ---

// SymbolSpan returns the declaration span of a qualified symbol in the
// language selected by kind (model.KindGoSymbol, model.KindPySym or
// model.KindOdinSym). Other kinds never resolve.
func (ix *Index) SymbolSpan(kind model.Kind, qualified string) (model.SymbolSpan, bool) {
	switch kind {
	case model.KindGoSymbol:
		if ix.gos != nil {
			return ix.gos.Span(qualified)
		}
	case model.KindPySym:
		if ix.pys != nil {
			return ix.pys.Span(qualified)
		}
	case model.KindOdinSym:
		if ix.od != nil {
			return ix.od.Span(qualified)
		}
	}
	return model.SymbolSpan{}, false
}

// AllSpans returns the documented declaration surface of every language:
// exported Go funcs, methods and types (internal/ packages only when
// includeInternal), exported Python defs and classes outside example trees,
// and every Odin proc and type. Each language's spans are sorted by
// qualified name.
func (ix *Index) AllSpans(includeInternal bool) []model.SymbolSpan {
	var out []model.SymbolSpan
	if ix.gos != nil {
		out = append(out, ix.gos.AllSpans(includeInternal)...)
	}
	if ix.pys != nil {
		out = append(out, ix.pys.AllSpans()...)
	}
	if ix.od != nil {
		out = append(out, ix.od.AllSpans()...)
	}
	return out
}

// --- anchors ---

func (ix *Index) HasAnchor(docRel, slug string) bool { return ix.anch.Has(docRel, slug) }
func (ix *Index) Anchors(docRel string) []string     { return ix.anch.Anchors(docRel) }

// --- config samples ---

func (ix *Index) HasConfigKey(d string) bool { return ix.cfg != nil && ix.cfg.Has(d) }

func (ix *Index) ConfigKeys() []string {
	if ix.cfg == nil {
		return nil
	}
	return ix.cfg.Keys()
}

// --- project identity ---

func (ix *Index) Project() model.Project { return ix.proj }

// --- string literals ---

func (ix *Index) HasLiteral(s string) bool {
	if ix.gos != nil && ix.gos.Literals().Has(s) {
		return true
	}
	if ix.pys != nil && ix.pys.Literals().Has(s) {
		return true
	}
	return ix.od != nil && ix.od.Literals().Has(s)
}

func (ix *Index) literalCount() int {
	n := 0
	if ix.gos != nil {
		n += ix.gos.Literals().Len()
	}
	if ix.pys != nil {
		n += ix.pys.Literals().Len()
	}
	if ix.od != nil {
		n += ix.od.Literals().Len()
	}
	return n
}

// --- HTTP routes ---

func (ix *Index) HasRoutes() bool { return !ix.rts.Empty() }

func (ix *Index) MatchRoute(method, p string) model.RouteMatch {
	m := ix.rts.Lookup(method, p)
	return model.RouteMatch{OK: m.OK, Methods: m.Methods, File: m.File, Line: m.Line, Mounted: m.Mounted}
}

func (ix *Index) Routes() []string { return ix.rts.List() }

// SimilarRoutes ranks registered paths by edit distance to the normalised
// form of p, parameters included ("/items/{}" for "/items/{id}").
func (ix *Index) SimilarRoutes(p string, n int) []string {
	q := routes.Display(routes.Normalize(p))
	var out []string
	for _, c := range fuzzy.Rank(q, ix.rts.Paths(), n, max(2, len(q)/5)) {
		out = append(out, c.Text)
	}
	return out
}

// Symbols returns a sorted listing used by `docrot index`.
func (ix *Index) Symbols(kind string) []string {
	switch kind {
	case "symbols":
		var out []string
		for _, e := range ix.GoExported() {
			if e.Kind == model.KindGoSymbol {
				out = append(out, e.Qualified)
			}
		}
		return out
	case "flags":
		return ix.Flags()
	case "env":
		return ix.Envs()
	case "paths":
		return ix.files.Files()
	case "anchors":
		var out []string
		for _, d := range ix.anch.Docs() {
			for _, a := range ix.anch.Anchors(d) {
				out = append(out, d+"#"+a)
			}
		}
		return out
	case "config":
		out := append([]string{}, ix.JSONKeys()...)
		out = append(out, ix.ConfigKeys()...)
		sort.Strings(out)
		return out
	case "odin":
		if ix.od == nil {
			return nil
		}
		return ix.od.ProcNames()
	case "python":
		return ix.PyModules()
	case "routes":
		return ix.Routes()
	case "targets":
		var out []string
		for tool, list := range ix.proj.Targets {
			for _, t := range list {
				out = append(out, tool+" "+t)
			}
		}
		sort.Strings(out)
		return out
	}
	return nil
}
