// Package engine wires every docrot stage together: discover documents,
// build the index, extract and resolve references, run the git-based
// staleness and bilingual-pair analyses, compute coverage, apply the
// baseline and assemble a report.
package engine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"docrot/internal/asciidoc"
	"docrot/internal/baseline"
	"docrot/internal/comments"
	"docrot/internal/config"
	"docrot/internal/coverage"
	"docrot/internal/extract"
	"docrot/internal/gitx"
	"docrot/internal/globx"
	"docrot/internal/index"
	"docrot/internal/markdown"
	"docrot/internal/model"
	"docrot/internal/pairs"
	"docrot/internal/report"
	"docrot/internal/resolve"
	"docrot/internal/rst"
	"docrot/internal/stale"
)

// Options configures a run.
type Options struct {
	Root          string
	Config        config.Config
	NoGit         bool
	Net           bool
	ShowAll       bool             // include baselined findings in output
	MinConfidence model.Confidence // 0 → from config
	Coverage      bool             // compute the coverage section
	AllComments   bool             // run the comment checks on every exported symbol, not only documented ones
	BaselinePath  string           // "" → <root>/.docrot-baseline.json
	NoBaseline    bool             // ignore any baseline file (used by `docrot baseline`)
	Verbose       bool
	Stderr        io.Writer
	Version       string
	Now           time.Time
	// OutDir is a directory, relative to Root, rewritten with the report in
	// every format at the end of a run. Empty means "write nothing". It is
	// always excluded from document discovery and indexing, even when
	// NoOut is set, so a report left by an earlier run is never read back
	// as a document.
	OutDir string
	// NoOut suppresses writing OutDir without forgetting about it.
	NoOut bool
	// Changed restricts the checks to documents that differ from HEAD in
	// the work tree or index, untracked documents, and (with ChangedBase,
	// e.g. "origin/main") documents changed on this branch since the merge
	// base. Every document is still parsed so cross-document anchors
	// resolve; only the changed ones are extracted, resolved and analysed.
	// Coverage is skipped, since it needs every document. Requires git.
	Changed     bool
	ChangedBase string
}

// Run is the outcome of Check.
type Run struct {
	Report   *report.Report
	Index    *index.Index
	Docs     []string
	Refs     map[string][]model.Reference
	Warnings []string
	Git      *gitx.Repo // nil when unavailable/disabled
	Baseline *baseline.File
	Fixed    []baseline.Entry
	// Written lists the report files written into OutDir, named the way
	// OutDir was (so relative to Root unless the caller gave an absolute
	// directory) and always with forward slashes. Empty when nothing was
	// written, because OutDir was off or every write failed.
	Written []string
	// CommentSpans counts the declarations whose comments were checked.
	CommentSpans int
	// AllDocs is every discovered document when Changed restricted Docs to
	// a subset; nil otherwise.
	AllDocs []string
}

