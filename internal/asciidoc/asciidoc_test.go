package asciidoc

import (
	"reflect"
	"strings"
	"testing"
)

const sample = `= Project Title
Author Name

[[install]]
== Getting Started

Run ` + "`" + `make build` + "`" + ` then read link:docs/guide.adoc[the guide] and xref:usage[Usage],
or <<install>> and <<other.adoc#x,there>>. See https://example.com/page[site] and https://example.com/bare.
Config: ` + "`" + `config.json` + "`" + ` and src/main.go. Version 1.2.3.

[source,go]
----
import "docrot/internal/model"
----

....
$ go build ./cmd/app
....

// docrot:ignore
Ignored ` + "`" + `nope/x.go` + "`" + ` here.

////
a block comment
////

image::img/logo.png[Logo]
include::partials/x.adoc[]

== Getting Started

|===
| a | b
| 1 | 2
|===
`

func TestParse(t *testing.T) {
	d := Parse("README.adoc", []byte(sample))
	var heads []string
	for _, h := range d.Headings {
		heads = append(heads, h.Text+"|"+h.Slug)
	}
	want := []string{"Project Title|_project_title", "Getting Started|install", "Getting Started|_getting_started"}
	if !reflect.DeepEqual(heads, want) {
		t.Errorf("headings = %v, want %v", heads, want)
	}
	if !reflect.DeepEqual(d.Labels, []string{"install"}) {
		t.Errorf("labels = %v", d.Labels)
	}
	var spans []string
	for _, s := range d.Spans {
		spans = append(spans, s.Text)
	}
	if !reflect.DeepEqual(spans, []string{"make build", "config.json", "nope/x.go"}) {
		t.Errorf("spans = %v", spans)
	}
	var links []string
	for _, l := range d.Links {
		links = append(links, l.Target)
	}
	wantLinks := []string{"docs/guide.adoc", "#usage", "#install", "other.adoc#x", "https://example.com/page", "https://example.com/bare", "partials/x.adoc"}
	if !reflect.DeepEqual(links, wantLinks) {
		t.Errorf("links = %v, want %v", links, wantLinks)
	}
	if len(d.Images) != 1 || d.Images[0].Target != "img/logo.png" {
		t.Errorf("images = %+v", d.Images)
	}
	var fences []string
	for _, f := range d.Fences {
		fences = append(fences, f.Lang+":"+strings.Join(f.Content, "|"))
	}
	if !reflect.DeepEqual(fences, []string{`go:import "docrot/internal/model"`, ":$ go build ./cmd/app"}) {
		t.Errorf("fences = %v", fences)
	}
	if len(d.Tables) != 1 || d.Tables[0].Rows != 2 {
		t.Errorf("tables = %+v", d.Tables)
	}
	var bare []string
	for _, b := range d.BarePaths {
		bare = append(bare, b.Text)
	}
	if !reflect.DeepEqual(bare, []string{"src/main.go"}) {
		t.Errorf("bare = %v", bare)
	}
	for _, s := range d.Spans {
		if s.Text == "nope/x.go" && !d.Ignored(s.Line) {
			t.Error("docrot:ignore did not cover the next line")
		}
	}
	if len(d.Comments) != 2 {
		t.Errorf("comments = %+v", d.Comments)
	}
}
