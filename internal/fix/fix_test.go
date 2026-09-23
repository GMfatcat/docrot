package fix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"docrot/internal/model"
)

func finding(file string, line, col int, text, fixText string, baselined bool) model.Finding {
	f := model.Finding{
		Rule: model.RuleMissingPath, Severity: model.SevError,
		Loc: model.Location{File: file, Line: line, Col: col},
		Ref: &model.Reference{Kind: model.KindPath, Text: text, Norm: text},
	}
	if fixText != "" {
		f.Data = map[string]any{DataKey: fixText}
	}
	f.Baselined = baselined
	return f
}

func TestPlan(t *testing.T) {
	findings := []model.Finding{
		finding("docs/b.md", 3, 5, "old/x.go", "new/x.go", false),
		finding("README.md", 12, 0, "readme.MD", "README.md", false),
		finding("README.md", 12, 0, "readme.MD", "README.md", false),                                                  // duplicate
		finding("README.md", 2, 1, "a.go", "", false),                                                                 // no fix text
		finding("README.md", 4, 1, "b.go", "c.go", true),                                                              // baselined
		finding("README.md", 5, 1, "same.go", "same.go", false),                                                       // nothing to change
		{Rule: model.RuleMissingPath, Loc: model.Location{File: "x.md", Line: 1}, Data: map[string]any{DataKey: "y"}}, // no Ref
	}
	got := Plan(findings)
	if len(got) != 2 {
		t.Fatalf("Plan = %+v, want 2 edits", got)
	}
	if got[0].File != "README.md" || got[0].Line != 12 || got[0].Old != "readme.MD" || got[0].New != "README.md" || got[0].Rule != model.RuleMissingPath {
		t.Errorf("edit 0 = %+v", got[0])
	}
	if got[1].File != "docs/b.md" || got[1].Col != 5 {
		t.Errorf("edit 1 = %+v", got[1])
	}
}

func TestApply(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// CRLF with a BOM, two edits on one line, one on another, one stale
	write("README.md", "\xef\xbb\xbf# T\r\nSee [a](docs/guid.md) and `docs/guid.md` and `Readme.md`.\r\nOld `lib/x.go` here.\r\n")
	write("docs/b.md", "one\ntwo `old/x.go` here\n")
	edits := []Edit{
		{File: "README.md", Line: 2, Col: 5, Old: "docs/guid.md", New: "docs/guide.md"},  // the link: column of "["
		{File: "README.md", Line: 2, Col: 27, Old: "docs/guid.md", New: "docs/guide.md"}, // the span
		{File: "README.md", Line: 2, Col: 46, Old: "Readme.md", New: "README.md"},
		{File: "README.md", Line: 3, Col: 6, Old: "lib/y.go", New: "lib/z.go"}, // not on the line any more
		{File: "docs/b.md", Line: 2, Old: "old/x.go", New: "new/x.go"},
	}

	// dry run: nothing written
	res, err := Apply(root, edits, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].File != "README.md" || res[1].File != "docs/b.md" {
		t.Fatalf("results = %+v", res)
	}
	r := res[0]
	if len(r.Applied) != 3 || len(r.Skipped) != 1 || r.Skipped[0].Old != "lib/y.go" {
		t.Errorf("README: applied %d skipped %+v", len(r.Applied), r.Skipped)
	}
	if len(r.Changes) != 1 || r.Changes[0].After != "See [a](docs/guide.md) and `docs/guide.md` and `README.md`." {
		t.Errorf("README changes = %+v", r.Changes)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "README.md")); !strings.Contains(string(b), "docs/guid.md") {
		t.Error("dry run wrote the file")
	}

	// apply: line endings and the BOM survive, the other lines are untouched
	if _, err := Apply(root, edits, true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "README.md"))
	want := "\xef\xbb\xbf# T\r\nSee [a](docs/guide.md) and `docs/guide.md` and `README.md`.\r\nOld `lib/x.go` here.\r\n"
	if string(b) != want {
		t.Errorf("README after apply:\n%q\nwant\n%q", b, want)
	}
	b, _ = os.ReadFile(filepath.Join(root, "docs/b.md"))
	if string(b) != "one\ntwo `new/x.go` here\n" {
		t.Errorf("docs/b.md after apply: %q", b)
	}

	// a missing file is an error
	if _, err := Apply(root, []Edit{{File: "nope.md", Line: 1, Old: "a", New: "b"}}, false); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestLocate(t *testing.T) {
	line := "x docs/a.md y docs/a.md"
	cases := []struct {
		col, want int
	}{
		{3, 2},   // exactly at the column
		{1, 2},   // after the column
		{15, 14}, // the second occurrence
		{0, 2},   // no column: first occurrence
		{99, 2},  // column past the end: first occurrence
	}
	for _, c := range cases {
		if got := locate(line, Edit{Col: c.col, Old: "docs/a.md"}); got != c.want {
			t.Errorf("locate(col=%d) = %d, want %d", c.col, got, c.want)
		}
	}
	if got := locate(line, Edit{Old: "zzz"}); got != -1 {
		t.Errorf("absent text located at %d", got)
	}
}
