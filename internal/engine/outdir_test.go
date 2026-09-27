package engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"docrot/internal/config"
)

// tinyRepo writes a one-document, one-package repository into a temporary
// directory and returns its root. testdata/fixture is deliberately not used
// here: this test writes reports into the root it is given.
func tinyRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/tiny\n\ngo 1.26\n")
	write("app/server.go", `package app

// Serve starts the server.
func Serve() {}
`)
	write("README.md", `# tiny

Start it with `+"`app.Serve`"+`, defined in `+"`app/server.go`"+`.

Some guides still point at `+"`app/gone.go`"+` as the entry point.
`)
	return root
}

// checkTiny runs Check over the repository with the given output directory.
func checkTiny(t *testing.T, root, outDir string) *Run {
	t.Helper()
	run, err := Check(Options{
		Root:    root,
		Config:  config.Default(),
		NoGit:   true,
		OutDir:  outDir,
		Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestCheckWritesOutDir(t *testing.T) {
	root := tinyRepo(t)
	run := checkTiny(t, root, config.DefaultOutDir)

	want := []string{".docrot/report.html", ".docrot/report.md", ".docrot/report.json", ".docrot/report.txt"}
	if !slices.Equal(run.Written, want) {
		t.Errorf("Written = %v, want %v", run.Written, want)
	}
	if len(run.Warnings) > 0 {
		t.Errorf("writing the out dir warned: %v", run.Warnings)
	}

	// The four reports plus a .gitignore that hides the whole directory.
	dir := filepath.Join(root, config.DefaultOutDir)
	sizes := map[string]int64{}
	for _, name := range append(slices.Clone(OutFiles), ".gitignore") {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if st.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
		sizes[name] = st.Size()
	}
	if got := readFile(t, filepath.Join(dir, ".gitignore")); got != "*\n" {
		t.Errorf(".gitignore = %q, want %q", got, "*\n")
	}
	// No temporary files are left behind by the atomic writes.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(OutFiles)+1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("out dir holds %v, want only the reports and .gitignore", names)
	}

	// report.md is the agent report, and carries info-level findings.
	md := readFile(t, filepath.Join(dir, "report.md"))
	for _, want := range []string{"# docrot report", "## How to read this", "## Fix checklist", "app/gone.go"} {
		if !strings.Contains(md, want) {
			t.Errorf("report.md is missing %q", want)
		}
	}
	// report.txt shows info too; the terminal default would hide it.
	txt := readFile(t, filepath.Join(dir, "report.txt"))
	if strings.Contains(txt, "info hidden") {
		t.Errorf("report.txt hid info findings:\n%s", txt)
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "report.html")), "<html") {
		t.Error("report.html is not HTML")
	}
	if !strings.HasPrefix(readFile(t, filepath.Join(dir, "report.json")), "{") {
		t.Error("report.json is not JSON")
	}

	// Yesterday's report must never be mistaken for a document.
	if slices.Contains(run.Docs, ".docrot/report.md") {
		t.Errorf("the out dir was discovered as documentation: %v", run.Docs)
	}

	// A second run rewrites the same files in place, rather than appending
	// to them or leaving a stale copy behind.
	again := checkTiny(t, root, config.DefaultOutDir)
	if !slices.Equal(again.Written, want) {
		t.Errorf("second run wrote %v, want %v", again.Written, want)
	}
	if slices.Contains(again.Docs, ".docrot/report.md") {
		t.Errorf("the second run discovered its own report: %v", again.Docs)
	}
	if !slices.Equal(run.Docs, again.Docs) {
		t.Errorf("docs changed between runs: %v then %v", run.Docs, again.Docs)
	}
	for name, size := range sizes {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == ".gitignore" && st.Size() != size {
			t.Errorf(".gitignore was rewritten (%d → %d bytes)", size, st.Size())
		}
	}
	// Everything but the duration in the summary is identical, so the two
	// Markdown reports differ in at most that one line.
	if a, b := readFile(t, filepath.Join(dir, "report.md")), md; differingLines(a, b) > 1 {
		t.Errorf("the second report.md is not a rewrite of the first:\n--- first\n%s\n--- second\n%s", b, a)
	}
}

