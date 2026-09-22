package report

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"docrot/internal/model"
)

// sampleReport builds a report with one finding per severity plus a
// baselined one, a pair finding and a coverage section.
func sampleReport() *Report {
	return &Report{
		Version: "0.1.0",
		Summary: Summary{
			Root:       "/repo",
			Docs:       14,
			References: 1204,
			Errors:     1,
			Warnings:   1,
			Infos:      1,
			Baselined:  1,
			Fixed:      1,
			Duration:   830 * time.Millisecond,
			Git:        GitOK,
			Extra:      map[string]string{"go packages": "14"},
		},
		Findings: []model.Finding{
			{
				Rule:        model.RuleMissingSymbol,
				Severity:    model.SevError,
				Message:     "`httpx.WriteJSON` not found in package httpx",
				Loc:         model.Location{File: "README.md", Line: 42, Col: 15},
				Suggestion:  "httpx.WriteData",
				Fingerprint: "aaaa1111",
				Ref:         &model.Reference{Kind: model.KindGoSymbol, Text: "httpx.WriteJSON", Context: "see `httpx.WriteJSON`"},
			},
			{
				Rule:        model.RuleMissingPath,
				Severity:    model.SevWarning,
				Message:     "`docs/contract.md` not found",
				Loc:         model.Location{File: "llms.txt", Line: 12, Col: 3},
				Suggestion:  "docs/contracts.md",
				Fingerprint: "bbbb2222",
			},
			{
				Rule:        model.RulePairLag,
				Severity:    model.SevInfo,
				Message:     "4 commits to README.md since README-zh.md last changed",
				Loc:         model.Location{File: "README-zh.md"},
				Fingerprint: "cccc3333",
			},
			{
				Rule:        model.RuleUnknownFlag,
				Severity:    model.SevWarning,
				Message:     "`--verbose` is not defined",
				Loc:         model.Location{File: "README.md", Line: 8, Col: 1},
				Fingerprint: "dddd4444",
				Baselined:   true,
			},
		},
		Coverage: &Coverage{
			Packages: []PackageCoverage{{Package: "httpx", Total: 4, Documented: 3, Missing: []string{"httpx.Close"}}},
			Flags:    CoverageGroup{Total: 2, Documented: 1, Missing: []string{"--quiet"}},
			Envs:     CoverageGroup{Total: 0, Documented: 0},
		},
	}
}

func TestSort(t *testing.T) {
	in := []model.Finding{
		{Rule: "z", Severity: model.SevInfo, Loc: model.Location{File: "b.md", Line: 1}},
		{Rule: "a", Severity: model.SevWarning, Loc: model.Location{File: "a.md", Line: 10, Col: 2}},
		{Rule: "b", Severity: model.SevError, Loc: model.Location{File: "a.md", Line: 10, Col: 2}},
		{Rule: "a", Severity: model.SevError, Loc: model.Location{File: "a.md", Line: 2}},
		{Rule: "a", Severity: model.SevError, Loc: model.Location{File: "a.md", Line: 10, Col: 1}},
		{Rule: "a", Severity: model.SevError, Message: "x", Loc: model.Location{File: "a.md"}},
		{Rule: "a", Severity: model.SevError, Message: "a", Loc: model.Location{File: "a.md"}},
	}
	Sort(in)

	type key struct {
		file string
		line int
		col  int
		sev  model.Severity
		rule string
		msg  string
	}
	want := []key{
		{"a.md", 0, 0, model.SevError, "a", "a"},
		{"a.md", 0, 0, model.SevError, "a", "x"},
		{"a.md", 2, 0, model.SevError, "a", ""},
		{"a.md", 10, 1, model.SevError, "a", ""},
		{"a.md", 10, 2, model.SevError, "b", ""},
		{"a.md", 10, 2, model.SevWarning, "a", ""},
		{"b.md", 1, 0, model.SevInfo, "z", ""},
	}
	for i, w := range want {
		got := key{in[i].Loc.File, in[i].Loc.Line, in[i].Loc.Col, in[i].Severity, in[i].Rule, in[i].Message}
		if got != w {
			t.Errorf("findings[%d] = %+v, want %+v", i, got, w)
		}
	}
}

func TestSortIsStable(t *testing.T) {
	in := []model.Finding{
		{Rule: "a", Severity: model.SevError, Message: "m", Loc: model.Location{File: "a.md"}, Fingerprint: "1"},
		{Rule: "a", Severity: model.SevError, Message: "m", Loc: model.Location{File: "a.md"}, Fingerprint: "2"},
	}
	Sort(in)
	if in[0].Fingerprint != "1" || in[1].Fingerprint != "2" {
		t.Errorf("Sort reordered equal findings: %q, %q", in[0].Fingerprint, in[1].Fingerprint)
	}
}

func TestColorEnabled(t *testing.T) {
	if ColorEnabled(&bytes.Buffer{}) {
		t.Error("ColorEnabled(*bytes.Buffer) = true, want false")
	}
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if ColorEnabled(f) {
		t.Error("ColorEnabled(regular file) = true, want false")
	}
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled(os.Stdout) {
		t.Error("ColorEnabled with NO_COLOR set = true, want false")
	}
}

func TestWriteDispatch(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"text", "README.md:42:15:"},
		{"", "README.md:42:15:"},
		{"json", `"docrot": "0.1.0"`},
		{"JSON", `"docrot": "0.1.0"`},
		{"sarif", `"version": "2.1.0"`},
		{"html", "<!doctype html>"},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Write(tt.format, &buf, sampleReport(), Options{}); err != nil {
				t.Fatalf("Write(%q): %v", tt.format, err)
			}
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("Write(%q) output missing %q", tt.format, tt.want)
			}
		})
	}
	if err := Write("yaml", &bytes.Buffer{}, sampleReport(), Options{}); err == nil {
		t.Error(`Write("yaml") = nil error, want an unknown-format error`)
	}
}

func TestHumanInt(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{7, "7"},
		{999, "999"},
		{1000, "1,000"},
		{1204, "1,204"},
		{1234567, "1,234,567"},
		{-1204, "-1,204"},
	}
	for _, tt := range tests {
		if got := humanInt(tt.in); got != tt.want {
			t.Errorf("humanInt(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRelPath(t *testing.T) {
	if got := relPath("", "docs\\a.md"); got != "docs/a.md" {
		t.Errorf("relPath = %q, want docs/a.md", got)
	}
	if got := relPath("/repo", "docs/a.md"); got != "docs/a.md" {
		t.Errorf("relPath of an already-relative path = %q", got)
	}
}

// decodeJSON is a helper: runs WriteJSON and unmarshals into a map.
func decodeJSON(t *testing.T, r *Report, o Options) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteJSON(&buf, r, o); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("WriteJSON produced invalid JSON: %v\n%s", err, buf.String())
	}
	return m
}
