package gitx

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Fixed commit times for the fixture repository.
var (
	t1 = time.Unix(1704067200, 0) // 2024-01-01T00:00:00Z, adds a.md and b.go
	t2 = time.Unix(1706745600, 0) // 2024-02-01T00:00:00Z, edits a.md line 2
	t3 = time.Unix(1709251200, 0) // 2024-03-01T00:00:00Z, renames b.go -> c.go
)

// requireGit skips the test when git is not usable.
func requireGit(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("git not on PATH")
	}
}

// git runs a git command in dir, failing the test on error. When at is
// non-zero it is used as both author and committer date.
func git(t *testing.T, dir string, at time.Time, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	if !at.IsZero() {
		stamp := at.UTC().Format("2006-01-02T15:04:05+00:00")
		cmd.Env = append(cmd.Env, "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// write creates dir/rel with the given content.
func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture builds a temp repository with three commits:
//
//	t1: a.md (3 lines), b.go, sub/d.md
//	t2: a.md line 2 edited
//	t3: b.go renamed to c.go, sub/d.md renamed to sub/e.md
//
// It also leaves an untracked file untracked.md behind.
func fixture(t *testing.T) string {
	t.Helper()
	requireGit(t)
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

	write(t, dir, "a.md", "line one\nline two\nline three\n")
	write(t, dir, "b.go", "package b\n")
	write(t, dir, "sub/d.md", "sub doc\n")
	git(t, dir, t1, "add", "-A")
	git(t, dir, t1, "commit", "-q", "-m", "first")

	write(t, dir, "a.md", "line one\nline two edited\nline three\n")
	git(t, dir, t2, "commit", "-q", "-am", "edit a.md")

	git(t, dir, t3, "mv", "b.go", "c.go")
	git(t, dir, t3, "mv", "sub/d.md", "sub/e.md")
	git(t, dir, t3, "commit", "-q", "-m", "rename b.go")

	write(t, dir, "untracked.md", "not added\n")
	return dir
}

func open(t *testing.T, root string) *Repo {
	t.Helper()
	r, err := Open(root, Options{})
	if err != nil {
		t.Fatalf("Open(%s): %v", root, err)
	}
	return r
}

func TestOpen(t *testing.T) {
	requireGit(t)
	repo := fixture(t)
	outside := t.TempDir()

	tests := []struct {
		name    string
		root    string
		wantErr bool
		prefix  string
	}{
		{name: "top level", root: repo, prefix: ""},
		{name: "subdirectory", root: filepath.Join(repo, "sub"), prefix: "sub/"},
		{name: "not a repo", root: outside, wantErr: true},
		{name: "missing directory", root: filepath.Join(outside, "nope"), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Open(tc.root, Options{})
			if tc.wantErr {
				if !errors.Is(err, ErrUnavailable) {
					t.Fatalf("Open: err = %v, want ErrUnavailable", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if got := r.Prefix(); got != tc.prefix {
				t.Errorf("Prefix = %q, want %q", got, tc.prefix)
			}
			wantTop := strings.ToLower(strings.ReplaceAll(mustEvalAbs(t, repo), "\\", "/"))
			if got := strings.ToLower(r.TopLevel()); got != wantTop {
				t.Errorf("TopLevel = %q, want %q", got, wantTop)
			}
		})
	}
}

func mustEvalAbs(t *testing.T, p string) string {
	t.Helper()
	q, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	a, err := filepath.Abs(q)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestHeadAndIsTracked(t *testing.T) {
	r := open(t, fixture(t))

	head, err := r.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if len(head) < 4 || len(head) > 40 || !isHex(head) {
		t.Errorf("Head = %q, want a short hash", head)
	}

	tests := []struct {
		rel  string
		want bool
	}{
		{"a.md", true},
		{"./a.md", true},
		{"c.go", true},
		{"sub/e.md", true},
		{"b.go", false},         // renamed away
		{"untracked.md", false}, // never added
		{"nope.md", false},
	}
	for _, tc := range tests {
		t.Run(tc.rel, func(t *testing.T) {
			if got := r.IsTracked(tc.rel); got != tc.want {
				t.Errorf("IsTracked(%q) = %v, want %v", tc.rel, got, tc.want)
			}
		})
	}
}

func TestLastCommitTime(t *testing.T) {
	r := open(t, fixture(t))

	tests := []struct {
		rel    string
		want   time.Time
		wantOK bool
	}{
		{rel: "a.md", want: t2, wantOK: true},
		{rel: "c.go", want: t3, wantOK: true},
		{rel: "sub/e.md", want: t3, wantOK: true},
		{rel: "untracked.md", wantOK: false},
		{rel: "nope.md", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.rel, func(t *testing.T) {
			got, ok, err := r.LastCommitTime(tc.rel)
			if err != nil {
				t.Fatalf("LastCommitTime: %v", err)
			}
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (t = %v)", ok, tc.wantOK, got)
			}
			if ok && !got.Equal(tc.want) {
				t.Errorf("time = %v, want %v", got.UTC(), tc.want.UTC())
			}
		})
	}
}

func TestCommitsSince(t *testing.T) {
	r := open(t, fixture(t))

	tests := []struct {
		name     string
		rel      string
		since    time.Time
		want     []time.Time
		subjects []string
	}{
		{
			name:     "all of a.md",
			rel:      "a.md",
			since:    time.Time{},
			want:     []time.Time{t2, t1},
			subjects: []string{"edit a.md", "first"},
		},
		{
			name:     "after first commit",
			rel:      "a.md",
			since:    t1,
			want:     []time.Time{t2},
			subjects: []string{"edit a.md"},
		},
		{
			name:  "boundary: exactly equal is excluded",
			rel:   "a.md",
			since: t2,
			want:  nil,
		},
		{
			name:  "one second before the boundary",
			rel:   "a.md",
			since: t2.Add(-time.Second),
			want:  []time.Time{t2},
		},
		{
			name:  "renamed file is not followed",
			rel:   "c.go",
			since: time.Time{},
			want:  []time.Time{t3},
		},
		{
			name:  "untracked",
			rel:   "untracked.md",
			since: time.Time{},
			want:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.CommitsSince(tc.rel, tc.since)
			if err != nil {
				t.Fatalf("CommitsSince: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d commits %+v, want %d", len(got), got, len(tc.want))
			}
			for i, c := range got {
				if !c.Time.Equal(tc.want[i]) {
					t.Errorf("commit %d time = %v, want %v", i, c.Time.UTC(), tc.want[i].UTC())
				}
				if c.Hash == "" || !isHex(c.Hash) {
					t.Errorf("commit %d hash = %q", i, c.Hash)
				}
				if i < len(tc.subjects) && c.Subject != tc.subjects[i] {
					t.Errorf("commit %d subject = %q, want %q", i, c.Subject, tc.subjects[i])
				}
			}
		})
	}
}

func TestBlameLineTimes(t *testing.T) {
	dir := fixture(t)
	r := open(t, dir)

	tests := []struct {
		name string
		rel  string
		want []time.Time // index 0 unused
	}{
		{name: "edited line", rel: "a.md", want: []time.Time{{}, t1, t2, t1}},
		{name: "single line", rel: "c.go", want: []time.Time{{}, t1}},
		{name: "in subdirectory", rel: "sub/e.md", want: []time.Time{{}, t1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.BlameLineTimes(tc.rel)
			if err != nil {
				t.Fatalf("BlameLineTimes: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d entries %v, want %d", len(got), got, len(tc.want))
			}
			for i := 1; i < len(got); i++ {
				if !got[i].Equal(tc.want[i]) {
					t.Errorf("line %d = %v, want %v", i, got[i].UTC(), tc.want[i].UTC())
				}
			}
		})
	}

	t.Run("untracked", func(t *testing.T) {
		if _, err := r.BlameLineTimes("untracked.md"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("err = %v, want ErrUnavailable", err)
		}
	})

	t.Run("uncommitted line", func(t *testing.T) {
		write(t, dir, "a.md", "line one\nline two edited\nline three\nbrand new\n")
		fresh := open(t, dir) // a fresh Repo: the first one cached the old blame
		got, err := fresh.BlameLineTimes("a.md")
		if err != nil {
			t.Fatalf("BlameLineTimes: %v", err)
		}
		if len(got) != 5 {
			t.Fatalf("got %d entries %v, want 5", len(got), got)
		}
		if !got[2].Equal(t2) {
			t.Errorf("line 2 = %v, want %v", got[2].UTC(), t2.UTC())
		}
		if !got[4].IsZero() {
			t.Errorf("line 4 = %v, want the zero time", got[4].UTC())
		}
	})
}

func TestRenames(t *testing.T) {
	repo := fixture(t)

	tests := []struct {
		name string
		root string
		want map[string]string
	}{
		{
			name: "from top level",
			root: repo,
			want: map[string]string{"b.go": "c.go", "sub/d.md": "sub/e.md"},
		},
		{
			name: "from a subdirectory root",
			root: filepath.Join(repo, "sub"),
			want: map[string]string{"d.md": "e.md"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := open(t, tc.root).Renames()
			if err != nil {
				t.Fatalf("Renames: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("Renames()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// TestRenameChain checks that old->mid->new collapses to old->new.
func TestRenameChain(t *testing.T) {
	requireGit(t)
	dir := fixture(t)
	git(t, dir, t3.Add(24*time.Hour), "mv", "c.go", "final.go")
	git(t, dir, t3.Add(24*time.Hour), "commit", "-q", "-m", "rename again")

	got, err := open(t, dir).Renames()
	if err != nil {
		t.Fatalf("Renames: %v", err)
	}
	want := map[string]string{
		"b.go":     "final.go",
		"c.go":     "final.go",
		"sub/d.md": "sub/e.md",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Renames()[%q] = %q, want %q", k, got[k], v)
		}
	}
}

// TestSubdirectoryRoot checks that a root below the work tree top level
// translates paths correctly in both directions.
func TestSubdirectoryRoot(t *testing.T) {
	repo := fixture(t)
	r := open(t, filepath.Join(repo, "sub"))

	if !r.IsTracked("e.md") {
		t.Error("IsTracked(e.md) = false, want true")
	}
	if r.IsTracked("a.md") {
		t.Error("IsTracked(a.md) = true, want false (a.md lives above the root)")
	}
	got, ok, err := r.LastCommitTime("e.md")
	if err != nil || !ok {
		t.Fatalf("LastCommitTime(e.md) = %v, %v, %v", got, ok, err)
	}
	if !got.Equal(t3) {
		t.Errorf("LastCommitTime(e.md) = %v, want %v", got.UTC(), t3.UTC())
	}
	blame, err := r.BlameLineTimes("e.md")
	if err != nil {
		t.Fatalf("BlameLineTimes(e.md): %v", err)
	}
	if len(blame) != 2 || !blame[1].Equal(t1) {
		t.Errorf("BlameLineTimes(e.md) = %v, want [zero %v]", blame, t1.UTC())
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		rel    string
		want   string
	}{
		{name: "plain", prefix: "", rel: "a.md", want: "a.md"},
		{name: "dot slash", prefix: "", rel: "./docs/a.md", want: "docs/a.md"},
		{name: "backslashes", prefix: "", rel: `docs\a.md`, want: "docs/a.md"},
		{name: "empty means root", prefix: "", rel: "", want: "."},
		{name: "prefixed", prefix: "sub/", rel: "a.md", want: "sub/a.md"},
		{name: "prefixed backslashes", prefix: "sub/", rel: `x\y.md`, want: "sub/x/y.md"},
		{name: "prefixed empty", prefix: "sub/", rel: ".", want: "sub"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Repo{prefix: tc.prefix}
			if got := r.resolve(tc.rel); got != tc.want {
				t.Errorf("resolve(%q) = %q, want %q", tc.rel, got, tc.want)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "lf", in: "a\nb\n", want: []string{"a", "b"}},
		{name: "crlf", in: "a\r\nb\r\n", want: []string{"a", "b"}},
		{name: "no trailing newline", in: "a\r\nb", want: []string{"a", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitLines([]byte(tc.in))
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			}
		})
	}
}

// TestCacheAndDedup checks that repeated and concurrent queries for the same
// file spawn exactly one git process.
func TestCacheAndDedup(t *testing.T) {
	r := open(t, fixture(t))

	before := r.Stats()
	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, _, err := r.LastCommitTime("a.md"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	after := r.Stats()
	if got := after.Commands - before.Commands; got != 1 {
		t.Errorf("ran %d git commands for %d identical queries, want 1", got, n)
	}
	if got := after.CacheHits - before.CacheHits; got != n-1 {
		t.Errorf("cache hits = %d, want %d", got, n-1)
	}
	if after.Duration <= 0 {
		t.Errorf("Stats().Duration = %v, want > 0", after.Duration)
	}

	// Different files still run their own command.
	if _, _, err := r.LastCommitTime("c.go"); err != nil {
		t.Fatal(err)
	}
	if got := r.Stats().Commands - after.Commands; got != 1 {
		t.Errorf("ran %d git commands for a new file, want 1", got)
	}
}

// TestConcurrencyLimit checks that no more than Options.Concurrency git
// processes run at the same time.
func TestConcurrencyLimit(t *testing.T) {
	requireGit(t)
	r, err := Open(fixture(t), Options{Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if cap(r.sem) != 2 {
		t.Fatalf("semaphore capacity = %d, want 2", cap(r.sem))
	}

	files := []string{"a.md", "c.go", "sub/e.md", "nope.md", "untracked.md"}
	before := r.Stats().Commands // Open itself already ran rev-parse
	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			r.IsTracked(f)
			if _, _, err := r.LastCommitTime(f); err != nil {
				t.Error(err)
			}
		}(f)
	}
	wg.Wait()
	if got := r.Stats().Commands - before; got != 2*len(files) {
		t.Errorf("Commands = %d, want %d", got, 2*len(files))
	}
}

func TestOptionsNormalized(t *testing.T) {
	tests := []struct {
		name string
		in   Options
		want Options
	}{
		{name: "zero", in: Options{}, want: Options{Timeout: DefaultTimeout, Concurrency: DefaultConcurrency}},
		{name: "negative concurrency", in: Options{Concurrency: -3}, want: Options{Timeout: DefaultTimeout, Concurrency: 1}},
		{name: "explicit", in: Options{Timeout: time.Second, Concurrency: 9}, want: Options{Timeout: time.Second, Concurrency: 9}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.normalized(); got != tc.want {
				t.Errorf("normalized() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestIgnored(t *testing.T) {
	if !Available() {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	mustGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2024-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2024-01-01T00:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mustGit("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("dist/\n*.exe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Ignored([]string{"dist/app", "tool.exe", "README.md", "src/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !got["dist/app"] || !got["tool.exe"] {
		t.Errorf("expected dist/app and tool.exe to be ignored: %v", got)
	}
	if got["README.md"] || got["src/main.go"] {
		t.Errorf("unexpected ignores: %v", got)
	}
	none, err := r.Ignored([]string{"README.md"})
	if err != nil || len(none) != 0 {
		t.Errorf("no ignores: %v %v", none, err)
	}
}
