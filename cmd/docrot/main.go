// Command docrot finds where documentation lies about the code.
//
//	docrot check [dir] [flags]     scan docs, report findings, exit 1 on failures,
//	                               and rewrite the output directory (.docrot by default)
//	docrot baseline [dir]          freeze current findings into .docrot-baseline.json
//	docrot coverage [dir]          which exported symbols/flags/env are undocumented
//	docrot pairs [dir]             only the bilingual source/translation checks
//	docrot explain <doc>           every reference extracted from one document
//	docrot index [dir] --kind K    dump an index (symbols|flags|env|paths|anchors|config|odin|python)
//	docrot init [dir]              write a default .docrot.json
//	docrot version
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"text/tabwriter"

	"docrot/internal/baseline"
	"docrot/internal/config"
	"docrot/internal/engine"
	"docrot/internal/model"
	"docrot/internal/report"
)

// version is set with -ldflags "-X main.version=v1.2.3"; otherwise the
// release number below is reported together with the VCS revision.
var version = ""

const release = "0.2.0"

const (
	exitOK       = 0
	exitFindings = 1
	exitUsage    = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "docrot: internal error: %v\n", r)
			if os.Getenv("DOCROT_DEBUG") != "" {
				stderr.Write(debug.Stack())
			}
			code = exitUsage
		}
	}()
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "check":
		return cmdCheck(rest, stdout, stderr)
	case "baseline":
		return cmdBaseline(rest, stdout, stderr)
	case "coverage":
		return cmdCoverage(rest, stdout, stderr)
	case "pairs":
		return cmdPairs(rest, stdout, stderr)
	case "explain":
		return cmdExplain(rest, stdout, stderr)
	case "index":
		return cmdIndex(rest, stdout, stderr)
	case "init":
		return cmdInit(rest, stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "docrot "+versionString())
		return exitOK
	case "help", "--help", "-h":
		usage(stdout)
		return exitOK
	}
	fmt.Fprintf(stderr, "docrot: unknown command %q\n\n", cmd)
	usage(stderr)
	return exitUsage
}

func usage(w io.Writer) {
	fmt.Fprint(w, `docrot — find where documentation lies about the code

Usage:
  docrot check [dir] [flags]      scan docs and report findings (exit 1 when --fail-on is met)
  docrot baseline [dir]           write .docrot-baseline.json with the current findings
  docrot coverage [dir]           list exported symbols / flags / env vars no document mentions
  docrot pairs [dir]              only the source/translation pair checks
  docrot explain <doc>            show every reference extracted from one document
  docrot index [dir] --kind K     dump an index: symbols|flags|env|paths|anchors|config|odin|python
  docrot init [dir]               write a default .docrot.json
  docrot version

Run "docrot <command> -h" for the flags of a command.
`)
}

// common holds flags shared by the scanning commands.
type common struct {
	helpShown  bool // -h/--help was requested: exit 0, not 2
	fs         *flag.FlagSet
	configPath string
	noGit      bool
	net        bool
	verbose    bool
	minConf    string
}

func newCommon(name string) *common {
	c := &common{fs: flag.NewFlagSet("docrot "+name, flag.ContinueOnError)}
	c.fs.StringVar(&c.configPath, "config", "", "path to .docrot.json (default: <dir>/.docrot.json if present)")
	c.fs.BoolVar(&c.noGit, "no-git", false, "disable git-based analyses (stale sections, pair lag, rename suggestions)")
	c.fs.BoolVar(&c.net, "net", false, "check external URLs over the network")
	c.fs.BoolVar(&c.verbose, "verbose", false, "print index and git warnings")
	c.fs.StringVar(&c.minConf, "min-confidence", "", "ignore references below this confidence: low|medium|high")
	return c
}

// parse parses args, returning the root directory and the loaded config.
func (c *common) parse(args []string, stderr io.Writer) (root string, cfg config.Config, ok bool) {
	c.fs.SetOutput(stderr)
	pos, err := parseInterspersed(c.fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			c.helpShown = true
		}
		return "", cfg, false
	}
	root = "."
	if len(pos) > 0 {
		root = pos[0]
	}
	if len(pos) > 1 {
		fmt.Fprintf(stderr, "docrot: unexpected arguments: %s\n", strings.Join(pos[1:], " "))
		return "", cfg, false
	}
	if c.configPath != "" {
		cfg, err = config.Load(c.configPath)
	} else {
		cfg, _, err = config.LoadOrDefault(root)
	}
	if err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return "", cfg, false
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(stderr, "docrot: config: %v\n", err)
		return "", cfg, false
	}
	if c.minConf != "" {
		if _, ok := model.ParseConfidence(c.minConf); !ok {
			fmt.Fprintf(stderr, "docrot: --min-confidence must be low, medium or high\n")
			return "", cfg, false
		}
		cfg.MinConfidence = c.minConf
	}
	return root, cfg, true
}

