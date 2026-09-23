package comments

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"docrot/internal/gitx"
	"docrot/internal/model"
)

type fakeLookup struct{ syms, flags, files map[string]bool }

func (f fakeLookup) FileExists(r string) bool  { return f.files[r] }
func (f fakeLookup) IsGoPackage(n string) bool { return f.syms["pkg:"+n] }
func (f fakeLookup) SimilarPaths(rel string, n int) []string {
	var out []string
	for p := range f.files {
		if strings.HasSuffix(p, "/"+rel) || p == rel {
			out = append(out, p)
		}
	}
	return out
}
func (f fakeLookup) DirExists(r string) bool     { return false }
func (f fakeLookup) HasGoSymbol(q string) bool   { return f.syms[q] }
func (f fakeLookup) HasPySymbol(q string) bool   { return f.syms[q] }
func (f fakeLookup) HasOdinSymbol(q string) bool { return f.syms[q] }
func (f fakeLookup) HasFlag(n string) bool       { return f.flags[n] }
func (f fakeLookup) HasEnv(n string) bool        { return false }
func (f fakeLookup) HasJSONKey(d string) bool    { return false }
func (f fakeLookup) HasConfigKey(d string) bool  { return false }

func linesOf(s string) []string { return strings.Split(s, "\n") }

func TestMentions(t *testing.T) {
	src := `package x

// sendChunks streams every chunk from raw and returns the byte count.
// It retries per the retryBudget and honours --json for machine output;
// see docs/protocol.md and the Client.Push method. Plain words like
// streaming, count and honours are never checked. maxRetries is gone.
// Noise that must not be reported: errors.Is, logger.Info, golang.org,
// VERIFYING/COMPLETED/FAILED, env:"NAME", the default keyword, snake_case.
func (c *Client) sendChunks(src io.Reader) (int64, error) {
	budget := c.retryBudget
	_ = budget
	return 0, nil
}
`
	lines := linesOf(src)
	sp := model.SymbolSpan{
		Qualified: "x.Client.sendChunks", Kind: model.KindGoSymbol, File: "x.go",
		DocStart: 3, DocEnd: 8, DeclLine: 9, BodyStart: 9, BodyEnd: 13,
		Doc: []string{
			"sendChunks streams every chunk from raw and returns the byte count.",
			"It retries per the retryBudget and honours --json for machine output;",
			"see docs/protocol.md and the Client.Push method. Plain words like",
			"streaming, count and honours are never checked. maxRetries is gone.",
			"Noise that must not be reported: errors.Is, logger.Info, golang.org,",
			"VERIFYING/COMPLETED/FAILED, env:\"NAME\", the `default` keyword, snake_case.",
		},
		Params: []string{"c", "src"},
	}
	ix := fakeLookup{
		syms:  map[string]bool{"Client.Push": true, "x.Client.Push": true},
		flags: map[string]bool{"json": true},
		files: map[string]bool{"docs/protocol.md": true},
	}
	got := mentions(sp, lines, ix, Options{MentionSeverity: model.SevWarning})
	var names []string
	for _, f := range got {
		names = append(names, f.Data["mention"].(string))
		if f.Rule != model.RuleCommentMentions || f.Loc.Line != 3 || f.Severity != model.SevWarning {
			t.Errorf("bad finding %+v", f)
		}
	}
	want := []string{"maxRetries"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("mentions = %v, want %v", names, want)
	}
	// raw is a plain lowercase word: never a candidate, even though it is not a parameter any more
	for _, n := range names {
		if n == "raw" {
			t.Fatal("plain words must not be candidates")
		}
	}
}

func TestCandidates(t *testing.T) {
	got := candidates([]string{
		"Uses `cfg.Timeout` and `--out-dir`; see http://example.com/x_y for retryBudget.",
		"snake_case_name, CamelCase, a.b.c, ./scripts/x.py and plain words",
	})
	want := map[string]bool{"cfg.Timeout": true, "--out-dir": true, "snake_case_name": true, "a.b.c": true, "./scripts/x.py": true}
	for _, g := range got {
		if g == "retryBudget" {
			t.Fatal("tokens on a line with a URL must be skipped")
		}
		delete(want, g)
	}
	if len(want) != 0 {
		t.Fatalf("missing candidates %v in %v", want, got)
	}
}

