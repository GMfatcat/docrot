package engine

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"docrot/internal/config"
	"docrot/internal/model"
	"docrot/internal/report"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func checkFixture(t *testing.T) *Run {
	t.Helper()
	root := fixtureRoot(t)
	cfg, _, err := config.LoadOrDefault(root)
	if err != nil {
		t.Fatal(err)
	}
	run, err := Check(Options{Root: root, Config: cfg, NoGit: true, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

type want struct {
	rule string
	file string
	text string // Ref.Text (or "" for pair/stale findings → matched by message substring)
	sev  model.Severity
	sugg string
}

// TestFixtureGolden pins the exact set of error/warning findings for the
// fixture repo. Info findings are listed but not required to be exact.
func TestFixtureGolden(t *testing.T) {
	run := checkFixture(t)
	wants := []want{
		{model.RuleMissingPath, "README.md", "pkg/httpx/router.go", model.SevError, ""},
		{model.RuleMissingPath, "README.md", "docs/guid.md", model.SevError, "docs/guide.md"},
		{model.RuleBrokenAnchor, "README.md", "docs/guide.md#instal", model.SevError, "#install"},
		{model.RuleMissingCommand, "README.md", "./scripts/build.ps1", model.SevError, ""},
		{model.RuleMissingCommand, "README.md", "./cmd/server", model.SevError, ""},
		{model.RuleMissingSymbol, "README.md", "httpx.WriteDatum(w, v)", model.SevError, "httpx.WriteData"},
		{model.RuleMissingSymbol, "README.md", "Server.Address()", model.SevError, ""},
		{model.RuleMissingImport, "README.md", "example.com/fixture/pkg/router", model.SevError, ""},
		{model.RuleUnknownFlag, "README.md", "--confg", model.SevWarning, "--config"},
		{model.RuleUnknownFlag, "README.md", "--port", model.SevWarning, ""},
		{model.RuleUnknownEnv, "README.md", "FIXTURE_TRACE", model.SevWarning, ""},
		{model.RuleMissingSymbol, "README.md", "render_frames()", model.SevWarning, "fixture_odin.render_frame"},
		{model.RuleMissingSymbol, "README.md", "helper.summarise", model.SevError, "helper.summarize"},
		{model.RuleMissingPath, "llms.txt", "pkg/httpx/README.md", model.SevError, ""},
		{model.RuleBrokenAnchor, "docs/guide.md", "../README.md#nope", model.SevError, ""},
		{model.RuleMissingCommand, "README-zh.md", "./scripts/build.ps1", model.SevError, ""},
		{model.RuleMissingImport, "README-zh.md", "example.com/fixture/pkg/router", model.SevError, ""},
		{model.RulePairHeading, "README-zh.md", "", model.SevWarning, ""},
		{model.RulePairCode, "README-zh.md", "", model.SevWarning, ""},
		{model.RulePairLink, "README-zh.md", "docs/guid.md", model.SevWarning, ""},
		{model.RulePairLink, "README-zh.md", "docs/guide.md#instal", model.SevWarning, ""},
		{model.RulePairLink, "README-zh.md", "https://example.com/zh-only", model.SevWarning, ""},
	}
	got := run.Report.Findings
	matched := make([]bool, len(got))
	for _, w := range wants {
		found := false
		for i, f := range got {
			if matched[i] || f.Rule != w.rule || f.Loc.File != w.file {
				continue
			}
			if w.text != "" {
				if f.Ref != nil {
					if f.Ref.Text != w.text {
						continue
					}
				} else if !strings.Contains(f.Message, w.text) {
					continue
				}
			}
			matched[i] = true
			found = true
			if f.Severity != w.sev {
				t.Errorf("%s %s %q: severity %s, want %s", w.rule, w.file, w.text, f.Severity, w.sev)
			}
			if w.sugg != "" && f.Suggestion != w.sugg {
				t.Errorf("%s %s %q: suggestion %q, want %q", w.rule, w.file, w.text, f.Suggestion, w.sugg)
			}
			break
		}
		if !found {
			t.Errorf("missing expected finding: %s %s %q", w.rule, w.file, w.text)
		}
	}
	var unexpected []string
	for i, f := range got {
		if matched[i] {
			continue
		}
		line := fmt.Sprintf("%s %s %s %s", f.Loc, f.Severity, f.Rule, f.Message)
		if f.Severity == model.SevInfo {
			t.Logf("info: %s", line)
			continue
		}
		unexpected = append(unexpected, line)
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		t.Errorf("unexpected error/warning findings:\n  %s", strings.Join(unexpected, "\n  "))
	}
	// info findings we do expect
	infoWant := map[string]bool{"unknown-config-key|server.port": false, "pair-number|1.2.3": false, "pair-number|1.2.4": false}
	for _, f := range got {
		if f.Severity != model.SevInfo {
			continue
		}
		for k := range infoWant {
			rule, text, _ := strings.Cut(k, "|")
			if f.Rule == rule && ((f.Ref != nil && f.Ref.Text == text) || strings.Contains(f.Message, text)) {
				infoWant[k] = true
			}
		}
	}
	for k, ok := range infoWant {
		if !ok {
			t.Errorf("missing expected info finding %s", k)
		}
	}
	s := run.Report.Summary
	if s.Docs != 4 {
		t.Errorf("docs = %d, want 4", s.Docs)
	}
	if s.References < 40 {
		t.Errorf("references = %d, suspiciously low", s.References)
	}
	if s.Git != report.GitDisabled {
		t.Errorf("git = %q, want disabled", s.Git)
	}
}

func TestFixtureReportsRender(t *testing.T) {
	run := checkFixture(t)
	for _, f := range report.Formats {
		var buf bytes.Buffer
		if err := report.Write(f, &buf, run.Report, report.Options{}); err != nil {
			t.Errorf("%s: %v", f, err)
		}
		if buf.Len() == 0 {
			t.Errorf("%s: empty output", f)
		}
	}
}

func TestExplain(t *testing.T) {
	root := fixtureRoot(t)
	cfg, _, _ := config.LoadOrDefault(root)
	rows, err := Explain(Options{Root: root, Config: cfg}, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]int{}
	for _, r := range rows {
		statuses[r.Status]++
	}
	if statuses["ok"] == 0 || statuses["missing"] == 0 {
		t.Fatalf("explain statuses: %v", statuses)
	}
}

func TestCoverage(t *testing.T) {
	root := fixtureRoot(t)
	cfg, _, _ := config.LoadOrDefault(root)
	run, err := Check(Options{Root: root, Config: cfg, NoGit: true, Coverage: true})
	if err != nil {
		t.Fatal(err)
	}
	if run.Report.Coverage == nil {
		t.Fatal("no coverage")
	}
	var httpx *report.PackageCoverage
	for i := range run.Report.Coverage.Packages {
		if run.Report.Coverage.Packages[i].Package == "httpx" {
			httpx = &run.Report.Coverage.Packages[i]
		}
	}
	if httpx == nil {
		t.Fatal("no httpx coverage")
	}
	if !contains(httpx.Missing, "httpx.Undocumented") {
		t.Errorf("httpx.Undocumented should be missing; got %v", httpx.Missing)
	}
	if contains(httpx.Missing, "httpx.NewServer") || contains(httpx.Missing, "httpx.WriteData") {
		t.Errorf("documented symbols reported missing: %v", httpx.Missing)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// parseDocs (and the staleness stage) call the warn callback from several
// worker goroutines at once, so Check's warn closure has to serialise the
// append to Run.Warnings and the write to Stderr. This pins that contract:
// with an unsynchronised callback the race detector fires here.
func TestParseDocsWarnsFromWorkers(t *testing.T) {
	var mu sync.Mutex
	var warnings []string
	warn := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}
	docs := make([]string, 200)
	for i := range docs {
		docs[i] = fmt.Sprintf("no-such-dir/missing-%d.md", i)
	}
	parsed := parseDocs(t.TempDir(), docs, warn)
	if len(parsed) != 0 {
		t.Errorf("parsed %d docs, want 0", len(parsed))
	}
	if len(warnings) != len(docs) {
		t.Errorf("got %d warnings, want %d", len(warnings), len(docs))
	}
}