// exitCode after a failed parse: 0 for -h, 2 otherwise.
func (c *common) exitCode() int {
	if c.helpShown {
		return exitOK
	}
	return exitUsage
}

func (c *common) engineOptions(root string, cfg config.Config, stderr io.Writer) engine.Options {
	mc, _ := model.ParseConfidence(cfg.MinConfidence)
	return engine.Options{
		Root:          root,
		Config:        cfg,
		NoGit:         c.noGit,
		Net:           c.net,
		MinConfidence: mc,
		Verbose:       c.verbose,
		Stderr:        stderr,
		Version:       versionString(),
		// Every command keeps the output directory out of discovery and
		// indexing, but only `check` rewrites it.
		OutDir: cfg.OutDir,
		NoOut:  true,
	}
}

func cmdCheck(args []string, stdout, stderr io.Writer) int {
	c := newCommon("check")
	var format, output, failOn, outDir string
	var all, quiet, cov, info, noOut bool
	c.fs.StringVar(&format, "format", "text", "output format: "+strings.Join(report.Formats, "|"))
	c.fs.BoolVar(&info, "info", false, "also list info-level findings in the text report")
	c.fs.StringVar(&outDir, "out-dir", "", "directory rewritten with the report in every format (default from config, "+config.DefaultOutDir+")")
	c.fs.BoolVar(&noOut, "no-out", false, "do not write the output directory")
	c.fs.StringVar(&output, "output", "", "write the report to this file instead of stdout")
	c.fs.StringVar(&failOn, "fail-on", "", "exit 1 when a new finding of this severity or higher exists: error|warning|info|none (default from config, error)")
	c.fs.BoolVar(&all, "all", false, "also show baselined findings (implies --info)")
	c.fs.BoolVar(&quiet, "quiet", false, "print only the summary line (text format)")
	c.fs.BoolVar(&cov, "coverage", false, "append the documentation coverage section")
	root, cfg, ok := c.parse(args, stderr)
	if !ok {
		return c.exitCode()
	}
	if !contains(report.Formats, format) {
		fmt.Fprintf(stderr, "docrot: --format must be one of %s\n", strings.Join(report.Formats, ", "))
		return exitUsage
	}
	if failOn == "" {
		failOn = cfg.FailOn
	}
	failSev, ok := model.ParseSeverity(failOn)
	if !ok {
		fmt.Fprintf(stderr, "docrot: --fail-on must be error, warning, info or none\n")
		return exitUsage
	}
	var w io.Writer = stdout
	var outFile *os.File
	if output != "" {
		f, err := os.Create(output) // fail fast, before the scan
		if err != nil {
			fmt.Fprintf(stderr, "docrot: %v\n", err)
			return exitUsage
		}
		outFile, w = f, f
	}
	opts := c.engineOptions(root, cfg, stderr)
	opts.ShowAll = all
	opts.Coverage = cov
	if outDir != "" {
		opts.OutDir = outDir
	}
	opts.NoOut = noOut
	run, err := engine.Check(opts)
	if err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return exitUsage
	}
	ropts := report.Options{ShowBaselined: all, ShowInfo: info || all, Color: output == "" && report.ColorEnabled(stdout), Root: root}
	if quiet && format == "text" {
		fmt.Fprintln(w, report.SummaryLine(run.Report.Summary))
	} else if err := report.Write(format, w, run.Report, ropts); err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return exitUsage
	}
	if outFile != nil {
		if err := outFile.Close(); err != nil {
			fmt.Fprintf(stderr, "docrot: write %s: %v\n", output, err)
			return exitUsage
		}
		fmt.Fprintf(stderr, "%s\nreport written to %s\n", report.SummaryLine(run.Report.Summary), output)
	}
	if len(run.Written) > 0 {
		fmt.Fprintf(stderr, "reports written to %s/ (report.md for agents, report.html for humans)\n",
			strings.TrimSuffix(filepath.ToSlash(opts.OutDir), "/"))
	}
	if failSev == "" {
		return exitOK
	}
	for _, f := range run.Report.Findings {
		if !f.Baselined && f.Severity.Rank() >= failSev.Rank() {
			return exitFindings
		}
	}
	return exitOK
}