// Check runs the full pipeline.
func Check(opts Options) (*Run, error) {
	start := time.Now()
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	cfg := opts.Config
	cfg.Exclude = excludeOutDir(cfg.Exclude, opts.OutDir)
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", opts.Root)
	}
	run := &Run{Refs: map[string][]model.Reference{}}
	// warn is called from the parallel doc-parsing and staleness workers, so
	// it must serialise both the append and the write to Stderr.
	var warnMu sync.Mutex
	warn := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		warnMu.Lock()
		defer warnMu.Unlock()
		run.Warnings = append(run.Warnings, msg)
		fmt.Fprintln(opts.Stderr, "docrot: warning: "+msg)
	}

	// 1. discover docs and 2. build index, concurrently
	var (
		wg      sync.WaitGroup
		docs    []string
		parsed  map[string]*markdown.Doc
		docErr  error
		ix      *index.Index
		ixWarns []error
		ixErr   error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		docs, docErr = discoverDocs(root, cfg.Docs, cfg.Exclude)
		if docErr != nil {
			return
		}
		parsed = parseDocs(root, docs, cfg.MaxFileBytes(), warn)
	}()
	go func() {
		defer wg.Done()
		ix, ixWarns, ixErr = index.Build(root, index.Options{
			Exclude:         cfg.Exclude,
			ConfigSamples:   cfg.ConfigSamples,
			IncludeInternal: cfg.Coverage.IncludeInternal,
			MaxFileSize:     cfg.MaxFileBytes(),
		})
	}()
	wg.Wait()
	if docErr != nil {
		return nil, docErr
	}
	if ixErr != nil {
		return nil, ixErr
	}
	for _, e := range ixWarns {
		if opts.Verbose {
			warn("index: %v", e)
		}
	}
	for _, rel := range ix.Stats().SkippedLarge {
		warn("skipping %s: larger than maxFileMB, not parsed (its path still resolves)", rel)
	}
	run.Index = ix
	run.Docs = docs
	for _, d := range docs {
		if p := parsed[d]; p != nil {
			ix.AddDoc(d, p)
		}
	}

	// 3. git
	gitState := report.GitDisabled
	if !opts.NoGit {
		gopts := gitx.Options{}
		if opts.OutDir != "" && !opts.NoOut {
			// blame and log answers survive between runs inside the output
			// directory (its .gitignore covers them)
			dir := opts.OutDir
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(root, filepath.FromSlash(dir))
			}
			gopts.CacheFile = filepath.Join(dir, "git-cache.json")
		}
		repo, err := gitx.Open(root, gopts)
		if err != nil {
			gitState = report.GitUnavailable
			if opts.Verbose {
				warn("git: %v", err)
			}
		} else {
			run.Git = repo
			gitState = report.GitOK
		}
	}
	var renames map[string]string
	if run.Git != nil {
		if m, err := run.Git.Renames(); err == nil {
			renames = m
		} else if opts.Verbose {
			warn("git renames: %v", err)
		}
	}

	// 3b. --changed: keep only the documents git says differ
	checked := docs
	if opts.Changed {
		if run.Git == nil {
			return nil, errors.New("--changed needs git: the directory is not in a work tree, or --no-git was given")
		}
		changed, err := run.Git.Changed(opts.ChangedBase)
		if err != nil {
			return nil, err
		}
		set := make(map[string]bool, len(changed))
		for _, p := range changed {
			set[p] = true
		}
		checked = checked[:0:0]
		for _, d := range docs {
			if set[d] {
				checked = append(checked, d)
			}
		}
		run.AllDocs = docs
		run.Docs = checked
		if opts.Coverage || cfg.Coverage.Report {
			warn("coverage is skipped with --changed: it needs every document")
			opts.Coverage, cfg.Coverage.Report = false, false
		}
	}

	// 4. extract + resolve
	ignoreRes, err := cfg.IgnoreRegexps()
	if err != nil {
		return nil, err
	}
	minConf := opts.MinConfidence
	if minConf == 0 {
		if c, ok := model.ParseConfidence(cfg.MinConfidence); ok {
			minConf = c
		} else {
			minConf = model.Low
		}
	}
	sevOverrides := severityMap(cfg.Severity)
	rs := resolve.New(ix, resolve.Options{
		Root:          root,
		MinConfidence: minConf,
		Net:           opts.Net || cfg.Net,
		Severity:      sevOverrides,
		Renames:       renames,
		Siblings:      absSiblings(root, cfg.Siblings),
	})

	type docResult struct {
		doc      string
		refs     []model.Reference
		findings []model.Finding
		resolved []stale.ResolvedRef
	}
	results := make([]docResult, len(checked))
	mentioned := map[string]bool{}
	symbolRefs := map[string]model.Reference{} // kind|norm → one reference that resolved
	var mu sync.Mutex
	parallel(len(checked), func(i int) {
		d := checked[i]
		p := parsed[d]
		if p == nil {
			return
		}
		historical := globx.MatchAny(cfg.Stale.Exclude, d) // changelogs: no toolchain claims
		refs := extract.Extract(p, ix, extract.Options{Ignore: ignoreRes, NoToolchain: historical})
		r := docResult{doc: d, refs: refs}
		for _, ref := range refs {
			res := rs.Resolve(ref)
			switch {
			case res.Finding != nil:
				r.findings = append(r.findings, *res.Finding)
			case res.OK:
				// only file-level references feed the staleness analysis; a
				// directory reference ("servicex/") churns by definition
				if res.File != "" && ix.FileExists(res.File) {
					r.resolved = append(r.resolved, stale.ResolvedRef{Ref: ref, File: res.File})
				}
				mu.Lock()
				mentioned[string(ref.Kind)+"|"+ref.Norm] = true
				if ref.Kind == model.KindGoSymbol || ref.Kind == model.KindPySym || ref.Kind == model.KindOdinSym {
					symbolRefs[string(ref.Kind)+"|"+ref.Norm] = ref
				}
				mu.Unlock()
			}
		}
		results[i] = r
	})
	var findings []model.Finding
	totalRefs := 0
	for _, r := range results {
		if r.doc == "" {
			continue // the document failed to read; nothing was extracted
		}
		run.Refs[r.doc] = r.refs
		totalRefs += len(r.refs)
		findings = append(findings, r.findings...)
	}
	if run.Git != nil {
		findings = dropIgnoredPaths(run.Git, findings, warn, opts.Verbose)
	}

	// 5. stale sections
	if run.Git != nil && cfg.Stale.Enabled {
		staleOpts := stale.Options{MinChurn: cfg.Stale.MinChurn, MinDays: cfg.Stale.MinDays, Now: opts.Now}
		if s, ok := sevOverrides[model.RuleStaleSection]; ok {
			staleOpts.Severity = s
		}
		staleOut := make([][]model.Finding, len(checked))
		parallel(len(checked), func(i int) {
			r := results[i]
			if len(r.resolved) == 0 || parsed[r.doc] == nil || globx.MatchAny(cfg.Stale.Exclude, r.doc) {
				return
			}
			fs, err := stale.Analyze(run.Git, parsed[r.doc], r.doc, r.resolved, staleOpts)
			if err != nil {
				if opts.Verbose {
					warn("stale %s: %v", r.doc, err)
				}
				return
			}
			staleOut[i] = fs
		})
		for _, fs := range staleOut {
			findings = append(findings, fs...)
		}
	}

	// 5b. comments attached to the symbols documents referred to
	if cfg.Comments.Enabled {
		var spans []model.SymbolSpan
		if opts.AllComments {
			spans = ix.AllSpans(cfg.Coverage.IncludeInternal)
		} else {
			for _, ref := range symbolRefs {
				if sp, ok := ix.SymbolSpan(ref.Kind, ref.Norm); ok {
					spans = append(spans, sp)
				}
			}
		}
		if len(spans) > 0 {
			copts := comments.Options{MinChurn: cfg.Comments.MinChurn, MinFrac: cfg.Comments.MinFrac}
			if s, ok := sevOverrides[model.RuleStaleComment]; ok {
				copts.StaleSeverity = s
			}
			if s, ok := sevOverrides[model.RuleCommentMentions]; ok {
				copts.MentionSeverity = s
			}
			read := sourceReader(root, cfg.MaxFileBytes())
			findings = append(findings, comments.Analyze(run.Git, spans, ix, read, copts)...)
			sum := len(spans)
			run.CommentSpans = sum
		}
	}

	// 6. bilingual pairs (with --changed: only pairs with a changed side)
	prs := pairs.Detect(docs, toPairs(cfg.Pairs), cfg.PairPatterns)
	if opts.Changed {
		set := make(map[string]bool, len(checked))
		for _, d := range checked {
			set[d] = true
		}
		kept := prs[:0]
		for _, p := range prs {
			if set[p.Source] || set[p.Translation] {
				kept = append(kept, p)
			}
		}
		prs = kept
	}
	pairOpts := pairs.Options{Severity: sevOverrides, Repo: run.Git}
	for _, p := range prs {
		src, tr := parsed[p.Source], parsed[p.Translation]
		if src == nil || tr == nil {
			continue
		}
		findings = append(findings, pairs.Compare(p, src, tr, pairOpts)...)
	}

	// 7. coverage
	var cov *report.Coverage
	if opts.Coverage || cfg.Coverage.Report {
		exported := ix.GoExported()
		kept := exported[:0]
		for _, e := range exported {
			if e.Kind == model.KindEnv && resolve.IsExternalEnv(e.Qualified) {
				continue // NO_COLOR, TERM, GOPATH…: not this project's API
			}
			kept = append(kept, e)
		}
		exported = kept
		res := coverage.Compute(exported, mentioned)
		cov = toReportCoverage(res)
		if cfg.Coverage.Report {
			sev := model.SevInfo
			if s, ok := sevOverrides[model.RuleUndocumented]; ok {
				sev = s
			}
			findings = append(findings, coverage.Findings(res, exported, sev)...)
		}
	}

	// 7b. rule-scoped ignores (<!-- docrot:ignore missing-path -->)
	findings = dropRuleIgnored(findings, parsed)

	// 8. baseline
	bpath := opts.BaselinePath
	if bpath == "" {
		bpath = filepath.Join(root, baseline.DefaultName)
	}
	var baselined, fixedN int
	if opts.NoBaseline {
		// nothing to apply
	} else if b, err := baseline.Load(bpath); err == nil {
		run.Baseline = b
		var fixed []baseline.Entry
		baselined, _, fixed = baseline.Apply(b, findings)
		run.Fixed = fixed
		fixedN = len(fixed)
	} else if !errors.Is(err, fs.ErrNotExist) {
		warn("baseline: %v", err)
	}

	// 9. assemble
	report.Sort(findings)
	sum := report.Summary{
		Root:       root,
		Docs:       len(checked),
		References: totalRefs,
		Baselined:  baselined,
		Fixed:      fixedN,
		Duration:   time.Since(start),
		Git:        gitState,
		Extra:      map[string]string{},
	}
	for _, f := range findings {
		if f.Baselined {
			continue
		}
		switch f.Severity {
		case model.SevError:
			sum.Errors++
		case model.SevWarning:
			sum.Warnings++
		default:
			sum.Infos++
		}
	}
	st := ix.Stats()
	if st.GoPackages > 0 {
		sum.Extra["go packages"] = strconv.Itoa(st.GoPackages)
		sum.Extra["go symbols"] = strconv.Itoa(st.GoSymbols)
	}
	if st.OdinFiles > 0 {
		sum.Extra["odin symbols"] = strconv.Itoa(st.OdinSymbols)
	}
	if st.PyFiles > 0 {
		sum.Extra["python symbols"] = strconv.Itoa(st.PySymbols)
	}
	if st.Routes > 0 {
		sum.Extra["routes"] = strconv.Itoa(st.Routes)
	}
	if run.Git != nil {
		if err := run.Git.SaveCache(); err != nil {
			warn("git cache: %v", err)
		}
		if h := run.Git.CacheHits(); h > 0 {
			sum.Extra["git cache hits"] = strconv.Itoa(h)
		}
	}
	if opts.Changed {
		sum.Extra["changed"] = strconv.Itoa(len(checked)) + " of " + strconv.Itoa(len(docs)) + " docs"
	}
	if len(prs) > 0 {
		sum.Extra["pairs"] = strconv.Itoa(len(prs))
	}
	run.Report = &report.Report{Summary: sum, Findings: findings, Coverage: cov, Version: opts.Version}

	// 10. output directory
	if opts.OutDir != "" && !opts.NoOut {
		run.Written = writeOutDir(root, opts.OutDir, run.Report, opts.ShowAll, warn)
	}
	return run, nil
}