func gitRun(t *testing.T, dir, date string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestStaleGoAndPython(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "2024-01-01T00:00:00Z", "init", "-q")
	gitRun(t, dir, "2024-01-01T00:00:00Z", "config", "commit.gpgsign", "false")
	goV1 := "package p\n\n// Run does the thing.\n// Carefully.\nfunc Run() int {\n\ta := 1\n\tb := 2\n\tc := 3\n\treturn a + b + c\n}\n"
	pyV1 := "def run(x):\n    \"\"\"Run the thing.\n\n    Carefully.\n    \"\"\"\n    a = 1\n    b = 2\n    c = 3\n    return a + b + c + x\n"
	write("p.go", goV1)
	write("m.py", pyV1)
	gitRun(t, dir, "2024-01-01T00:00:00Z", "add", ".")
	gitRun(t, dir, "2024-01-01T00:00:00Z", "commit", "-q", "-m", "v1")
	// two later commits touch the bodies only
	write("p.go", strings.Replace(goV1, "a := 1", "a := 10", 1))
	write("m.py", strings.Replace(pyV1, "a = 1", "a = 10", 1))
	gitRun(t, dir, "2024-02-01T00:00:00Z", "commit", "-qam", "v2")
	write("p.go", strings.Replace(strings.Replace(goV1, "a := 1", "a := 10", 1), "b := 2", "b := 20", 1))
	write("m.py", strings.Replace(strings.Replace(pyV1, "a = 1", "a = 10", 1), "b = 2", "b = 20", 1))
	gitRun(t, dir, "2024-03-01T00:00:00Z", "commit", "-qam", "v3")

	repo, err := gitx.Open(dir, gitx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	read := func(rel string) []string {
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return nil
		}
		return strings.Split(string(b), "\n")
	}
	spans := []model.SymbolSpan{
		{Qualified: "p.Run", Kind: model.KindGoSymbol, File: "p.go", DocStart: 3, DocEnd: 4, DeclLine: 5, BodyStart: 5, BodyEnd: 10, Doc: []string{"Run does the thing.", "Carefully."}},
		{Qualified: "m.run", Kind: model.KindPySym, File: "m.py", DocStart: 2, DocEnd: 5, DeclLine: 1, BodyStart: 1, BodyEnd: 9, Doc: []string{"Run the thing.", "", "Carefully."}, Params: []string{"x"}},
	}
	got := Analyze(repo, spans, fakeLookup{}, read, Options{})
	var stale []string
	for _, f := range got {
		if f.Rule == model.RuleStaleComment {
			stale = append(stale, f.Data["symbol"].(string))
			if f.Data["newerCommits"].(int) != 2 || f.Severity != model.SevInfo {
				t.Errorf("unexpected stale finding %+v", f)
			}
			if !strings.Contains(f.Message, "last edited 2024-01-01") || !strings.Contains(f.Message, "2 commits") {
				t.Errorf("message: %s", f.Message)
			}
		}
	}
	if strings.Join(stale, ",") != "m.run,p.Run" {
		t.Fatalf("stale symbols = %v (all findings: %+v)", stale, got)
	}
	// raising the threshold silences both
	if got := Analyze(repo, spans, fakeLookup{}, read, Options{MinChurn: 3, MinFrac: 0.9}); len(got) != 0 {
		t.Fatalf("expected no findings with strict thresholds, got %+v", got)
	}
	// no git → no stale findings, mentions still run
	if got := Analyze(nil, spans, fakeLookup{}, read, Options{}); len(got) != 0 {
		t.Fatalf("expected nothing without git, got %+v", got)
	}
}
