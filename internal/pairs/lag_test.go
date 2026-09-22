package pairs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"docrot/internal/gitx"
	"docrot/internal/model"
)

// Fixed commit times for the fixture repository.
var (
	lagT0 = time.Unix(1704067200, 0) // 2024-01-01T00:00:00Z, adds both documents
	lagT1 = time.Unix(1704153600, 0) // 2024-01-02T00:00:00Z, edits README.md
	lagT2 = time.Unix(1704240000, 0) // 2024-01-03T00:00:00Z, edits README.md again
)

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

// lagFixture builds a repository whose README.md has two commits after
// README-zh.md was last touched.
func lagFixture(t *testing.T) string {
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

	writeFile(t, dir, "README.md", "# Title\n")
	writeFile(t, dir, "README-zh.md", "# 標題\n")
	git(t, dir, time.Time{}, "add", "-A")
	git(t, dir, lagT0, "commit", "-q", "-m", "initial")

	writeFile(t, dir, "README.md", "# Title\n\nusage\n")
	git(t, dir, time.Time{}, "add", "-A")
	git(t, dir, lagT1, "commit", "-q", "-m", "update usage")

	writeFile(t, dir, "README.md", "# Title\n\nusage!\n")
	git(t, dir, time.Time{}, "add", "-A")
	git(t, dir, lagT2, "commit", "-q", "-m", "fix typo")
	return dir
}

func TestComparePairLag(t *testing.T) {
	dir := lagFixture(t)
	repo, err := gitx.Open(dir, gitx.Options{})
	if err != nil {
		t.Fatalf("gitx.Open: %v", err)
	}

	// Structurally identical documents, so only pair-lag can fire.
	src := doc("README.md", "# A")
	tr := doc("README-zh.md", "# A")

	t.Run("without a repo the rule is skipped", func(t *testing.T) {
		if fs := Compare(readmePair, src, tr, Options{}); fs != nil {
			t.Errorf("Compare() = %+v, want nil", fs)
		}
	})

	t.Run("with a repo", func(t *testing.T) {
		fs := Compare(readmePair, src, tr, Options{Repo: repo})
		if len(fs) != 1 {
			t.Fatalf("Compare() = %+v, want one finding", fs)
		}
		f := fs[0]
		if f.Rule != model.RulePairLag || f.Severity != model.SevWarning {
			t.Errorf("rule/severity = %q/%q, want %q/%q",
				f.Rule, f.Severity, model.RulePairLag, model.SevWarning)
		}
		if f.Loc != (model.Location{File: "README-zh.md", Line: 1}) {
			t.Errorf("Loc = %+v, want README-zh.md:1", f.Loc)
		}
		const prefix = "2 commits to README.md since README-zh.md last changed ("
		if !strings.HasPrefix(f.Message, prefix) {
			t.Errorf("Message = %q, want prefix %q", f.Message, prefix)
		}
		for _, subject := range []string{`"fix typo"`, `"update usage"`} {
			if !strings.Contains(f.Message, subject) {
				t.Errorf("Message = %q, want it to mention %s", f.Message, subject)
			}
		}
		if i := strings.Index(f.Message, `"fix typo"`); i > strings.Index(f.Message, `"update usage"`) {
			t.Errorf("Message = %q, want the newest commit first", f.Message)
		}
		if want := lagT0.UTC().Format(time.RFC3339); f.Data["since"] != want {
			t.Errorf("Data[since] = %v, want %v", f.Data["since"], want)
		}
		commits, _ := f.Data["commits"].([]map[string]any)
		if len(commits) != 2 {
			t.Fatalf("Data[commits] = %v, want two entries", f.Data["commits"])
		}
		if commits[0]["subject"] != "fix typo" {
			t.Errorf("Data[commits][0] = %v, want the newest commit", commits[0])
		}
		if want := lagT2.UTC().Format(time.RFC3339); commits[0]["time"] != want {
			t.Errorf("Data[commits][0][time] = %v, want %v", commits[0]["time"], want)
		}
		if f.Fingerprint != model.Fingerprint(model.RulePairLag, "README-zh.md", "lag") {
			t.Errorf("Fingerprint = %q, want the lag fingerprint", f.Fingerprint)
		}
	})

	t.Run("no lag when the translation is newer", func(t *testing.T) {
		p := Pair{Source: "README-zh.md", Translation: "README.md"}
		if fs := Compare(p, tr, src, Options{Repo: repo}); fs != nil {
			t.Errorf("Compare() = %+v, want nil", fs)
		}
	})

	t.Run("untracked translation is skipped", func(t *testing.T) {
		p := Pair{Source: "README.md", Translation: "missing.md"}
		if fs := Compare(p, src, doc("missing.md", "# A"), Options{Repo: repo}); fs != nil {
			t.Errorf("Compare() = %+v, want nil", fs)
		}
	})
}
