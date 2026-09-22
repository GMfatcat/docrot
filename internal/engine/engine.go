// Package engine wires every docrot stage together: discover documents,
// build the index, extract and resolve references, run the git-based
// staleness and bilingual-pair analyses, compute coverage, apply the
// baseline and assemble a report.
package engine

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"docrot/internal/baseline"
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
	BaselinePath  string           // "" → <root>/.docrot-baseline.json
	Verbose       bool
	Stderr        io.Writer
	Version       string
	Now           time.Time
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
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", opts.Root)
	}
	run := &Run{Refs: map[string][]model.Reference{}}
	warn := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
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
		parsed = parseDocs(root, docs, warn)
	}()
	go func() {
		defer wg.Done()
		ix, ixWarns, ixErr = index.Build(root, index.Options{
			Exclude:         cfg.Exclude,
			ConfigSamples:   cfg.ConfigSamples,
			IncludeInternal: cfg.Coverage.IncludeInternal,
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
		repo, err := gitx.Open(root, gitx.Options{})
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
	})

	type docResult struct {
		doc      string
		refs     []model.Reference
		findings []model.Finding
		resolved []stale.ResolvedRef
	}
	results := make([]docResult, len(docs))
	mentioned := map[string]bool{}
	var mu sync.Mutex
	parallel(len(docs), func(i int) {
		d := docs[i]
		p := parsed[d]
		if p == nil {
			return
		}
		refs := extract.Extract(p, ix, extract.Options{Ignore: ignoreRes})
		r := docResult{doc: d, refs: refs}
		for _, ref := range refs {
			res := rs.Resolve(ref)
			switch {
			case res.Finding != nil:
				r.findings = append(r.findings, *res.Finding)
			case res.OK:
				if res.File != "" {
					r.resolved = append(r.resolved, stale.ResolvedRef{Ref: ref, File: res.File})
				}
				mu.Lock()
				mentioned[string(ref.Kind)+"|"+ref.Norm] = true
				mu.Unlock()
			}
		}
		results[i] = r
	})
	var findings []model.Finding
	totalRefs := 0
	for _, r := range results {
		run.Refs[r.doc] = r.refs
		totalRefs += len(r.refs)
		findings = append(findings, r.findings...)
	}

	// 5. stale sections
	if run.Git != nil && cfg.Stale.Enabled {
		staleOpts := stale.Options{MinChurn: cfg.Stale.MinChurn, MinDays: cfg.Stale.MinDays, Now: opts.Now}
		if s, ok := sevOverrides[model.RuleStaleSection]; ok {
			staleOpts.Severity = s
		}
		staleOut := make([][]model.Finding, len(docs))
		parallel(len(docs), func(i int) {
			r := results[i]
			if len(r.resolved) == 0 || parsed[r.doc] == nil {
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

	// 6. bilingual pairs
	prs := pairs.Detect(docs, toPairs(cfg.Pairs), cfg.PairPatterns)
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

	// 8. baseline
	bpath := opts.BaselinePath
	if bpath == "" {
		bpath = filepath.Join(root, baseline.DefaultName)
	}
	var baselined, fixedN int
	if b, err := baseline.Load(bpath); err == nil {
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
		Docs:       len(docs),
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
	if len(prs) > 0 {
		sum.Extra["pairs"] = strconv.Itoa(len(prs))
	}
	run.Report = &report.Report{Summary: sum, Findings: findings, Coverage: cov, Version: opts.Version}
	return run, nil
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
	ix, _, err := index.Build(root, index.Options{Exclude: cfg.Exclude, ConfigSamples: cfg.ConfigSamples})
	if err != nil {
		return nil, err
	}
	// anchors for every doc so cross-document anchors resolve
	docs, err := discoverDocs(root, cfg.Docs, cfg.Exclude)
	if err != nil {
		return nil, err
	}
	for d, p := range parseDocs(root, docs, func(string, ...any) {}) {
		ix.AddDoc(d, p)
	}
	p := markdown.Parse(rel, data)
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
	if err != nil || strings.HasPrefix(rel, "..") {
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
		if globx.MatchAny(include, rel) {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func parseDocs(root string, docs []string, warn func(string, ...any)) map[string]*markdown.Doc {
	out := make([]*markdown.Doc, len(docs))
	parallel(len(docs), func(i int) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(docs[i])))
		if err != nil {
			warn("read %s: %v", docs[i], err)
			return
		}
		out[i] = markdown.Parse(docs[i], data)
	})
	m := make(map[string]*markdown.Doc, len(docs))
	for i, d := range docs {
		if out[i] != nil {
			m[d] = out[i]
		}
	}
	return m
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
