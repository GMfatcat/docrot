package stale

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"docrot/internal/gitx"
	"docrot/internal/markdown"
	"docrot/internal/model"
)

// Fixed commit times for the fixture repositories.
var (
	t0 = time.Unix(1704067200, 0) // 2024-01-01T00:00:00Z, adds every file
	t1 = time.Unix(1704153600, 0) // 2024-01-02T00:00:00Z
	t2 = time.Unix(1704240000, 0) // 2024-01-03T00:00:00Z
	t3 = time.Unix(1704326400, 0) // 2024-01-04T00:00:00Z
)

// docContent is committed as doc.md: two sections, one per referenced file.
const docContent = "# Alpha\n\nSee `a.go` for details.\n\n# Beta\n\nSee `b.go` for details.\n"

// preambleContent starts with a section that has no heading.
const preambleContent = "Preamble mentions `a.go`.\n\n# Gamma\n\nNothing here.\n"

// git runs a git command in dir, failing the test on error. When at is
// non-zero it is used as both the author and the committer date.
func git(t *testing.T, dir string, at time.Time, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	if !at.IsZero() {
		stamp := at.UTC().Format("2006-01-02T15:04:05+00:00")
		cmd.Env = append(cmd.Env, "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// writeFile creates dir/rel with the given content.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commit stages everything and commits it at the given time.
func commit(t *testing.T, dir string, at time.Time, msg string) {
	t.Helper()
	git(t, dir, time.Time{}, "add", "-A")
	git(t, dir, at, "commit", "-q", "-m", msg)
}

// fixture builds a repository holding doc.md, preamble.md, a.go and b.go,
// all committed at t0. churn names the files that get one extra commit at
// t1, t2 and t3 each.
func fixture(t *testing.T, churn ...string) string {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	git(t, dir, time.Time{}, "-c", "init.defaultBranch=main", "init", "-q", ".")
	for _, kv := range [][2]string{
		{"user.name", "docrot test"},
		{"user.email", "docrot@example.invalid"},
		{"commit.gpgsign", "false"},
		{"core.autocrlf", "false"},
	} {
		git(t, dir, time.Time{}, "config", kv[0], kv[1])
	}

	writeFile(t, dir, "doc.md", docContent)
	writeFile(t, dir, "preamble.md", preambleContent)
	writeFile(t, dir, "a.go", "package main\n")
	writeFile(t, dir, "b.go", "package main\n")
	commit(t, dir, t0, "initial")

	for i, at := range []time.Time{t1, t2, t3} {
		for _, f := range churn {
			writeFile(t, dir, f, "package main\n\n// change "+string(rune('a'+i))+"\n")
		}
		commit(t, dir, at, "churn")
	}
	return dir
}

// open returns a Repo for dir.
func open(t *testing.T, dir string) *gitx.Repo {
	t.Helper()
	repo, err := gitx.Open(dir, gitx.Options{})
	if err != nil {
		t.Fatalf("gitx.Open: %v", err)
	}
	return repo
}

// docRefs are the references of doc.md: a.go in Alpha, b.go in Beta.
func docRefs() []ResolvedRef {
	return []ResolvedRef{
		{Ref: ref("a.go", 3), File: "a.go"},
		{Ref: ref("b.go", 7), File: "b.go"},
	}
}

func ref(text string, line int) model.Reference {
	return model.Reference{
		Kind:       model.KindPath,
		Text:       text,
		Norm:       text,
		Confidence: model.High,
		Loc:        model.Location{File: "doc.md", Line: line},
	}
}

func TestAnalyzeThresholds(t *testing.T) {
	dir := fixture(t, "a.go")
	repo := open(t, dir)
	doc := markdown.Parse("doc.md", []byte(docContent))

	tests := []struct {
		name string
		opts Options
		want bool // one finding for Alpha
	}{
		{"defaults: three commits reach minChurn", Options{}, true},
		{"minChurn not reached and the change is recent", Options{MinChurn: 10, MinDays: 90}, false},
		{"minChurn not reached but the change is old", Options{MinChurn: 10, MinDays: 1}, true},
		{"exactly minChurn", Options{MinChurn: 3, MinDays: 3650}, true},
		{"one above minChurn", Options{MinChurn: 4, MinDays: 3650}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(repo, doc, "doc.md", docRefs(), tt.opts)
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if !tt.want {
				if len(got) != 0 {
					t.Fatalf("Analyze() = %+v, want no findings", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("Analyze() = %+v, want one finding", got)
			}
			f := got[0]
			if f.Rule != model.RuleStaleSection || f.Severity != model.SevWarning {
				t.Errorf("rule/severity = %q/%q, want %q/%q",
					f.Rule, f.Severity, model.RuleStaleSection, model.SevWarning)
			}
			if f.Loc != (model.Location{File: "doc.md", Line: 1}) {
				t.Errorf("Loc = %+v, want doc.md:1 (the Alpha heading)", f.Loc)
			}
			if f.Ref != nil {
				t.Errorf("Ref = %+v, want nil", f.Ref)
			}
			want := `section "Alpha" last edited 2024-01-01; since then a.go: 3 commits (latest 2024-01-04)`
			if f.Message != want {
				t.Errorf("Message = %q\nwant %q", f.Message, want)
			}
			wantFP := model.Fingerprint(model.RuleStaleSection, "doc.md", markdown.Slug("Alpha"))
			if f.Fingerprint != wantFP {
				t.Errorf("Fingerprint = %q, want %q", f.Fingerprint, wantFP)
			}
			if f.Data["section"] != "Alpha" {
				t.Errorf("Data[section] = %v, want Alpha", f.Data["section"])
			}
			if want := t0.UTC().Format(time.RFC3339); f.Data["edited"] != want {
				t.Errorf("Data[edited] = %v, want %v", f.Data["edited"], want)
			}
			files, _ := f.Data["files"].([]map[string]any)
			if len(files) != 1 || files[0]["file"] != "a.go" || files[0]["commits"] != 3 {
				t.Errorf("Data[files] = %v, want one entry for a.go with 3 commits", f.Data["files"])
			}
		})
	}
}

func TestAnalyzeLeadingSection(t *testing.T) {
	dir := fixture(t, "a.go")
	repo := open(t, dir)
	doc := markdown.Parse("preamble.md", []byte(preambleContent))
	refs := []ResolvedRef{{
		Ref:  model.Reference{Kind: model.KindPath, Text: "a.go", Norm: "a.go", Loc: model.Location{File: "preamble.md", Line: 1}},
		File: "a.go",
	}}

	got, err := Analyze(repo, doc, "preamble.md", refs, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Analyze() = %+v, want one finding", got)
	}
	if got[0].Loc.Line != 1 {
		t.Errorf("Loc.Line = %d, want 1 for the section before the first heading", got[0].Loc.Line)
	}
	if !strings.HasPrefix(got[0].Message, `section "" last edited 2024-01-01`) {
		t.Errorf("Message = %q, want an empty section name", got[0].Message)
	}
	want := model.Fingerprint(model.RuleStaleSection, "preamble.md", "")
	if got[0].Fingerprint != want {
		t.Errorf("Fingerprint = %q, want %q", got[0].Fingerprint, want)
	}
}

func TestAnalyzeSkipsUncommittedSection(t *testing.T) {
	dir := fixture(t, "a.go", "b.go")
	// An uncommitted line inside the Beta section: it is being edited now.
	content := docContent + "Still writing about `b.go`.\n"
	writeFile(t, dir, "doc.md", content)

	repo := open(t, dir)
	doc := markdown.Parse("doc.md", []byte(content))

	got, err := Analyze(repo, doc, "doc.md", docRefs(), Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Analyze() = %+v, want only the Alpha finding", got)
	}
	if got[0].Data["section"] != "Alpha" {
		t.Errorf("Data[section] = %v, want Alpha (Beta is being edited)", got[0].Data["section"])
	}
}

func TestAnalyzeUntrackedDocument(t *testing.T) {
	dir := fixture(t, "a.go")
	writeFile(t, dir, "extra.md", docContent)
	repo := open(t, dir)
	doc := markdown.Parse("extra.md", []byte(docContent))

	got, err := Analyze(repo, doc, "extra.md", docRefs(), Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got != nil {
		t.Errorf("Analyze() = %+v, want nil for an untracked document", got)
	}
}

func TestAnalyzeEdgeCases(t *testing.T) {
	dir := fixture(t, "a.go")
	repo := open(t, dir)
	doc := markdown.Parse("doc.md", []byte(docContent))

	t.Run("nil repo", func(t *testing.T) {
		if _, err := Analyze(nil, doc, "doc.md", docRefs(), Options{}); err == nil {
			t.Error("Analyze(nil repo) = nil error, want an error")
		}
	})
	t.Run("nil document", func(t *testing.T) {
		got, err := Analyze(repo, nil, "doc.md", docRefs(), Options{})
		if err != nil || got != nil {
			t.Errorf("Analyze(nil doc) = %v, %v, want nil, nil", got, err)
		}
	})
	t.Run("no references", func(t *testing.T) {
		got, err := Analyze(repo, doc, "doc.md", nil, Options{})
		if err != nil || got != nil {
			t.Errorf("Analyze(no refs) = %v, %v, want nil, nil", got, err)
		}
	})
	t.Run("references without a file", func(t *testing.T) {
		refs := []ResolvedRef{{Ref: ref("a.go", 3)}}
		got, err := Analyze(repo, doc, "doc.md", refs, Options{})
		if err != nil || got != nil {
			t.Errorf("Analyze(unresolved refs) = %v, %v, want nil, nil", got, err)
		}
	})
}

func TestSections(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []section
	}{
		{"empty", "", nil},
		{"no headings", "a\nb\n", []section{{line: 1, start: 1, end: 2}}},
		{
			name:    "leading section then headings",
			content: "intro\n\n# A\n\ntext\n\n## B\n\ntext\n",
			want: []section{
				{name: "", line: 1, start: 1, end: 2},
				{name: "A", line: 3, start: 3, end: 6},
				{name: "B", line: 7, start: 7, end: 9},
			},
		},
		{
			name:    "heading on the first line",
			content: "# A\n\n# B\n",
			want: []section{
				{name: "A", line: 1, start: 1, end: 2},
				{name: "B", line: 3, start: 3, end: 3},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sections(markdown.Parse("x.md", []byte(tt.content)))
			if len(got) != len(tt.want) {
				t.Fatalf("sections() = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("sections()[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSectionTime(t *testing.T) {
	times := []time.Time{{}, t0, t2, {}, t1}
	tests := []struct {
		name       string
		start, end int
		want       time.Time
		ok         bool
	}{
		{"newest wins", 1, 2, t2, true},
		{"single line", 1, 1, t0, true},
		{"uncommitted line", 1, 3, time.Time{}, false},
		{"past the blame output", 4, 5, time.Time{}, false},
		{"empty range", 3, 2, time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := sectionTime(times, tt.start, tt.end)
			if ok != tt.ok || !got.Equal(tt.want) {
				t.Errorf("sectionTime() = %v, %v, want %v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
