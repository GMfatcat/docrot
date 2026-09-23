package anchors

import (
	"testing"

	"docrot/internal/markdown"
)

func TestCustomIDsAndGeneratedDocs(t *testing.T) {
	ix := New()
	ix.Add("docs/async.md", markdown.Parse("docs/async.md", []byte("## In a hurry? { #in-a-hurry }\n\ntext [](){#inline-anchor} more\n\n{#standalone_id}\n### Real heading\n")))
	ix.Add("docs/api.md", markdown.Parse("docs/api.md", []byte("# API\n\n::: pkg.module\n")))
	for slug, want := range map[string]bool{
		"in-a-hurry": true, "inline-anchor": true, "standalone_id": true, "real-heading": true, "nope": false,
	} {
		if got := ix.Has("docs/async.md", slug); got != want {
			t.Errorf("Has(async, %q) = %v, want %v", slug, got, want)
		}
	}
	if !ix.Has("docs/api.md", "pkg.module.anything") {
		t.Error("mkdocstrings-generated docs must accept any anchor")
	}
}
