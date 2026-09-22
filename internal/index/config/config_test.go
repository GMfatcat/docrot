package config

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

const configJSON = `{
  "server": {
    "addr": "localhost:8080",
    "flags": ["a", "b"]
  },
  "items": [
    {"name": "x", "value": 1},
    {"name": "y", "value": 2}
  ]
}
`

const configDevJSON = `{
  // this is a comment
  "debug": true, // trailing comment
  "server": {
    "addr": "0.0.0.0:9090" // override
  }
}
`

const badExampleJSON = `{ this is not valid json`

const deepJSON = `{
  "deep": {
    "key": "value"
  }
}
`

var patterns = []string{"config.json", "config*.json", "*.example.json", "configs/**/*.json"}

func buildFixture(t *testing.T) *Index {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "config.json", configJSON)
	writeFile(t, root, "config.dev.json", configDevJSON)
	writeFile(t, root, "bad.example.json", badExampleJSON)
	writeFile(t, root, "configs/sub/deep.json", deepJSON)
	writeFile(t, root, "unrelated.txt", "not json at all")
	ix, err := Build(root, patterns, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix
}

func TestBuildEmpty(t *testing.T) {
	root := t.TempDir()
	ix, err := Build(root, patterns, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ix.Empty() {
		t.Fatalf("expected Empty() when nothing matches")
	}
}

func TestFilesAndStats(t *testing.T) {
	ix := buildFixture(t)
	if ix.Empty() {
		t.Fatalf("expected non-empty index")
	}
	files := ix.Files()
	want := []string{"config.dev.json", "config.json", "configs/sub/deep.json"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("Files() = %v, want %v", files, want)
	}
	stats := ix.Stats()
	if stats.Files != 3 {
		t.Errorf("Stats.Files = %d, want 3", stats.Files)
	}
	if stats.Skipped != 1 {
		t.Errorf("Stats.Skipped = %d, want 1", stats.Skipped)
	}
	if stats.Keys == 0 {
		t.Errorf("Stats.Keys = 0, want > 0")
	}
}

func TestKeys(t *testing.T) {
	ix := buildFixture(t)
	keys := ix.Keys()
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(keys, sorted) {
		t.Errorf("Keys() not sorted: %v", keys)
	}
	want := map[string]bool{
		"server":       true,
		"server.addr":  true,
		"server.flags": true,
		"items":        true,
		"items.name":   true,
		"items.value":  true,
		"debug":        true,
		"deep":         true,
		"deep.key":     true,
	}
	got := make(map[string]bool)
	for _, k := range keys {
		got[k] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("Keys() missing %q; got %v", k, keys)
		}
	}
	if len(got) != len(want) {
		t.Errorf("Keys() = %v, want exactly %v", keys, want)
	}
}

func TestHas(t *testing.T) {
	ix := buildFixture(t)
	cases := []struct {
		dotted string
		want   bool
	}{
		{"server.addr", true},
		{"SERVER.ADDR", true},
		{"server", true},
		{"items.name", true},
		{"items[0].name", true},
		{"items[1].value", true},
		{"debug", true},
		{"deep.key", true},
		{"DEEP.KEY", true},
		{"items.nope", false},
		{"nonexistent", false},
		{"nonexistent.key", false},
	}
	for _, c := range cases {
		if got := ix.Has(c.dotted); got != c.want {
			t.Errorf("Has(%q) = %v, want %v", c.dotted, got, c.want)
		}
	}
}

func TestSimilar(t *testing.T) {
	ix := buildFixture(t)

	got := ix.Similar("servr.addr", 3)
	found := false
	for _, g := range got {
		if g == "server.addr" {
			found = true
		}
	}
	if !found {
		t.Errorf("Similar(servr.addr) = %v, want to contain server.addr", got)
	}

	if got := ix.Similar("totally-different-xyz-key", 3); len(got) != 0 {
		t.Errorf("Similar(totally-different-xyz-key) = %v, want empty", got)
	}
}

func TestExclude(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "config.json", `{"a": 1}`)
	writeFile(t, root, "skip/config.json", `{"b": 2}`)
	ix, err := Build(root, []string{"config.json"}, []string{"skip"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ix.Has("a") {
		t.Errorf("expected key 'a' to be indexed")
	}
	if ix.Has("b") {
		t.Errorf("expected key 'b' (under excluded dir) to be absent")
	}
}

func TestBasenameMatchInSubdir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "nested/dir/config.json", `{"z": 1}`)
	ix, err := Build(root, []string{"config.json"}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ix.Has("z") {
		t.Errorf("expected pattern without '/' to match basename in any directory")
	}
}
