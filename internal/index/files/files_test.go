package files

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// tree is the fixture layout used by most tests, relative to a temp root.
var tree = []string{
	"README.md",
	"README-zh.md",
	"llms.txt",
	"go.mod",
	"docs/contracts.md",
	"docs/rules.md",
	"docs/img/logo.png",
	"internal/httpx/httpx.go",
	"internal/httpx/httpx_test.go",
	"internal/model/model.go",
	"internal/model/testdata/sample.json",
	"cmd/docrot/main.go",
	"vendor/example.com/dep/dep.go",
	".git/config",
	"scripts/verify.py",
	"scripts/verify.test",
}

func makeTree(t *testing.T, paths []string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return root
}

func build(t *testing.T, exclude []string) *Index {
	t.Helper()
	ix, err := Build(makeTree(t, tree), exclude)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix
}

func TestBuildExistence(t *testing.T) {
	ix := build(t, []string{"vendor/**", "**/testdata/**"})
	tests := []struct {
		name              string
		rel               string
		file, dir, exists bool
	}{
		{"root file", "README.md", true, false, true},
		{"nested file", "docs/contracts.md", true, false, true},
		{"directory", "docs", false, true, true},
		{"nested directory", "internal/httpx", false, true, true},
		{"missing", "docs/nope.md", false, false, false},
		{"git always skipped", ".git/config", false, false, false},
		{"git dir skipped", ".git", false, false, false},
		{"excluded vendor file", "vendor/example.com/dep/dep.go", false, false, false},
		{"excluded vendor dir", "vendor", false, false, false},
		{"excluded testdata", "internal/model/testdata/sample.json", false, false, false},
		{"dot slash prefix", "./README.md", true, false, true},
		{"backslash separator", "docs" + string(os.PathSeparator) + "rules.md", true, false, true},
		{"trailing slash on dir", "docs/", false, true, true},
		{"case mismatch is not a match", "readme.md", false, false, false},
		{"empty path", "", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ix.FileExists(tt.rel); got != tt.file {
				t.Errorf("FileExists(%q) = %v, want %v", tt.rel, got, tt.file)
			}
			if got := ix.DirExists(tt.rel); got != tt.dir {
				t.Errorf("DirExists(%q) = %v, want %v", tt.rel, got, tt.dir)
			}
			if got := ix.Exists(tt.rel); got != tt.exists {
				t.Errorf("Exists(%q) = %v, want %v", tt.rel, got, tt.exists)
			}
		})
	}
}

func TestBuildExcludeByBaseName(t *testing.T) {
	ix := build(t, []string{"*.test"})
	if ix.FileExists("scripts/verify.test") {
		t.Error("a base-name exclude pattern should drop scripts/verify.test")
	}
	if !ix.FileExists("scripts/verify.py") {
		t.Error("scripts/verify.py should still be indexed")
	}
}

func TestBuildErrors(t *testing.T) {
	t.Run("missing root", func(t *testing.T) {
		if _, err := Build(filepath.Join(t.TempDir(), "nope"), nil); err == nil {
			t.Fatal("want error for a root that does not exist")
		}
	})
	t.Run("bad exclude pattern", func(t *testing.T) {
		if _, err := Build(t.TempDir(), []string{"[bad"}); err == nil {
			t.Fatal("want error for a malformed exclude pattern")
		}
	})
}

func TestFilesAndDirsSorted(t *testing.T) {
	ix := build(t, []string{"vendor/**", "**/testdata/**"})
	got := ix.Files()
	want := []string{
		"README-zh.md",
		"README.md",
		"cmd/docrot/main.go",
		"docs/contracts.md",
		"docs/img/logo.png",
		"docs/rules.md",
		"go.mod",
		"internal/httpx/httpx.go",
		"internal/httpx/httpx_test.go",
		"internal/model/model.go",
		"llms.txt",
		"scripts/verify.py",
		"scripts/verify.test",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Files() =\n%v\nwant\n%v", got, want)
	}
	if n := ix.Len(); n != len(ix.Files())+len(ix.Dirs()) {
		t.Errorf("Len() = %d, want %d", n, len(ix.Files())+len(ix.Dirs()))
	}
	// The returned slice must be a copy.
	got[0] = "mutated"
	if ix.Files()[0] == "mutated" {
		t.Error("Files() must return a copy")
	}
}

func TestTopLevelDirs(t *testing.T) {
	ix := build(t, []string{"vendor/**"})
	want := []string{"cmd", "docs", "internal", "scripts"}
	if got := ix.TopLevelDirs(); !reflect.DeepEqual(got, want) {
		t.Errorf("TopLevelDirs() = %v, want %v", got, want)
	}
}