// An output directory with an ordinary name is excluded from discovery just
// as a dot-directory is; the default excludes would hide ".docrot" anyway.
func TestCheckOutDirNeverBecomesDocs(t *testing.T) {
	root := tinyRepo(t)
	first := checkTiny(t, root, "docrot-out")
	if !slices.Contains(first.Written, "docrot-out/report.md") {
		t.Fatalf("Written = %v", first.Written)
	}
	second := checkTiny(t, root, "docrot-out")
	for _, d := range second.Docs {
		if strings.HasPrefix(d, "docrot-out/") {
			t.Errorf("the out dir was discovered as documentation: %v", second.Docs)
		}
	}
	if second.Report.Summary.Docs != first.Report.Summary.Docs {
		t.Errorf("doc count changed from %d to %d once reports existed",
			first.Report.Summary.Docs, second.Report.Summary.Docs)
	}
}

func TestCheckNoOut(t *testing.T) {
	root := tinyRepo(t)
	run, err := Check(Options{Root: root, Config: config.Default(), NoGit: true, OutDir: config.DefaultOutDir, NoOut: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Written) != 0 {
		t.Errorf("Written = %v, want nothing", run.Written)
	}
	if _, err := os.Stat(filepath.Join(root, config.DefaultOutDir)); !os.IsNotExist(err) {
		t.Errorf("--no-out still created the directory (%v)", err)
	}

	// An empty OutDir means the same thing.
	run, err = Check(Options{Root: root, Config: config.Default(), NoGit: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Written) != 0 {
		t.Errorf("OutDir=\"\": Written = %v, want nothing", run.Written)
	}
}

// A directory that cannot be written is a warning, never a failed run: the
// findings still have to reach stdout.
func TestCheckOutDirFailureIsAWarning(t *testing.T) {
	root := tinyRepo(t)
	// A regular file where the directory should go makes MkdirAll fail on
	// every platform.
	if err := os.WriteFile(filepath.Join(root, "blocked"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	run, err := Check(Options{Root: root, Config: config.Default(), NoGit: true, OutDir: "blocked", Stderr: &stderr})
	if err != nil {
		t.Fatalf("a broken out dir must not fail the run: %v", err)
	}
	if len(run.Written) != 0 {
		t.Errorf("Written = %v, want nothing", run.Written)
	}
	if len(run.Warnings) == 0 || !strings.Contains(stderr.String(), "out dir") {
		t.Errorf("no warning was reported: %v / %q", run.Warnings, stderr.String())
	}
	if run.Report == nil || run.Report.Summary.Docs == 0 {
		t.Error("the report should still be complete")
	}
}

func TestExcludeOutDir(t *testing.T) {
	tests := []struct {
		name    string
		exclude []string
		outDir  string
		want    []string
	}{
		{"appended", []string{"dist/**"}, ".docrot", []string{"dist/**", ".docrot/**"}},
		{"already there", []string{".docrot/**"}, ".docrot", []string{".docrot/**"}},
		{"normalised", nil, "./out/", []string{"out/**"}},
		{"disabled", []string{"dist/**"}, "", []string{"dist/**"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := slices.Clone(tt.exclude)
			got := excludeOutDir(in, tt.outDir)
			if !slices.Equal(got, tt.want) {
				t.Errorf("excludeOutDir(%v, %q) = %v, want %v", tt.exclude, tt.outDir, got, tt.want)
			}
			if !slices.Equal(in, tt.exclude) {
				t.Errorf("the caller's slice was modified: %v", in)
			}
		})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// differingLines counts the lines in which a and b disagree.
func differingLines(a, b string) int {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	n := 0
	for i := 0; i < len(la) || i < len(lb); i++ {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		if x != y {
			n++
		}
	}
	return n
}
