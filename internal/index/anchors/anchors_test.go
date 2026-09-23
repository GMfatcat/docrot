package anchors

import (
	"reflect"
	"strings"
	"testing"

	"docrot/internal/markdown"
)

func build(t *testing.T, docs map[string][]string) *Index {
	t.Helper()
	ix := New()
	for path, lines := range docs {
		ix.Add(path, markdown.Parse(path, []byte(strings.Join(lines, "\n"))))
	}
	return ix
}

func TestHasAndAnchors(t *testing.T) {
	ix := build(t, map[string][]string{
		"README.md": {
			"# docrot",
			"## Getting Started",
			"## \U0001F3AF 核心目標",
			"## Usage",
			"## Usage",
			"### foo_bar",
		},
	})

	tests := []struct {
		name string
		doc  string
		slug string
		want bool
	}{
		{"simple", "README.md", "docrot", true},
		{"two words", "README.md", "getting-started", true},
		{"cjk with emoji", "README.md", "-核心目標", true},
		{"duplicate suffix", "README.md", "usage-1", true},
		{"raw heading text fallback", "README.md", "getting started", true},
		{"underscore normalised", "README.md", "foo_bar", true},
		{"slug of underscore heading", "README.md", "foo-bar", true},
		{"mixed case input", "README.md", "Getting-Started", true},
		{"windows separator in path", "README.md", "docrot", true},
		{"dot slash prefix", "./README.md", "docrot", true},
		{"missing slug", "README.md", "nope", false},
		{"missing doc", "OTHER.md", "docrot", false},
		{"empty slug", "README.md", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ix.Has(tt.doc, tt.slug); got != tt.want {
				t.Errorf("Has(%q, %q) = %v, want %v", tt.doc, tt.slug, got, tt.want)
			}
		})
	}

	want := []string{"-核心目標", "docrot", "foo_bar", "getting-started", "usage", "usage-1"}
	if got := ix.Anchors("README.md"); !reflect.DeepEqual(got, want) {
		t.Errorf("Anchors = %v, want %v", got, want)
	}
	if got := ix.Anchors("nope.md"); got != nil {
		t.Errorf("Anchors(missing) = %v, want nil", got)
	}
}

func TestBackslashPathIsNormalised(t *testing.T) {
	ix := build(t, map[string][]string{"docs/a.md": {"# Intro"}})
	if !ix.Has(`docs\a.md`, "intro") {
		t.Error("backslash path should normalise to forward slashes")
	}
}

func TestAnchorsIsACopy(t *testing.T) {
	ix := build(t, map[string][]string{"a.md": {"# One", "# Two"}})
	got := ix.Anchors("a.md")
	got[0] = "mutated"
	if ix.Anchors("a.md")[0] == "mutated" {
		t.Error("Anchors must return a copy")
	}
}

func TestDocs(t *testing.T) {
	ix := build(t, map[string][]string{
		"b.md":      {"# B"},
		"a.md":      {"# A"},
		"docs/c.md": {"# C"},
	})
	want := []string{"a.md", "b.md", "docs/c.md"}
	if got := ix.Docs(); !reflect.DeepEqual(got, want) {
		t.Errorf("Docs = %v, want %v", got, want)
	}
	if got := New().Docs(); len(got) != 0 {
		t.Errorf("Docs on empty index = %v", got)
	}
}

func TestAddMergesAndIgnoresNil(t *testing.T) {
	ix := New()
	ix.Add("a.md", markdown.Parse("a.md", []byte("# One\n")))
	ix.Add("a.md", markdown.Parse("a.md", []byte("# Two\n")))
	ix.Add("a.md", nil)
	want := []string{"one", "two"}
	if got := ix.Anchors("a.md"); !reflect.DeepEqual(got, want) {
		t.Errorf("Anchors = %v, want %v", got, want)
	}
	if got := ix.Docs(); !reflect.DeepEqual(got, []string{"a.md"}) {
		t.Errorf("Docs = %v", got)
	}
}

func TestSimilar(t *testing.T) {
	ix := build(t, map[string][]string{
		"README.md": {"# Installation", "## Configuration", "## Usage", "## Contributing"},
	})
	tests := []struct {
		name string
		slug string
		n    int
		want []string
	}{
		{"typo", "instalation", 3, []string{"installation"}},
		{"transposition", "usaeg", 3, []string{"usage"}},
		{"exact match excluded", "usage", 3, nil},
		{"nothing close", "zzzzzzzzzzzzz", 3, nil},
		{"n limits results", "configuratio", 1, []string{"configuration"}},
		{"zero n", "usage", 0, nil},
		{"empty slug", "", 3, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ix.Similar("README.md", tt.slug, tt.n)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Similar(%q, %d) = %v, want %v", tt.slug, tt.n, got, tt.want)
			}
		})
	}
	if got := ix.Similar("missing.md", "usage", 3); got != nil {
		t.Errorf("Similar(missing doc) = %v", got)
	}
}

func TestSimilarOrdering(t *testing.T) {
	ix := build(t, map[string][]string{"a.md": {"# abc", "# abd", "# abcd", "# xyz"}})
	got := ix.Similar("a.md", "abc", 3)
	want := []string{"abcd", "abd"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Similar = %v, want %v", got, want)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
		{"usage", "usage", 0},
		{"核心", "核心目標", 2},
	}
	for _, tt := range tests {
		if got := levenshtein(tt.a, tt.b); got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestConcurrentReads(t *testing.T) {
	ix := build(t, map[string][]string{"a.md": {"# One", "## Two", "## Three"}})
	done := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				if !ix.Has("a.md", "one") {
					t.Error("missing anchor")
					break
				}
				_ = ix.Anchors("a.md")
				_ = ix.Docs()
				_ = ix.Similar("a.md", "twoo", 2)
			}
			done <- true
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