func TestGlob(t *testing.T) {
	ix := build(t, []string{"vendor/**", "**/testdata/**"})
	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{"root markdown", "*.md", []string{"README-zh.md", "README.md"}},
		{"docs markdown", "docs/*.md", []string{"docs/contracts.md", "docs/rules.md"}},
		{"recursive markdown", "**/*.md", []string{"README-zh.md", "README.md", "docs/contracts.md", "docs/rules.md"}},
		{"recursive go", "internal/**/*.go", []string{"internal/httpx/httpx.go", "internal/httpx/httpx_test.go", "internal/model/model.go"}},
		{"directories match too", "internal/*", []string{"internal/httpx", "internal/model"}},
		{"everything under docs", "docs/**", []string{"docs", "docs/contracts.md", "docs/img", "docs/img/logo.png", "docs/rules.md"}},
		{"no match", "*.rs", nil},
		{"bad pattern", "[bad", nil},
		{"excluded not returned", "vendor/**", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ix.Glob(tt.pattern); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Glob(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestSimilarPaths(t *testing.T) {
	paths := []string{
		"README.md",
		"docs/contracts.md",
		"docs/contract.txt",
		"docs/guide/setup.md",
		"internal/httpx/httpx.go",
		"internal/model/model.go",
		"cmd/docrot/main.go",
		"deep/a/b/c/model.go",
	}
	root := makeTree(t, paths)
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	tests := []struct {
		name string
		rel  string
		n    int
		want []string
	}{
		{"case mismatch wins", "readme.md", 3, []string{"README.md"}},
		{"case mismatch nested", "DOCS/CONTRACTS.MD", 1, []string{"docs/contracts.md"}},
		{"same base name elsewhere, nearest dir first", "internal/model.go", 2,
			[]string{"internal/model/model.go", "deep/a/b/c/model.go"}},
		{"small edit distance in base name", "docs/contract.md", 1, []string{"docs/contracts.md"}},
		{"transposed letters", "docs/guide/setpu.md", 1, []string{"docs/guide/setup.md"}},
		{"same stem, other extension", "docs/setup.txt", 1, []string{"docs/guide/setup.md"}},
		{"self is never suggested", "README.md", 3, nil},
		{"nothing similar", "totally/unrelated/zzzzzzz.qqq", 3, nil},
		{"n zero", "readme.md", 0, nil},
		{"empty input", "", 3, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ix.SimilarPaths(tt.rel, tt.n)
			if len(got) > tt.n {
				t.Fatalf("SimilarPaths(%q, %d) returned %d results", tt.rel, tt.n, len(got))
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("SimilarPaths(%q, %d) = %v, want %v", tt.rel, tt.n, got, tt.want)
			}
		})
	}
}

func TestSimilarPathsDeduplicatesAndCaps(t *testing.T) {
	root := makeTree(t, []string{"a/x.md", "b/x.md", "c/x.md", "d/x.md", "x.md"})
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := ix.SimilarPaths("q/x.md", 3)
	if len(got) != 3 {
		t.Fatalf("SimilarPaths = %v, want 3 results", got)
	}
	seen := map[string]bool{}
	for _, g := range got {
		if seen[g] {
			t.Fatalf("duplicate suggestion %q in %v", g, got)
		}
		seen[g] = true
		if !strings.HasSuffix(g, "x.md") {
			t.Fatalf("unexpected suggestion %q", g)
		}
	}
}

func TestNorm(t *testing.T) {
	tests := []struct{ in, want string }{
		{"docs/a.md", "docs/a.md"},
		{"./docs/a.md", "docs/a.md"},
		{"docs/", "docs"},
		{"/docs", "docs"},
		{".", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := Norm(tt.in); got != tt.want {
				t.Errorf("Norm(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDirDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"docs", "docs", 0},
		{"", "docs", 1},
		{"docs", "docs/img", 1},
		{"docs/img", "docs/api", 2},
		{"a/b/c", "x/y", 5},
	}
	for _, tt := range tests {
		t.Run(tt.a+"|"+tt.b, func(t *testing.T) {
			if got := dirDistance(tt.a, tt.b); got != tt.want {
				t.Errorf("dirDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		max  int
		want int
	}{
		{"", "", 2, 0},
		{"abc", "abc", 2, 0},
		{"abc", "abd", 2, 1},
		{"contract", "contracts", 2, 1},
		{"setup", "setpu", 2, 2},
		{"abc", "", 5, 3},
		{"abcdef", "uvwxyz", 2, 3}, // over the cap: max+1
		{"a", "abcd", 2, 3},        // length difference alone exceeds the cap
	}
	for _, tt := range tests {
		t.Run(tt.a+"|"+tt.b, func(t *testing.T) {
			if got := levenshtein(tt.a, tt.b, tt.max); got != tt.want {
				t.Errorf("levenshtein(%q, %q, %d) = %d, want %d", tt.a, tt.b, tt.max, got, tt.want)
			}
		})
	}
}

func TestStemOf(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a.md", "a"},
		{"a.tar.gz", "a.tar"},
		{"Makefile", "Makefile"},
		{".gitignore", ".gitignore"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := stemOf(tt.in); got != tt.want {
				t.Errorf("stemOf(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