func cmdBaseline(args []string, stdout, stderr io.Writer) int {
	c := newCommon("baseline")
	var out string
	c.fs.StringVar(&out, "output", "", "baseline file (default <dir>/"+baseline.DefaultName+")")
	root, cfg, ok := c.parse(args, stderr)
	if !ok {
		return c.exitCode()
	}
	opts := c.engineOptions(root, cfg, stderr)
	opts.NoBaseline = true // a baseline is being rebuilt, not applied
	run, err := engine.Check(opts)
	if err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return exitUsage
	}
	if out == "" {
		out = filepath.Join(root, baseline.DefaultName)
	}
	if err := baseline.Save(out, run.Report.Findings); err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return exitUsage
	}
	fmt.Fprintf(stdout, "baseline written to %s (%d findings)\n", out, len(run.Report.Findings))
	return exitOK
}

func cmdCoverage(args []string, stdout, stderr io.Writer) int {
	c := newCommon("coverage")
	var format string
	c.fs.StringVar(&format, "format", "text", "output format: text|json")
	root, cfg, ok := c.parse(args, stderr)
	if !ok {
		return c.exitCode()
	}
	if format != "text" && format != "json" {
		fmt.Fprintf(stderr, "docrot: --format must be text or json\n")
		return exitUsage
	}
	cfg.Stale.Enabled = false
	opts := c.engineOptions(root, cfg, stderr)
	opts.NoGit = true
	opts.Coverage = true
	run, err := engine.Check(opts)
	if err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return exitUsage
	}
	cov := run.Report.Coverage
	if format == "json" {
		r := &report.Report{Summary: run.Report.Summary, Coverage: cov, Version: versionString()}
		if err := report.WriteJSON(stdout, r, report.Options{}); err != nil {
			fmt.Fprintf(stderr, "docrot: %v\n", err)
			return exitUsage
		}
		return exitOK
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PACKAGE\tDOCUMENTED\tTOTAL\tPCT\tUNDOCUMENTED")
	total, documented := 0, 0
	for _, p := range cov.Packages {
		total += p.Total
		documented += p.Documented
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d%%\t%s\n", p.Package, p.Documented, p.Total, pct(p.Documented, p.Total), joinMax(p.Missing, 8))
	}
	fmt.Fprintf(tw, "flags\t%d\t%d\t%d%%\t%s\n", cov.Flags.Documented, cov.Flags.Total, pct(cov.Flags.Documented, cov.Flags.Total), joinMax(cov.Flags.Missing, 8))
	fmt.Fprintf(tw, "env\t%d\t%d\t%d%%\t%s\n", cov.Envs.Documented, cov.Envs.Total, pct(cov.Envs.Documented, cov.Envs.Total), joinMax(cov.Envs.Missing, 8))
	tw.Flush()
	fmt.Fprintf(stdout, "\nsymbols: %d/%d documented (%d%%) across %d docs\n", documented, total, pct(documented, total), run.Report.Summary.Docs)
	return exitOK
}

func cmdPairs(args []string, stdout, stderr io.Writer) int {
	c := newCommon("pairs")
	var format string
	c.fs.StringVar(&format, "format", "text", "output format: text|json")
	root, cfg, ok := c.parse(args, stderr)
	if !ok {
		return c.exitCode()
	}
	cfg.Stale.Enabled = false
	opts := c.engineOptions(root, cfg, stderr)
	run, err := engine.Check(opts)
	if err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return exitUsage
	}
	var keep []model.Finding
	for _, f := range run.Report.Findings {
		if strings.HasPrefix(f.Rule, "pair-") {
			keep = append(keep, f)
		}
	}
	r := *run.Report
	r.Findings = keep
	r.Summary.Errors, r.Summary.Warnings, r.Summary.Infos = 0, 0, 0
	for _, f := range keep {
		switch f.Severity {
		case model.SevError:
			r.Summary.Errors++
		case model.SevWarning:
			r.Summary.Warnings++
		default:
			r.Summary.Infos++
		}
	}
	if len(keep) == 0 && format == "text" {
		fmt.Fprintf(stdout, "no pair findings (%s pairs)\n", orZero(r.Summary.Extra["pairs"]))
		return exitOK
	}
	if err := report.Write(format, stdout, &r, report.Options{Color: report.ColorEnabled(stdout), Root: root, ShowInfo: true}); err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return exitUsage
	}
	return exitOK
}