// OutFiles are the files every `docrot check` run rewrites inside the
// output directory, in the order they are written.
var OutFiles = []string{"report.html", "report.md", "report.json", "report.txt"}

// outGitignore is what the output directory's .gitignore says: ignore
// everything in here, including itself. It is written once and never
// overwritten, so a project that wants to commit its reports only has to
// empty the file.
const outGitignore = "*\n"

// writeOutDir rewrites <root>/<outDir> with the report in every format and
// returns the files it wrote, relative to root. Nothing here is fatal: a
// read-only checkout should still get its findings on stdout, so every
// failure becomes a warning and the run continues.
func writeOutDir(root, outDir string, r *report.Report, showBaselined bool, warn func(string, ...any)) []string {
	dir := outDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, filepath.FromSlash(outDir))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		warn("out dir: %v", err)
		return nil
	}
	gitignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitignore); errors.Is(err, fs.ErrNotExist) {
		if err := writeFileAtomic(gitignore, func(w io.Writer) error {
			_, err := io.WriteString(w, outGitignore)
			return err
		}); err != nil {
			warn("out dir: %v", err)
		}
	}
	// The text and Markdown reports are the ones an agent reads end to end,
	// so they carry the info findings the terminal hides by default.
	base := report.Options{Root: root, ShowBaselined: showBaselined}
	full := base
	full.ShowInfo = true
	formats := map[string]report.Options{
		"report.html": base,
		"report.md":   full,
		"report.json": base,
		"report.txt":  full,
	}
	names := map[string]string{"report.html": "html", "report.md": "md", "report.json": "json", "report.txt": "text"}
	var written []string
	for _, name := range OutFiles {
		o := formats[name]
		err := writeFileAtomic(filepath.Join(dir, name), func(w io.Writer) error {
			return report.Write(names[name], w, r, o)
		})
		if err != nil {
			warn("out dir: %s: %v", name, err)
			continue
		}
		written = append(written, path.Join(filepath.ToSlash(outDir), name))
	}
	return written
}

