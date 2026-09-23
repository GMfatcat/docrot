package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"docrot/internal/config"
	"docrot/internal/model"
)

// TestCaseSensitivityIsPlatformIndependent pins the behaviour the README
// promises: a path that differs from the real file only by letter case is
// a finding on Windows and macOS too, and a document called README.MD is
// still discovered.
func TestCaseSensitivityIsPlatformIndependent(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("Docs/Guide.md", "# Guide\n")
	mk("Readme.MD", "# T\n\nSee `docs/guide.md`, `Docs/Guide.md` and [g](DOCS/GUIDE.md).\n")
	mk("go.mod", "module example.com/c\n")

	cfg := config.Default()
	run, err := Check(Options{Root: root, Config: cfg, NoGit: true, NoOut: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Docs) != 2 {
		t.Fatalf("docs = %v, want Docs/Guide.md and Readme.MD", run.Docs)
	}
	var caseFindings []string
	for _, f := range run.Report.Findings {
		if f.Rule == model.RuleMissingPath && strings.Contains(f.Message, "letter case") {
			caseFindings = append(caseFindings, f.Ref.Text+"->"+f.Suggestion)
		}
	}
	want := []string{"docs/guide.md->Docs/Guide.md", "DOCS/GUIDE.md->Docs/Guide.md"}
	if len(caseFindings) != len(want) {
		t.Fatalf("case findings = %v, want %v (all: %+v)", caseFindings, want, run.Report.Findings)
	}
	for i := range want {
		if caseFindings[i] != want[i] {
			t.Errorf("case finding %d = %q, want %q", i, caseFindings[i], want[i])
		}
	}
	for _, f := range run.Report.Findings {
		if f.Ref != nil && f.Ref.Text == "Docs/Guide.md" {
			t.Errorf("exact-case reference must not be reported: %+v", f)
		}
	}
}
