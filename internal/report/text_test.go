package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"docrot/internal/model"
)

func TestTextLines(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteText(&buf, sampleReport(), Options{ShowInfo: true}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")

	want := []string{
		"README-zh.md: info pair-lag 4 commits to README.md since README-zh.md last changed",
		"README.md:42:15: error missing-symbol `httpx.WriteJSON` not found in package httpx (did you mean httpx.WriteData?)",
		"llms.txt:12:3: warning missing-path `docs/contract.md` not found (did you mean docs/contracts.md?)",
		"",
		"1 error, 1 warning, 1 info (1 baselined, 1 fixed) — 14 docs, 1,204 references, 0.83s",
		"go packages: 14",
	}
	for i, w := range want {
		if i >= len(lines) {
			t.Fatalf("output has %d lines, want at least %d:\n%s", len(lines), len(want), buf.String())
		}
		if lines[i] != w {
			t.Errorf("line %d =\n  %q\nwant\n  %q", i, lines[i], w)
		}
	}
	if strings.Contains(buf.String(), "--verbose") {
		t.Error("baselined finding leaked into the default text report")
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Error("text report emitted ANSI escapes with Color off")
	}
}

func TestTextShowBaselined(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteText(&buf, sampleReport(), Options{ShowBaselined: true, ShowInfo: true}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	want := "[baselined] README.md:8:1: warning unknown-flag `--verbose` is not defined"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("output missing %q:\n%s", want, buf.String())
	}
}

func TestTextColor(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteText(&buf, sampleReport(), Options{Color: true, ShowInfo: true}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		ansiRed + "error" + ansiReset,
		ansiYellow + "warning" + ansiReset,
		ansiCyan + "info" + ansiReset,
		ansiDim + "missing-symbol" + ansiReset,
		ansiGreen + "(did you mean httpx.WriteData?)" + ansiReset,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("coloured output missing %q", want)
		}
	}
}

func TestTextCoverage(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteText(&buf, sampleReport(), Options{ShowInfo: true}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	for _, want := range []string{
		"Coverage:",
		"  httpx: 3/4 (75%) missing: httpx.Close",
		"  flags: 1/2 (50%) missing: --quiet",
		"  envs: 0/0 (100%)",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("coverage output missing %q:\n%s", want, buf.String())
		}
	}
}

func TestTextNoCoverageSection(t *testing.T) {
	r := sampleReport()
	r.Coverage = nil
	var buf bytes.Buffer
	if err := WriteText(&buf, r, Options{ShowInfo: true}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if strings.Contains(buf.String(), "Coverage:") {
		t.Error("coverage section printed for a nil Coverage")
	}
}

func TestSummaryLine(t *testing.T) {
	tests := []struct {
		name    string
		summary Summary
		want    string
	}{
		{
			name:    "clean run",
			summary: Summary{Docs: 1, References: 1, Git: GitOK},
			want:    "0 errors, 0 warnings, 0 info — 1 doc, 1 reference, 0.00s",
		},
		{
			name: "spec example",
			summary: Summary{
				Errors: 3, Warnings: 2, Infos: 5, Baselined: 12, Fixed: 1,
				Docs: 14, References: 1204, Duration: 830 * time.Millisecond, Git: GitOK,
			},
			want: "3 errors, 2 warnings, 5 info (12 baselined, 1 fixed) — 14 docs, 1,204 references, 0.83s",
		},
		{
			name:    "baselined only",
			summary: Summary{Baselined: 12, Docs: 2, References: 3, Git: GitOK},
			want:    "0 errors, 0 warnings, 0 info (12 baselined) — 2 docs, 3 references, 0.00s",
		},
		{
			name:    "git unavailable",
			summary: Summary{Docs: 2, References: 3, Git: GitUnavailable},
			want:    "0 errors, 0 warnings, 0 info — 2 docs, 3 references, 0.00s (git: unavailable)",
		},
		{
			name:    "git disabled",
			summary: Summary{Docs: 2, References: 3, Git: GitDisabled},
			want:    "0 errors, 0 warnings, 0 info — 2 docs, 3 references, 0.00s (git: disabled)",
		},
		{
			name:    "no git status",
			summary: Summary{Docs: 2, References: 3},
			want:    "0 errors, 0 warnings, 0 info — 2 docs, 3 references, 0.00s",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SummaryLine(tt.summary); got != tt.want {
				t.Errorf("SummaryLine() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

func TestTextEmptyReport(t *testing.T) {
	var buf bytes.Buffer
	r := &Report{Summary: Summary{Git: GitOK}}
	if err := WriteText(&buf, r, Options{ShowInfo: true}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	want := "0 errors, 0 warnings, 0 info — 0 docs, 0 references, 0.00s\n"
	if buf.String() != want {
		t.Errorf("empty report =\n  %q\nwant\n  %q", buf.String(), want)
	}
}

func TestCoverageCellCapsMissing(t *testing.T) {
	missing := make([]string, 13)
	for i := range missing {
		missing[i] = string(rune('a' + i))
	}
	got := coverageCell(0, 13, missing)
	if !strings.Contains(got, "and 3 more") {
		t.Errorf("coverageCell = %q, want it to cap at %d names", got, maxMissingListed)
	}
	if strings.Contains(got, ", k,") {
		t.Errorf("coverageCell listed more than %d names: %q", maxMissingListed, got)
	}
}

func TestTextLocationFormats(t *testing.T) {
	tests := []struct {
		name string
		loc  model.Location
		want string
	}{
		{"file only", model.Location{File: "a.md"}, "a.md: "},
		{"file and line", model.Location{File: "a.md", Line: 3}, "a.md:3: "},
		{"full", model.Location{File: "a.md", Line: 3, Col: 9}, "a.md:3:9: "},
		{"windows separators", model.Location{File: "docs\\a.md", Line: 1}, "docs/a.md:1: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := model.Finding{Rule: "r", Severity: model.SevInfo, Message: "m", Loc: tt.loc}
			got := textLine(f, Options{}, palette(false))
			if !strings.HasPrefix(got, tt.want) {
				t.Errorf("textLine = %q, want prefix %q", got, tt.want)
			}
		})
	}
}

func TestTextHidesInfoByDefault(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteText(&buf, sampleReport(), Options{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, " info pair-lag") {
		t.Fatal("info finding should be hidden without ShowInfo")
	}
	if !strings.Contains(out, "info hidden; --info to show") {
		t.Fatalf("missing hidden-info note in summary:\n%s", out)
	}
}
