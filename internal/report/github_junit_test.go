package report

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"docrot/internal/model"
)

// ciReport is a small report with one finding per severity plus one
// baselined finding, for the CI-facing writers.
func ciReport() *Report {
	return &Report{
		Version: "0.9.0",
		Summary: Summary{Docs: 2, References: 10, Errors: 1, Warnings: 1, Infos: 1, Baselined: 1, Duration: 830 * time.Millisecond, Git: GitOK},
		Findings: []model.Finding{
			{Rule: model.RuleMissingPath, Severity: model.SevError, Message: "`docs/guid.md` not found", Loc: model.Location{File: "README.md", Line: 12, Col: 3}, Suggestion: "docs/guide.md"},
			{Rule: model.RuleMissingPath, Severity: model.SevError, Message: "`a,b:c` not found\nsecond line 100%", Loc: model.Location{File: "README.md", Line: 14}},
			{Rule: model.RuleUnknownFlag, Severity: model.SevWarning, Message: "flag `--confg` is not defined", Loc: model.Location{File: "README.md", Line: 20, Col: 8}, Suggestion: "--config"},
			{Rule: model.RuleUnknownConfigKey, Severity: model.SevInfo, Message: "config key `x.y` not found", Loc: model.Location{File: "docs/a.md", Line: 3}},
			{Rule: model.RuleMissingSymbol, Severity: model.SevError, Message: "old", Loc: model.Location{File: "docs/a.md", Line: 9}, Baselined: true},
			{Rule: model.RulePairLag, Severity: model.SevWarning, Message: "2 commits behind", Loc: model.Location{File: "README-zh.md", Line: 1}},
		},
	}
}

func TestGitHubAnnotations(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteGitHub(&buf, ciReport(), Options{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	want := []string{
		"::warning file=README-zh.md,line=1,title=pair-lag::2 commits behind",
		"::error file=README.md,line=12,col=3,title=missing-path::`docs/guid.md` not found (did you mean docs/guide.md?)",
		"::error file=README.md,line=14,title=missing-path::`a,b:c` not found%0Asecond line 100%25",
		"::warning file=README.md,line=20,col=8,title=unknown-flag::flag `--confg` is not defined (did you mean --config?)",
	}
	if len(lines) != len(want)+1 {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(want)+1, buf.String())
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d:\n got %q\nwant %q", i, lines[i], w)
		}
	}
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "::notice title=docrot::1 error, 1 warning, 1 info") {
		t.Errorf("summary notice = %q", last)
	}

	// --info and --all add the notice-level and baselined findings
	buf.Reset()
	if err := WriteGitHub(&buf, ciReport(), Options{ShowInfo: true, ShowBaselined: true}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "::notice file=docs/a.md,line=3,title=unknown-config-key::config key `x.y` not found") {
		t.Errorf("info finding missing with ShowInfo:\n%s", out)
	}
	if !strings.Contains(out, "::error file=docs/a.md,line=9,title=missing-symbol::[baselined] old") {
		t.Errorf("baselined finding missing with ShowBaselined:\n%s", out)
	}
}

func TestGitHubEscapesProperties(t *testing.T) {
	r := &Report{Findings: []model.Finding{{Rule: "x:y", Severity: model.SevError, Message: "m", Loc: model.Location{File: "a,b:c.md", Line: 1}}}}
	var buf bytes.Buffer
	if err := WriteGitHub(&buf, r, Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "::error file=a%2Cb%3Ac.md,line=1,title=x%3Ay::m\n") {
		t.Errorf("unescaped property: %q", buf.String())
	}
}

func TestJUnit(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJUnit(&buf, ciReport(), Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), xml.Header) {
		t.Errorf("no XML header: %q", buf.String()[:40])
	}
	var got junitSuites
	if err := xml.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, buf.String())
	}
	if len(got.Suites) != 1 || got.Suites[0].Name != "docrot" {
		t.Fatalf("suites = %+v", got.Suites)
	}
	s := got.Suites[0]
	// README.md×missing-path, README.md×unknown-flag, docs/a.md×unknown-config-key (info → skipped),
	// docs/a.md×missing-symbol (baselined → skipped), README-zh.md×pair-lag, plus the summary case
	if s.Tests != 6 || s.Failures != 3 || s.Skipped != 2 {
		t.Errorf("tests=%d failures=%d skipped=%d, want 6/3/2", s.Tests, s.Failures, s.Skipped)
	}
	if got.Tests != s.Tests || got.Failures != s.Failures || got.Skipped != s.Skipped {
		t.Errorf("root counts %d/%d/%d differ from the suite's", got.Tests, got.Failures, got.Skipped)
	}
	byName := map[string]junitCase{}
	for _, c := range s.Cases {
		byName[c.ClassName+" "+c.Name] = c
	}
	mp := byName["README.md missing-path"]
	if mp.Failure == nil || mp.Failure.Type != "error" {
		t.Fatalf("README.md missing-path = %+v", mp)
	}
	if !strings.HasPrefix(mp.Failure.Message, "README.md:12:3: error missing-path `docs/guid.md` not found (did you mean docs/guide.md?)") {
		t.Errorf("failure message = %q", mp.Failure.Message)
	}
	if !strings.Contains(mp.Failure.Body, "README.md:12:3:") || !strings.Contains(mp.Failure.Body, "README.md:14: error missing-path `a,b:c` not found\nsecond line 100%") {
		t.Errorf("failure body should list both findings:\n%s", mp.Failure.Body)
	}
	if c := byName["docs/a.md unknown-config-key"]; c.Skipped == nil || c.Failure != nil || !strings.Contains(c.Skipped.Message, "info") {
		t.Errorf("info case = %+v", c)
	}
	if c := byName["docs/a.md missing-symbol"]; c.Skipped == nil || !strings.Contains(c.Skipped.Message, "baselined") {
		t.Errorf("baselined case = %+v", c)
	}
	if c := byName["docrot summary"]; c.Failure != nil || c.Skipped != nil || !strings.Contains(c.SystemOut, "1 error, 1 warning") {
		t.Errorf("summary case = %+v", c)
	}
	if s.Time != "0.830" {
		t.Errorf("suite time = %q", s.Time)
	}

	// with ShowBaselined the baselined group fails like any other
	buf.Reset()
	if err := WriteJUnit(&buf, ciReport(), Options{ShowBaselined: true}); err != nil {
		t.Fatal(err)
	}
	if err := xml.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Failures != 4 || got.Skipped != 1 {
		t.Errorf("with ShowBaselined: failures=%d skipped=%d, want 4/1", got.Failures, got.Skipped)
	}
}

func TestJUnitCleanRun(t *testing.T) {
	var buf bytes.Buffer
	r := &Report{Summary: Summary{Docs: 3, Duration: time.Second}}
	if err := WriteJUnit(&buf, r, Options{}); err != nil {
		t.Fatal(err)
	}
	var got junitSuites
	if err := xml.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Tests != 1 || got.Failures != 0 || got.Skipped != 0 {
		t.Errorf("clean run: tests=%d failures=%d skipped=%d", got.Tests, got.Failures, got.Skipped)
	}
}

func TestFormatsListCIWriters(t *testing.T) {
	for _, name := range []string{"github", "junit"} {
		if !contains(Formats, name) {
			t.Errorf("Formats does not list %s: %v", name, Formats)
		}
		var buf bytes.Buffer
		if err := Write(name, &buf, ciReport(), Options{}); err != nil || buf.Len() == 0 {
			t.Errorf("Write(%s): err=%v len=%d", name, err, buf.Len())
		}
	}
}