func cmdExplain(args []string, stdout, stderr io.Writer) int {
	c := newCommon("explain")
	var kind, rootFlag string
	c.fs.StringVar(&kind, "kind", "", "only show references of this kind")
	c.fs.StringVar(&rootFlag, "root", "", "repo root (default: directory containing .docrot.json or go.mod above the doc, else cwd)")
	c.fs.SetOutput(stderr)
	pos, err := parseInterspersed(c.fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "usage: docrot explain <doc> [--kind K] [--root DIR]")
		return exitUsage
	}
	doc := pos[0]
	root := rootFlag
	if root == "" {
		root = findRoot(doc)
	}
	var cfg config.Config
	if c.configPath != "" {
		cfg, err = config.Load(c.configPath)
	} else {
		cfg, _, err = config.LoadOrDefault(root)
	}
	if err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return exitUsage
	}
	rows, err := engine.Explain(engine.Options{Root: root, Config: cfg, Net: c.net, Stderr: stderr}, doc)
	if err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return exitUsage
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "LINE\tKIND\tCONF\tSTATUS\tTEXT\tNOTE")
	n := 0
	for _, r := range rows {
		if kind != "" && string(r.Ref.Kind) != kind {
			continue
		}
		n++
		note := r.Message
		if note == "" && r.File != "" && r.File != r.Ref.Norm {
			note = "→ " + r.File
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", r.Ref.Loc.Line, r.Ref.Kind, r.Ref.Confidence, r.Status, r.Ref.Text, note)
	}
	tw.Flush()
	fmt.Fprintf(stdout, "\n%d references\n", n)
	return exitOK
}

func cmdIndex(args []string, stdout, stderr io.Writer) int {
	c := newCommon("index")
	var kind string
	c.fs.StringVar(&kind, "kind", "symbols", "symbols|flags|env|paths|anchors|config|odin|python")
	root, cfg, ok := c.parse(args, stderr)
	if !ok {
		return c.exitCode()
	}
	cfg.Stale.Enabled = false
	opts := c.engineOptions(root, cfg, stderr)
	opts.NoGit = true
	run, err := engine.Check(opts)
	if err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return exitUsage
	}
	items := run.Index.Symbols(kind)
	if items == nil && !contains([]string{"symbols", "flags", "env", "paths", "anchors", "config", "odin", "python"}, kind) {
		fmt.Fprintf(stderr, "docrot: unknown index kind %q\n", kind)
		return exitUsage
	}
	sort.Strings(items)
	for _, it := range items {
		fmt.Fprintln(stdout, it)
	}
	fmt.Fprintf(stderr, "%d %s\n", len(items), kind)
	return exitOK
}

func cmdInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docrot init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	p := filepath.Join(dir, config.FileNames[0])
	if err := config.WriteDefault(p); err != nil {
		fmt.Fprintf(stderr, "docrot: %v\n", err)
		return exitUsage
	}
	fmt.Fprintf(stdout, "wrote %s (docrot check rewrites %s/ with the report in every format)\n",
		p, config.DefaultOutDir)
	return exitOK
}

// --- helpers ---

// parseInterspersed parses flags that may appear before or after
// positional arguments (Go's flag package stops at the first positional).
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
		if len(args) == 0 {
			return pos, nil
		}
	}
}

func versionString() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		rev, dirty := "", ""
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					dirty = "-dirty"
				}
			}
		}
		if rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			return release + "+" + rev + dirty
		}
		if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			return bi.Main.Version
		}
	}
	return release
}

// findRoot walks up from doc looking for .docrot.json, go.mod or .git.
func findRoot(doc string) string {
	abs, err := filepath.Abs(doc)
	if err != nil {
		return "."
	}
	dir := filepath.Dir(abs)
	for {
		for _, marker := range []string{config.FileNames[0], "go.mod", ".git"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func pct(a, b int) int {
	if b == 0 {
		return 100
	}
	return int(float64(a)/float64(b)*100 + 0.5)
}

func joinMax(list []string, n int) string {
	if len(list) <= n {
		return strings.Join(list, ", ")
	}
	return strings.Join(list[:n], ", ") + fmt.Sprintf(", … (+%d)", len(list)-n)
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}
