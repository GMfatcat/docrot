package engine

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"docrot/internal/config"
	"docrot/internal/gitx"
	"docrot/internal/model"
)

// gitDated runs git with a fixed author and committer date, so that commits
// made within one test are ordered in time the way the staleness rules read
// them.
func gitDated(t *testing.T, dir, date string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// TestStaleSkipsTranslation checks that commits to a document's own
// translation never make one of its sections stale (the pair rules cover
// that drift), while commits to any other referenced file still do.
func TestStaleSkipsTranslation(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	gitIn(t, root, "init", "-q", "-b", "main")
	writeIn(t, root, "go.mod", "module example.com/x\n\ngo 1.22\n")
	writeIn(t, root, "README.md", "# X\n\n[中文](README-zh.md) · English\n\n## Guide\n\nSee [the guide](docs/guide.md).\n")
	writeIn(t, root, "docs/guide.md", "# Guide\n\nv1\n")
	zh := func(day string) string {
		return "# X\n\n中文 · [English](README.md)\n\n## 指南\n\n見 [指南](docs/guide.md)。\n\n改 " + day + "\n"
	}
	writeIn(t, root, "README-zh.md", zh("01"))
	gitIn(t, root, "add", ".")
	gitDated(t, root, "2024-01-01T00:00:00Z", "commit", "-q", "-m", "base")
	// three later commits touch the translation and the guide; README.md is untouched
	for _, day := range []string{"02", "03", "04"} {
		writeIn(t, root, "README-zh.md", zh(day))
		writeIn(t, root, "docs/guide.md", "# Guide\n\nv"+day+"\n")
		gitIn(t, root, "add", ".")
		gitDated(t, root, "2024-01-"+day+"T00:00:00Z", "commit", "-q", "-m", "edit "+day)
	}

	run, err := Check(Options{Root: root, Config: config.Default(), NoOut: true, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var stale []string
	for _, f := range run.Report.Findings {
		if f.Rule == model.RuleStaleSection {
			stale = append(stale, f.Loc.File+":"+strconv.Itoa(f.Loc.Line))
		}
	}
	// the intro of README.md links only its translation: not stale; the
	// Guide section links docs/guide.md, which has three newer commits:
	// stale. README-zh.md was edited in the last commit: nothing newer.
	if got := strings.Join(stale, ","); got != "README.md:5" {
		t.Fatalf("stale-section findings = %q, want README.md:5 only", got)
	}
}