// writeFileAtomic renders into a temporary file beside path and renames it
// over path, so an interrupted or failing run never leaves a half-written
// report where the next reader expects a whole one.
func writeFileAtomic(dest string, render func(io.Writer) error) (err error) {
	f, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	bw := bufio.NewWriter(f)
	if err = render(bw); err != nil {
		return err
	}
	if err = bw.Flush(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// excludeOutDir appends "<outDir>/**" to the exclude globs unless it is
// already there. Without it the reports of the previous run would be
// discovered as documents and indexed as files.
func excludeOutDir(exclude []string, outDir string) []string {
	if outDir == "" {
		return exclude
	}
	pat := path.Clean(filepath.ToSlash(outDir)) + "/**"
	for _, e := range exclude {
		if e == pat {
			return exclude
		}
	}
	return append(slices.Clone(exclude), pat)
}

// ExplainRow is one extracted reference with its resolution, for `docrot explain`.
type ExplainRow struct {
	Ref     model.Reference
	Status  string // "ok" | "missing" | "skipped"
	Message string
	File    string
}

// Explain runs extraction and resolution for one document and returns every
// reference with what docrot decided about it.
func Explain(opts Options, doc string) ([]ExplainRow, error) {
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	rel, err := relDoc(root, doc)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	cfg := opts.Config
	ix, _, err := index.Build(root, index.Options{Exclude: cfg.Exclude, ConfigSamples: cfg.ConfigSamples, MaxFileSize: cfg.MaxFileBytes()})
	if err != nil {
		return nil, err
	}
	// anchors for every doc so cross-document anchors resolve
	docs, err := discoverDocs(root, cfg.Docs, cfg.Exclude)
	if err != nil {
		return nil, err
	}
	for d, p := range parseDocs(root, docs, cfg.MaxFileBytes(), func(string, ...any) {}) {
		ix.AddDoc(d, p)
	}
	p := parseDoc(rel, data)
	ix.AddDoc(rel, p)
	ignoreRes, err := cfg.IgnoreRegexps()
	if err != nil {
		return nil, err
	}
	refs := extract.Extract(p, ix, extract.Options{Ignore: ignoreRes})
	rs := resolve.New(ix, resolve.Options{Root: root, MinConfidence: model.Low, Net: opts.Net || cfg.Net, Severity: severityMap(cfg.Severity)})
	rows := make([]ExplainRow, 0, len(refs))
	for _, ref := range refs {
		res := rs.Resolve(ref)
		row := ExplainRow{Ref: ref, File: res.File}
		switch {
		case res.Finding != nil:
			row.Status = "missing"
			row.Message = res.Finding.Message
			if res.Finding.Suggestion != "" {
				row.Message += " (did you mean " + res.Finding.Suggestion + "?)"
			}
		case res.OK:
			row.Status = "ok"
		default:
			row.Status = "skipped"
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// --- helpers ---------------------------------------------------------------

// dropIgnoredPaths removes missing-path/command findings whose path matches
// a .gitignore rule: "dist/app.exe" not existing is a build artifact, not
// documentation rot.
func dropIgnoredPaths(repo *gitx.Repo, findings []model.Finding, warn func(string, ...any), verbose bool) []model.Finding {
	var paths []string
	cand := func(f model.Finding) []string {
		if f.Ref == nil || strings.ContainsAny(f.Ref.Norm, "*?") {
			return nil
		}
		norm := path.Clean(f.Ref.Norm)
		out := []string{norm}
		if d := path.Dir(f.Loc.File); d != "." {
			out = append(out, path.Clean(d+"/"+norm))
		}
		return out
	}
	for _, f := range findings {
		if f.Rule == model.RuleMissingPath || f.Rule == model.RuleMissingCommand {
			paths = append(paths, cand(f)...)
		}
	}
	if len(paths) == 0 {
		return findings
	}
	ignored, err := repo.Ignored(paths)
	if err != nil {
		if verbose {
			warn("git check-ignore: %v (gitignored paths will be reported)", err)
		}
		return findings
	}
	if len(ignored) == 0 {
		return findings
	}
	kept := findings[:0]
	for _, f := range findings {
		drop := false
		if f.Rule == model.RuleMissingPath || f.Rule == model.RuleMissingCommand {
			for _, c := range cand(f) {
				if ignored[c] {
					drop = true
				}
			}
		}
		if !drop {
			kept = append(kept, f)
		}
	}
	return kept
}

// dropRuleIgnored removes findings that a rule-scoped ignore directive in
// their document covers. Line 0 (whole-file findings) is never covered.
func dropRuleIgnored(findings []model.Finding, parsed map[string]*markdown.Doc) []model.Finding {
	kept := findings[:0]
	for _, f := range findings {
		if p := parsed[f.Loc.File]; p != nil && p.RuleIgnored != nil && f.Loc.Line > 0 && p.RuleIgnored(f.Loc.Line, f.Rule) {
			continue
		}
		kept = append(kept, f)
	}
	return kept
}

func relDoc(root, doc string) (string, error) {
	abs := doc
	if !filepath.IsAbs(abs) {
		if _, err := os.Stat(doc); err == nil {
			abs, _ = filepath.Abs(doc)
		} else {
			abs = filepath.Join(root, doc)
		}
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s is outside %s", doc, root)
	}
	return filepath.ToSlash(rel), nil
}

// discoverDocs walks root and returns relative doc paths matching the
// include globs and not the exclude globs. Sorted.
func discoverDocs(root string, include, exclude []string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == ".git" || globx.MatchAny(exclude, rel) || globx.MatchAny(exclude, rel+"/") {
				return filepath.SkipDir
			}
			return nil
		}
		if globx.MatchAny(exclude, rel) {
			return nil
		}
		// Include globs match case-insensitively: "**/*.md" must find
		// README.MD and Readme.markdown-style variants on every platform.
		if globx.MatchAny(include, rel) || globx.MatchAny(include, strings.ToLower(rel)) {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// parseDocs reads and tokenizes the documents. A document larger than
// maxSize bytes (0 = unlimited) is skipped with a warning: a multi-megabyte
// "Markdown" file is a data dump, not prose, and would only cost time.
func parseDocs(root string, docs []string, maxSize int64, warn func(string, ...any)) map[string]*markdown.Doc {
	out := make([]*markdown.Doc, len(docs))
	parallel(len(docs), func(i int) {
		p := filepath.Join(root, filepath.FromSlash(docs[i]))
		if maxSize > 0 {
			if st, err := os.Stat(p); err == nil && st.Size() > maxSize {
				warn("skipping %s: %d MiB exceeds maxFileMB", docs[i], st.Size()>>20)
				return
			}
		}
		data, err := os.ReadFile(p)
		if err != nil {
			warn("read %s: %v", docs[i], err)
			return
		}
		out[i] = parseDoc(docs[i], data)
	})
	m := make(map[string]*markdown.Doc, len(docs))
	for i, d := range docs {
		if out[i] != nil {
			m[d] = out[i]
		}
	}
	return m
}

// parseDoc tokenizes one document with the parser its format calls for:
// reStructuredText (.rst, .rest, and .txt files that look like it — Django
// keeps Sphinx sources as .txt), AsciiDoc (.adoc, .asciidoc, .asc), and
// Markdown for everything else (llms.txt included).
func parseDoc(rel string, data []byte) *markdown.Doc {
	switch strings.ToLower(path.Ext(rel)) {
	case ".rst", ".rest":
		return rst.Parse(rel, data)
	case ".adoc", ".asciidoc", ".asc":
		return asciidoc.Parse(rel, data)
	case ".txt":
		if rst.Looks(data) {
			return rst.Parse(rel, data)
		}
	}
	return markdown.Parse(rel, data)
}

// parallel runs fn(i) for i in [0,n) on a bounded worker pool.
func parallel(n int, fn func(i int)) {
	workers := runtime.NumCPU()
	if workers > n {
		workers = n
	}
	if workers <= 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	var wg sync.WaitGroup
	ch := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		ch <- i
	}
	close(ch)
	wg.Wait()
}

// sourceReader returns a cached, size-capped line reader for source files.
func sourceReader(root string, maxSize int64) comments.ReadLines {
	var mu sync.Mutex
	cache := map[string][]string{}
	return func(rel string) []string {
		mu.Lock()
		if l, ok := cache[rel]; ok {
			mu.Unlock()
			return l
		}
		mu.Unlock()
		p := filepath.Join(root, filepath.FromSlash(rel))
		var lines []string
		if st, err := os.Stat(p); err == nil && (maxSize <= 0 || st.Size() <= maxSize) {
			if b, err := os.ReadFile(p); err == nil {
				lines = strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
			}
		}
		mu.Lock()
		cache[rel] = lines
		mu.Unlock()
		return lines
	}
}

// absSiblings resolves sibling repo roots relative to root, keeping only
// directories that exist.
func absSiblings(root string, sibs []string) []string {
	var out []string
	for _, s := range sibs {
		p := s
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

func severityMap(m map[string]string) map[string]model.Severity {
	out := map[string]model.Severity{}
	for k, v := range m {
		if s, ok := model.ParseSeverity(v); ok && s != "" {
			out[k] = s
		}
	}
	return out
}

func toPairs(ps []config.Pair) []pairs.Pair {
	out := make([]pairs.Pair, 0, len(ps))
	for _, p := range ps {
		out = append(out, pairs.Pair{Source: filepath.ToSlash(p.Source), Translation: filepath.ToSlash(p.Translation)})
	}
	return out
}

func toReportCoverage(r coverage.Result) *report.Coverage {
	c := &report.Coverage{}
	for _, g := range r.Packages {
		c.Packages = append(c.Packages, report.PackageCoverage{Package: g.Name, Total: g.Total, Documented: g.Documented, Missing: g.Missing})
	}
	c.Flags = report.CoverageGroup{Total: r.Flags.Total, Documented: r.Flags.Documented, Missing: r.Flags.Missing}
	c.Envs = report.CoverageGroup{Total: r.Envs.Total, Documented: r.Envs.Documented, Missing: r.Envs.Missing}
	return c
}
