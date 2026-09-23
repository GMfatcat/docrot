package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"docrot/internal/config"
	"docrot/internal/gitx"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeIn(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestChangedMode checks that --changed restricts the run to modified and
// untracked documents while every document still feeds the anchor index,
// and that --since adds the branch's commits.
func TestChangedMode(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	gitIn(t, root, "init", "-q", "-b", "main")
	writeIn(t, root, "go.mod", "module example.com/x\n\ngo 1.22\n")
	writeIn(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeIn(t, root, "README.md", "# X\n\n## Install\n\nSee [guide](docs/guide.md#setup) and `missing.go`.\n")
	writeIn(t, root, "docs/guide.md", "# Guide\n\n## Setup\n\nBack to [install](../README.md#install).\n")
	writeIn(t, root, "docs/other.md", "# Other\n\nMentions `nothere/file.go`.\n")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "base")

	cfg := config.Default()
	base := Options{Root: root, Config: cfg, NoOut: true, Version: "test"}

	// nothing changed: nothing checked, nothing found
	run, err := Check(withChanged(base, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Docs) != 0 || len(run.AllDocs) != 3 || len(run.Report.Findings) != 0 {
		t.Fatalf("clean tree: docs=%v all=%v findings=%d", run.Docs, run.AllDocs, len(run.Report.Findings))
	}
	if run.Report.Summary.Extra["changed"] != "0 of 3 docs" {
		t.Errorf("extra = %v", run.Report.Summary.Extra)
	}

	// modify one doc (work tree) and add an untracked one
	writeIn(t, root, "docs/guide.md", "# Guide\n\n## Setup\n\nBack to [install](../README.md#install) and [nope](../README.md#nope).\n")
	writeIn(t, root, "docs/new.md", "# New\n\nSee `main.go` and `gone.go`.\n")
	run, err = Check(withChanged(base, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(run.Docs, ","); got != "docs/guide.md,docs/new.md" {
		t.Fatalf("changed docs = %q", got)
	}
	var files []string
	for _, f := range run.Report.Findings {
		files = append(files, f.Loc.File+":"+f.Rule)
	}
	got := strings.Join(files, ",")
	if !strings.Contains(got, "docs/guide.md:broken-anchor") {
		t.Errorf("the cross-document anchor in the modified doc was not checked: %s", got)
	}
	if strings.Contains(got, "README.md") || strings.Contains(got, "docs/other.md") {
		t.Errorf("unchanged documents were checked: %s", got)
	}
	// the untracked doc's info finding about a bare file name exists but is info
	found := false
	for _, f := range run.Report.Findings {
		if f.Loc.File == "docs/new.md" && f.Ref != nil && f.Ref.Text == "gone.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("untracked document not checked: %s", got)
	}

	// --since: a branch commit touching README.md, work tree clean
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "wip")
	gitIn(t, root, "checkout", "-q", "-b", "feature")
	writeIn(t, root, "README.md", "# X\n\n## Install\n\nSee [guide](docs/guide.md#setup) and `missing.go` and `also.go`.\n")
	gitIn(t, root, "commit", "-q", "-am", "feature")
	run, err = Check(withChanged(base, "main"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(run.Docs, ","); got != "README.md" {
		t.Fatalf("since main: docs = %q", got)
	}
	run, err = Check(withChanged(base, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Docs) != 0 {
		t.Errorf("without --since a committed change is not 'changed': %v", run.Docs)
	}

	// --no-git and --changed do not mix
	bad := withChanged(base, "")
	bad.NoGit = true
	if _, err := Check(bad); err == nil {
		t.Error("expected an error for --changed without git")
	}
}

func withChanged(o Options, since string) Options {
	o.Changed = true
	o.ChangedBase = since
	return o
}
