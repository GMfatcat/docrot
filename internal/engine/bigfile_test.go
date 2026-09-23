package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"docrot/internal/config"
)

// TestLargeAndBinaryFilesAreNeverParsed pins the resource policy: binaries
// are only ever listed by name, and text files above maxFileMB are skipped
// with a warning while their paths still resolve.
func TestLargeAndBinaryFilesAreNeverParsed(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, data []byte) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// a 3 MiB "model" full of NUL bytes and a 3 MiB Python and Markdown file
	blob := bytes.Repeat([]byte{0}, 3<<20)
	mk("models/big.gguf", blob)
	mk("gen/huge.py", append([]byte("def generated():\n    pass\n"), bytes.Repeat([]byte("# x\n"), 800_000)...))
	mk("docs/dump.md", append([]byte("# dump\n"), bytes.Repeat([]byte("row\n"), 800_000)...))
	mk("go.mod", []byte("module example.com/big\n"))
	mk("README.md", []byte("# big\n\nSee `models/big.gguf`, `gen/huge.py`, `docs/dump.md` and `generated()`.\n"))

	cfg := config.Default()
	cfg.MaxFileMB = 1
	var stderr bytes.Buffer
	run, err := Check(Options{Root: root, Config: cfg, NoGit: true, NoOut: true, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"skipping gen/huge.py", "skipping docs/dump.md"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr.String())
		}
	}
	if strings.Contains(stderr.String(), "big.gguf") {
		t.Errorf("binary must not even be mentioned: %s", stderr.String())
	}
	for _, f := range run.Report.Findings {
		if f.Ref != nil && (f.Ref.Text == "models/big.gguf" || f.Ref.Text == "gen/huge.py" || f.Ref.Text == "docs/dump.md") {
			t.Errorf("path to a large/binary file must still resolve: %+v", f)
		}
	}
	if len(run.Docs) != 2 || run.Docs[0] != "README.md" || run.Docs[1] != "docs/dump.md" {
		t.Errorf("docs = %v (dump.md is discovered but skipped)", run.Docs)
	}
	if _, ok := run.Refs["docs/dump.md"]; ok {
		t.Error("skipped document must not have references")
	}
}
