package stale

import (
	"strings"
	"testing"
	"time"

	"docrot/internal/gitx"
	"docrot/internal/markdown"
	"docrot/internal/model"
)

// TestStaleSymbol: a section names Run(); Run's body changes in three
// commits after the section was written → stale-symbol, and the coarser
// stale-section for the same section is not repeated. A section whose
// symbol did not change keeps the file-level verdict.
func TestStaleSymbol(t *testing.T) {
	dir := fixtureSymbol(t)
	repo := open(t, dir)
	doc := markdown.Parse("doc.md", []byte(symDoc))
	span := func(name string, start, end int) *model.SymbolSpan {
		return &model.SymbolSpan{Qualified: name, Kind: model.KindGoSymbol, File: "a.go", DeclLine: start, BodyStart: start, BodyEnd: end}
	}
	refs := []ResolvedRef{
		{Ref: symRef("Run()", "Run", 3), File: "a.go", Span: span("Run", 3, 6)},
		{Ref: symRef("Stop()", "Stop", 7), File: "a.go", Span: span("Stop", 8, 10)},
	}
	got, err := Analyze(repo, doc, "doc.md", refs, Options{MinChurn: 3})
	if err != nil {
		t.Fatal(err)
	}
	var rules []string
	for _, f := range got {
		rules = append(rules, f.Rule+"@"+itoaLine(f.Loc.Line))
	}
	// Alpha: the symbol-level finding replaces the section-level one;
	// Beta: Stop's body never changed, so the file-level churn of a.go is
	// still reported for that section
	if strings.Join(rules, ",") != "stale-symbol@3,stale-section@5" {
		t.Fatalf("findings = %v (%+v)", rules, got)
	}
	f := got[0]
	if f.Data["commits"] != 3 || f.Ref == nil || f.Ref.Norm != "Run" {
		t.Errorf("finding = %+v", f)
	}
	if !strings.Contains(f.Message, "`Run()` (a.go:3) changed in 3 commits") {
		t.Errorf("message = %q", f.Message)
	}
}

const symDoc = "# Alpha\n\nSee `Run()`.\n\n# Beta\n\nSee `Stop()`.\n"

// a.go at t0: Run on lines 3-6, Stop on lines 8-10.
const aGo0 = "package main\n\nfunc Run() {\n\tx := 1\n\t_ = x\n}\n\nfunc Stop() {\n\t_ = 0\n}\n"

func fixtureSymbol(t *testing.T) string {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	git(t, dir, time.Time{}, "-c", "init.defaultBranch=main", "init", "-q", ".")
	for _, kv := range [][2]string{{"user.name", "docrot test"}, {"user.email", "docrot@example.invalid"}, {"commit.gpgsign", "false"}, {"core.autocrlf", "false"}} {
		git(t, dir, time.Time{}, "config", kv[0], kv[1])
	}
	writeFile(t, dir, "doc.md", symDoc)
	writeFile(t, dir, "a.go", aGo0)
	commit(t, dir, t0, "symbols")
	// three commits, each rewriting a different line of Run's body (blame
	// attributes a line to its last change, so the test spreads the edits)
	versions := []string{
		"package main\n\nfunc Run() {\n\tx := 2\n\t_ = x\n}\n\nfunc Stop() {\n\t_ = 0\n}\n",
		"package main\n\nfunc Run() {\n\tx := 2\n\t_ = x + 0\n}\n\nfunc Stop() {\n\t_ = 0\n}\n",
		"package main\n\nfunc Run() { // run\n\tx := 2\n\t_ = x + 0\n}\n\nfunc Stop() {\n\t_ = 0\n}\n",
	}
	for i, at := range []time.Time{t1, t2, t3} {
		writeFile(t, dir, "a.go", versions[i])
		commit(t, dir, at, "churn run")
	}
	return dir
}

func symRef(text, norm string, line int) model.Reference {
	return model.Reference{Kind: model.KindGoSymbol, Text: text, Norm: norm, Confidence: model.High, Loc: model.Location{File: "doc.md", Line: line, Col: 5}}
}

func itoaLine(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
