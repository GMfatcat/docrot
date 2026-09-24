package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// call runs the CLI and returns its exit code and both streams.
func call(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestExitCodes(t *testing.T) {
	fx := fixture(t)
	cases := []struct {
		name string
		args []string
		code int
		want string // substring of stdout+stderr
	}{
		{"no arguments is a usage error", nil, exitUsage, "Usage:"},
		{"unknown command", []string{"frobnicate"}, exitUsage, "unknown command"},
		{"help exits 0", []string{"help"}, exitOK, "docrot check"},
		{"help opens with the banner", []string{"help"}, exitOK, `\/__,_ / \/___/  \/____/ \/_/ \/___/      \/__/`},
		{"version stays a single plain line", []string{"version"}, exitOK, "docrot " + release + "\n"},
		{"command help exits 0", []string{"check", "-h"}, exitOK, "-fail-on"},
		{"version", []string{"version"}, exitOK, "docrot " + release},
		{"fixture has seeded findings", []string{"check", fx, "--no-git", "--no-out"}, exitFindings, "missing-symbol"},
		{"fail-on none never fails", []string{"check", fx, "--no-git", "--no-out", "--fail-on", "none"}, exitOK, "errors,"},
		{"quiet prints the summary only", []string{"check", fx, "--no-git", "--no-out", "--quiet", "--fail-on", "none"}, exitOK, "docs,"},
		{"bad format", []string{"check", fx, "--no-git", "--no-out", "--format", "xml"}, exitUsage, "--format must be"},
		{"bad fail-on", []string{"check", fx, "--no-git", "--no-out", "--fail-on", "loud"}, exitUsage, "--fail-on must be"},
		{"changed needs git", []string{"check", fx, "--no-git", "--no-out", "--changed"}, exitUsage, "needs git"},
		{"missing directory", []string{"check", filepath.Join(fx, "nope"), "--no-git", "--no-out"}, exitUsage, "not a directory"},
		{"explain a document", []string{"explain", filepath.Join(fx, "README.md"), "--no-git"}, exitOK, "missing"},
		{"explain needs a document", []string{"explain"}, exitUsage, "usage: docrot explain"},
		{"index routes", []string{"index", fx, "--kind", "routes"}, exitOK, "GET /v1/items"},
		{"index unknown kind", []string{"index", fx, "--kind", "planets"}, exitUsage, "unknown index kind"},
		{"pairs are warnings, below the default fail-on", []string{"pairs", fx, "--no-git"}, exitOK, "pair-heading"},
		{"comments", []string{"comments", fx, "--no-git"}, exitOK, "comment-mentions-missing"},
		{"fix without git has nothing mechanical", []string{"fix", fx, "--no-git"}, exitOK, "nothing to fix"},
		{"fix bad format", []string{"fix", fx, "--no-git", "--format", "xml"}, exitUsage, "--format must be"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errs := call(tc.args...)
			if code != tc.code {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", code, tc.code, out, errs)
			}
			if !strings.Contains(out+errs, tc.want) {
				t.Errorf("output lacks %q\nstdout: %s\nstderr: %s", tc.want, out, errs)
			}
		})
	}
}

func TestFormatsWriteFiles(t *testing.T) {
	fx := fixture(t)
	dir := t.TempDir()
	for _, format := range []string{"text", "md", "json", "sarif", "html", "github", "junit"} {
		out := filepath.Join(dir, "report."+format)
		code, _, errs := call("check", fx, "--no-git", "--no-out", "--format", format, "--output", out, "--fail-on", "none")
		if code != exitOK {
			t.Fatalf("%s: exit %d: %s", format, code, errs)
		}
		b, err := os.ReadFile(out)
		if err != nil || len(b) == 0 {
			t.Fatalf("%s: no report written: %v", format, err)
		}
		if format == "html" && (bytes.Contains(b, []byte("<script src")) || bytes.Contains(b, []byte("<link href"))) {
			t.Errorf("html report references an external resource")
		}
		if format == "github" && (!bytes.HasPrefix(b, []byte("::")) || !bytes.Contains(b, []byte("::error file=README.md,"))) {
			t.Errorf("github report lacks the README.md annotations: %.80s", b)
		}
		if format == "junit" && !bytes.Contains(b, []byte(`<testcase classname="README.md" name="missing-path"`)) {
			t.Errorf("junit report lacks the README.md missing-path case: %.200s", b)
		}
		if !strings.Contains(errs, "report written to") {
			t.Errorf("%s: stderr lacks the written notice: %s", format, errs)
		}
	}
}

// TestFixDryRunAndApply builds a small repository with a wrongly-cased
// link, checks that `fix` reports it without touching the file, and that
// `--apply` rewrites exactly that text.
func TestFixDryRunAndApply(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/x\n\ngo 1.22\n")
	write("docs/Guide.md", "# Guide\n")
	write("README.md", "# X\n\nRead the [guide](docs/guide.md) and `docs/GUIDE.md`.\n")

	code, out, errs := call("fix", dir, "--no-git")
	if code != exitOK || !strings.Contains(out, "docs/guide.md → docs/Guide.md") || !strings.Contains(out, "dry run") {
		t.Fatalf("dry run: exit %d\nstdout: %s\nstderr: %s", code, out, errs)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if !strings.Contains(string(b), "docs/guide.md") {
		t.Fatal("dry run modified README.md")
	}

	code, out, errs = call("fix", dir, "--no-git", "--apply")
	if code != exitOK || !strings.Contains(out, "2 fixes written to 1 file") {
		t.Fatalf("apply: exit %d\nstdout: %s\nstderr: %s", code, out, errs)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "README.md"))
	if string(b) != "# X\n\nRead the [guide](docs/Guide.md) and `docs/Guide.md`.\n" {
		t.Errorf("README.md after apply: %q", b)
	}
	// a second run has nothing left, and json output is well-formed
	code, out, _ = call("fix", dir, "--no-git", "--format", "json")
	if code != exitOK || !strings.Contains(out, `"fixes": 0`) {
		t.Errorf("json after apply: exit %d %s", code, out)
	}
}

func TestInitWritesConfig(t *testing.T) {
	dir := t.TempDir()
	code, _, errs := call("init", dir)
	if code != exitOK {
		t.Fatalf("init: exit %d: %s", code, errs)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".docrot.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"docs"`, `"outDir"`, `"stale"`, `"comments"`} {
		if !bytes.Contains(b, []byte(key)) {
			t.Errorf("config lacks %s", key)
		}
	}
	// the written config is a valid input for the next run
	if code, _, errs := call("check", dir, "--no-git", "--no-out"); code != exitOK {
		t.Errorf("check with the written config: exit %d: %s", code, errs)
	}
}

func TestParseInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	verbose := fs.Bool("verbose", false, "")
	name := fs.String("name", "", "")
	pos, err := parseInterspersed(fs, []string{"dir", "--verbose", "more", "--name", "x", "--", "--not-a-flag"})
	if err != nil {
		t.Fatal(err)
	}
	if !*verbose || *name != "x" {
		t.Errorf("flags: verbose=%v name=%q", *verbose, *name)
	}
	if strings.Join(pos, ",") != "dir,more,--not-a-flag" {
		t.Errorf("positionals = %v", pos)
	}
}
