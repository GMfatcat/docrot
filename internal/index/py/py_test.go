package py

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const initPy = `"""Package init."""
VERSION = "1.0"
`

const modPy = `MAX_RETRIES = 3

class Greeter:
    def __init__(self, name):
        self.name = name

    def greet(self):
        return f"hello {self.name}"

    async def agreet(self):
        return self.greet()


def helper():
    return 42
`

const standalonePy = `def standalone_func():
    pass
`

func buildFixture(t *testing.T) *Index {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "pkg/__init__.py", initPy)
	writeFile(t, root, "pkg/mod.py", modPy)
	writeFile(t, root, "standalone.py", standalonePy)
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix
}

func TestBuildEmpty(t *testing.T) {
	root := t.TempDir()
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ix.Empty() {
		t.Fatalf("expected Empty() on dir with no .py files")
	}
}

func TestModulesAndStats(t *testing.T) {
	ix := buildFixture(t)
	if ix.Empty() {
		t.Fatalf("expected non-empty index")
	}
	mods := ix.Modules()
	want := []string{"pkg", "pkg.mod", "standalone"}
	if len(mods) != len(want) {
		t.Fatalf("Modules() = %v, want %v", mods, want)
	}
	for i, w := range want {
		if mods[i] != w {
			t.Errorf("Modules()[%d] = %q, want %q", i, mods[i], w)
		}
	}
	stats := ix.Stats()
	if stats.Files != 3 {
		t.Errorf("Stats.Files = %d, want 3", stats.Files)
	}
	if stats.Modules != 3 {
		t.Errorf("Stats.Modules = %d, want 3", stats.Modules)
	}
	if stats.Symbols == 0 {
		t.Errorf("Stats.Symbols = 0, want > 0")
	}
}

func TestHas(t *testing.T) {
	ix := buildFixture(t)
	cases := []struct {
		q    string
		want bool
	}{
		{"pkg.mod.Greeter.greet", true},
		{"Greeter.greet", true},
		{"Greeter.greet()", true},
		{"pkg.mod.Greeter.agreet", true},
		{"agreet", true},
		{"pkg.mod.helper", true},
		{"helper", true},
		{"helper()", true},
		{"pkg.mod.MAX_RETRIES", true},
		{"pkg.VERSION", true},
		{"VERSION", true},
		{"standalone.standalone_func", true},
		{"standalone_func", true},
		{"pkg.mod.Greeter.nope", false},
		{"Greeter.nope", false},
		{"nope", false},
		{"pkg.mod.Greeter", true}, // class itself indexed under module.Class
	}
	for _, c := range cases {
		if got := ix.Has(c.q); got != c.want {
			t.Errorf("Has(%q) = %v, want %v", c.q, got, c.want)
		}
	}
}

func TestFile(t *testing.T) {
	ix := buildFixture(t)
	file, line, ok := ix.File("pkg.mod.helper")
	if !ok {
		t.Fatalf("File(pkg.mod.helper) not found")
	}
	if file != "pkg/mod.py" {
		t.Errorf("File() file = %q, want pkg/mod.py", file)
	}
	if line <= 0 {
		t.Errorf("File() line = %d, want > 0", line)
	}

	if _, _, ok := ix.File("pkg.mod.nope"); ok {
		t.Errorf("File(pkg.mod.nope) found, want not found")
	}
}

func TestSimilar(t *testing.T) {
	ix := buildFixture(t)

	got := ix.Similar("pkg.mod.Greeter.gret", 3)
	found := false
	for _, g := range got {
		if g == "pkg.mod.Greeter.greet" {
			found = true
		}
	}
	if !found {
		t.Errorf("Similar(pkg.mod.Greeter.gret) = %v, want to contain pkg.mod.Greeter.greet", got)
	}

	got = ix.Similar("helpr", 3)
	found = false
	for _, g := range got {
		if g == "pkg.mod.helper" {
			found = true
		}
	}
	if !found {
		t.Errorf("Similar(helpr) = %v, want to contain pkg.mod.helper", got)
	}

	if got := ix.Similar("completely-unrelated-xyz", 3); len(got) != 0 {
		t.Errorf("Similar(completely-unrelated-xyz) = %v, want empty", got)
	}
}

func TestExclude(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "keep.py", "def foo():\n    pass\n")
	writeFile(t, root, "skip/skip.py", "def bar():\n    pass\n")
	ix, err := Build(root, []string{"skip"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ix.Has("foo") {
		t.Errorf("expected foo to be indexed")
	}
	if ix.Has("bar") {
		t.Errorf("expected bar to be excluded")
	}
}

func TestNestedPackage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/__init__.py", "")
	writeFile(t, root, "pkg/sub/__init__.py", "")
	writeFile(t, root, "pkg/sub/leaf.py", "def deep():\n    pass\n")
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	mods := ix.Modules()
	wantMods := map[string]bool{"pkg": true, "pkg.sub": true, "pkg.sub.leaf": true}
	if len(mods) != len(wantMods) {
		t.Fatalf("Modules() = %v, want %d entries", mods, len(wantMods))
	}
	for _, m := range mods {
		if !wantMods[m] {
			t.Errorf("unexpected module %q", m)
		}
	}
	if !ix.Has("pkg.sub.leaf.deep") {
		t.Errorf("expected pkg.sub.leaf.deep to be indexed")
	}
}
