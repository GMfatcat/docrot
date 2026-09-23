package markdown

import (
	"reflect"
	"strings"
	"testing"
)

// doc parses a document written as a slice of lines.
func doc(lines ...string) *Doc {
	return Parse("t.md", []byte(strings.Join(lines, "\n")))
}

const bt = "`"

func TestSlug(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Hello World", "hello-world"},
		{"case", "Getting STARTED", "getting-started"},
		{"punctuation dropped", "What's new?", "whats-new"},
		{"underscore kept (GitHub)", "foo_bar", "foo_bar"},
		{"hyphens not collapsed", "A -- B", "a----b"},
		{"emoji removed keeps hyphen", "\U0001F431 meowbase", "-meowbase"},
		{"cjk kept", "\U0001F3AF 核心目標", "-核心目標"},
		{"cjk plain", "安裝說明", "安裝說明"},
		{"code span", "Using " + bt + "go test" + bt, "using-go-test"},
		{"custom id", "Install {#install-here}", "install"},
		{"inline link", "[Setup](#setup)", "setup"},
		{"ref link", "[Setup][s]", "setup"},
		{"html tag", `<a name="x"></a> Setup`, "-setup"},
		{"digits", "Go 1.26 support", "go-126-support"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Slug(tt.in); got != tt.want {
				t.Errorf("Slug(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHeadings(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  []Heading
	}{
		{
			name:  "atx levels",
			lines: []string{"# One", "### Three", "####### TooDeep"},
			want: []Heading{
				{Line: 1, Level: 1, Text: "One", Slug: "one"},
				{Line: 2, Level: 3, Text: "Three", Slug: "three"},
			},
		},
		{
			name:  "closing hashes",
			lines: []string{"## Title ##", "## Keep#"},
			want: []Heading{
				{Line: 1, Level: 2, Text: "Title", Slug: "title"},
				{Line: 2, Level: 2, Text: "Keep#", Slug: "keep"},
			},
		},
		{
			name:  "no space is not a heading",
			lines: []string{"#NotAHeading", "#"},
			want:  []Heading{{Line: 2, Level: 1, Text: "", Slug: ""}},
		},
		{
			name:  "duplicates get suffixes",
			lines: []string{"## Usage", "## Usage", "## Usage"},
			want: []Heading{
				{Line: 1, Level: 2, Text: "Usage", Slug: "usage"},
				{Line: 2, Level: 2, Text: "Usage", Slug: "usage-1"},
				{Line: 3, Level: 2, Text: "Usage", Slug: "usage-2"},
			},
		},
		{
			name:  "cjk and emoji",
			lines: []string{"## \U0001F3AF 核心目標"},
			want: []Heading{
				{Line: 1, Level: 2, Text: "\U0001F3AF 核心目標", Slug: "-核心目標"},
			},
		},
		{
			name:  "setext",
			lines: []string{"Title", "=====", "", "Sub", "---", "", "body"},
			want: []Heading{
				{Line: 1, Level: 1, Text: "Title", Slug: "title"},
				{Line: 4, Level: 2, Text: "Sub", Slug: "sub"},
			},
		},
		{
			name:  "setext not after blank or list",
			lines: []string{"", "---", "- item", "---"},
			want:  nil,
		},
		{
			name:  "front matter close is not setext",
			lines: []string{"---", "title: x", "---", "# Real"},
			want:  []Heading{{Line: 4, Level: 1, Text: "Real", Slug: "real"}},
		},
		{
			name:  "heading inside fence ignored",
			lines: []string{"# Real", "```", "# Fake", "```"},
			want:  []Heading{{Line: 1, Level: 1, Text: "Real", Slug: "real"}},
		},
		{
			name:  "heading inside comment ignored",
			lines: []string{"<!--", "# Fake", "-->", "# Real"},
			want:  []Heading{{Line: 4, Level: 1, Text: "Real", Slug: "real"}},
		},
		{
			name:  "trailing comment stripped from text",
			lines: []string{"## Title <!-- docrot:ignore -->"},
			want:  []Heading{{Line: 1, Level: 2, Text: "Title", Slug: "title"}},
		},
		{
			name:  "table delimiter is not setext",
			lines: []string{"a | b", "--- | ---", "1 | 2"},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := doc(tt.lines...).Headings
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("headings = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFences(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  []Fence
	}{
		{
			name:  "basic",
			lines: []string{"```go", "x := 1", "```"},
			want:  []Fence{{StartLine: 1, EndLine: 3, Lang: "go", Info: "go", Content: []string{"x := 1"}}},
		},
		{
			name:  "info string lang is first word lowercased",
			lines: []string{"```GO title=\"a b\"", "x", "```"},
			want: []Fence{{StartLine: 1, EndLine: 3, Lang: "go",
				Info: "GO title=\"a b\"", Content: []string{"x"}}},
		},
		{
			name:  "tilde fence may contain backticks",
			lines: []string{"~~~text", "```", "~~~"},
			want:  []Fence{{StartLine: 1, EndLine: 3, Lang: "text", Info: "text", Content: []string{"```"}}},
		},
		{
			name:  "nested in list item",
			lines: []string{"- step", "", "  ```sh", "  go test ./...", "  ```", "", "after"},
			want: []Fence{{StartLine: 3, EndLine: 5, Lang: "sh", Info: "sh",
				Content: []string{"go test ./..."}}},
		},
		{
			name:  "four backticks enclose three",
			lines: []string{"````md", "```go", "x", "```", "````"},
			want: []Fence{{StartLine: 1, EndLine: 5, Lang: "md", Info: "md",
				Content: []string{"```go", "x", "```"}}},
		},
		{
			name:  "unclosed runs to end of document",
			lines: []string{"text", "```", "a", "b"},
			want: []Fence{{StartLine: 2, EndLine: 4, Lang: "", Info: "",
				Content: []string{"a", "b"}}},
		},
		{
			name:  "longer closer is allowed",
			lines: []string{"```", "a", "`````"},
			want:  []Fence{{StartLine: 1, EndLine: 3, Content: []string{"a"}}},
		},
		{
			name:  "backtick info string is not a fence",
			lines: []string{"```a`b```"},
			want:  nil,
		},
		{
			name:  "indented code block is not a fence",
			lines: []string{"para", "", "    not code to docrot"},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := doc(tt.lines...).Fences
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fences = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFenceContentIsNotScanned(t *testing.T) {
	d := doc("# H", "```go", "a `code` b [l](t) <!-- docrot:ignore-file -->", "```")
	if len(d.Spans) != 0 {
		t.Errorf("spans = %+v, want none", d.Spans)
	}
	if len(d.Links) != 0 {
		t.Errorf("links = %+v, want none", d.Links)
	}
	if len(d.Comments) != 0 {
		t.Errorf("comments = %+v, want none", d.Comments)
	}
	if d.IgnoreFile {
		t.Error("IgnoreFile set from inside a fence")
	}
}

func TestSpans(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  []Span
	}{
		{
			name:  "simple with column",
			lines: []string{"Use " + bt + "foo" + bt + " here"},
			want:  []Span{{Line: 1, Col: 6, Text: "foo", Kind: "code"}},
		},
		{
			name:  "double backticks hold a backtick",
			lines: []string{"a ``x ` y`` b"},
			want:  []Span{{Line: 1, Col: 5, Text: "x ` y", Kind: "code"}},
		},
		{
			name:  "one space stripped from both ends",
			lines: []string{bt + bt + " " + bt + "a" + bt + " " + bt + bt},
			want:  []Span{{Line: 1, Col: 3, Text: "`a`", Kind: "code"}},
		},
		{
			name:  "blank content is not stripped",
			lines: []string{bt + "  " + bt},
			want:  []Span{{Line: 1, Col: 2, Text: "  ", Kind: "code"}},
		},
		{
			name:  "unmatched backtick is literal",
			lines: []string{"a " + bt + "b c", "d " + bt + "e" + bt + " f"},
			want:  []Span{{Line: 2, Col: 4, Text: "e", Kind: "code"}},
		},
		{
			name:  "run length must match",
			lines: []string{"a ``b` c``"},
			want:  []Span{{Line: 1, Col: 5, Text: "b` c", Kind: "code"}},
		},
		{
			name:  "cjk columns are byte offsets",
			lines: []string{"中文 " + bt + "x" + bt},
			want:  []Span{{Line: 1, Col: 9, Text: "x", Kind: "code"}},
		},
		{
			name:  "comment inside a span stays a span",
			lines: []string{"see " + bt + "<!-- docrot:ignore -->" + bt},
			want:  []Span{{Line: 1, Col: 6, Text: "<!-- docrot:ignore -->", Kind: "code"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := doc(tt.lines...).Spans
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("spans = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSpanSection(t *testing.T) {
	d := doc("# Top", "a "+bt+"x"+bt, "## Sub", "b "+bt+"y"+bt)
	want := []string{"Top", "Sub"}
	var got []string
	for _, s := range d.Spans {
		got = append(got, s.Section)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sections = %v, want %v", got, want)
	}
	if d.SectionAt(1) != "Top" || d.SectionAt(4) != "Sub" {
		t.Errorf("SectionAt = %q/%q", d.SectionAt(1), d.SectionAt(4))
	}
	if s := Parse("t.md", []byte("no heading\n")).SectionAt(1); s != "" {
		t.Errorf("SectionAt before any heading = %q, want empty", s)
	}
}

func TestLinks(t *testing.T) {
	tests := []struct {
		name   string
		lines  []string
		links  []Link
		images []Link
	}{
		{
			name:  "inline",
			lines: []string{"see [docs](docs/a.md) now"},
			links: []Link{{Line: 1, Col: 5, Text: "docs", Target: "docs/a.md"}},
		},
		{
			name:  "with title",
			lines: []string{`[a](b.md "T")`},
			links: []Link{{Line: 1, Col: 1, Text: "a", Target: "b.md", Title: "T"}},
		},
		{
			name:  "angle destination",
			lines: []string{"[a](<b c.md>)"},
			links: []Link{{Line: 1, Col: 1, Text: "a", Target: "b c.md"}},
		},
		{
			name:  "nested brackets in text",
			lines: []string{"[see [this] thing](u.md)"},
			links: []Link{{Line: 1, Col: 1, Text: "see [this] thing", Target: "u.md"}},
		},
		{
			name:  "balanced parens in destination",
			lines: []string{"[w](https://en.wikipedia.org/wiki/Foo_(bar))"},
			links: []Link{{Line: 1, Col: 1, Text: "w", Target: "https://en.wikipedia.org/wiki/Foo_(bar)"}},
		},
		{
			name:   "image",
			lines:  []string{"![alt](img/x.png)"},
			images: []Link{{Line: 1, Col: 1, Text: "alt", Target: "img/x.png", IsImage: true}},
		},
		{
			name:  "reference link",
			lines: []string{"see [spec][s] here", "", "[s]: docs/spec.md"},
			links: []Link{{Line: 1, Col: 5, Text: "spec", Target: "docs/spec.md"}},
		},
		{
			name:  "collapsed reference",
			lines: []string{"see [spec][]", "", "[SPEC]: docs/spec.md"},
			links: []Link{{Line: 1, Col: 5, Text: "spec", Target: "docs/spec.md"}},
		},
		{
			name:  "undefined reference is dropped",
			lines: []string{"see [spec][nope]"},
			links: nil,
		},
		{
			name:  "autolink",
			lines: []string{"<https://auto.example/x>"},
			links: []Link{{Line: 1, Col: 1, Target: "https://auto.example/x"}},
		},
		{
			name:  "bare url with trailing punctuation",
			lines: []string{"go to https://bare.example/a/b."},
			links: []Link{{Line: 1, Col: 7, Target: "https://bare.example/a/b"}},
		},
		{
			name:  "bare url keeps balanced parens",
			lines: []string{"x https://en.wikipedia.org/wiki/Foo_(bar) y"},
			links: []Link{{Line: 1, Col: 3, Target: "https://en.wikipedia.org/wiki/Foo_(bar)"}},
		},
		{
			name:  "no link parsing inside a code span",
			lines: []string{"a " + bt + "[x](y.md)" + bt + " b"},
			links: nil,
		},
		{
			name:  "no bare url inside a code span",
			lines: []string{bt + "https://x.example/y" + bt},
			links: nil,
		},
		{
			name:  "link inside a comment is ignored",
			lines: []string{"<!-- [a](b.md) -->"},
			links: nil,
		},
		{
			name:  "anchor link",
			lines: []string{"[jump](#usage)"},
			links: []Link{{Line: 1, Col: 1, Text: "jump", Target: "#usage"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := doc(tt.lines...)
			if !reflect.DeepEqual(d.Links, tt.links) {
				t.Errorf("links = %+v, want %+v", d.Links, tt.links)
			}
			if !reflect.DeepEqual(d.Images, tt.images) {
				t.Errorf("images = %+v, want %+v", d.Images, tt.images)
			}
		})
	}
}

func TestRefDefs(t *testing.T) {
	d := doc(`[Spec]: docs/spec.md "The spec"`, "[b]: <a b.md>", "not: a refdef")
	want := []Link{
		{Line: 1, Col: 1, Text: "Spec", Target: "docs/spec.md", Title: "The spec"},
		{Line: 2, Col: 1, Text: "b", Target: "a b.md"},
	}
	if !reflect.DeepEqual(d.RefDefs, want) {
		t.Errorf("refdefs = %+v, want %+v", d.RefDefs, want)
	}
	if len(d.Links) != 0 {
		t.Errorf("refdef lines must not produce links, got %+v", d.Links)
	}
}

func TestComments(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  []Comment
	}{
		{
			name:  "single line",
			lines: []string{"<!-- hidden -->"},
			want:  []Comment{{StartLine: 1, EndLine: 1, Text: "hidden"}},
		},
		{
			name:  "inline in prose",
			lines: []string{"a <!-- x --> b <!-- y --> c"},
			want: []Comment{
				{StartLine: 1, EndLine: 1, Text: "x"},
				{StartLine: 1, EndLine: 1, Text: "y"},
			},
		},
		{
			name:  "multi line",
			lines: []string{"before", "<!--", "multi", "line", "-->", "after"},
			want:  []Comment{{StartLine: 2, EndLine: 5, Text: "multi\nline"}},
		},
		{
			name:  "unterminated runs to end",
			lines: []string{"a", "<!-- oops", "b"},
			want:  []Comment{{StartLine: 2, EndLine: 3, Text: "oops\nb"}},
		},
		{
			name:  "text after a closing comment is still scanned",
			lines: []string{"<!--", "x", "--> tail " + bt + "s" + bt},
			want:  []Comment{{StartLine: 1, EndLine: 3, Text: "x"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := doc(tt.lines...).Comments
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("comments = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCommentTailIsScanned(t *testing.T) {
	d := doc("<!--", "x", "--> tail "+bt+"s"+bt)
	want := []Span{{Line: 3, Col: 11, Text: "s", Kind: "code"}}
	if !reflect.DeepEqual(d.Spans, want) {
		t.Errorf("spans = %+v, want %+v", d.Spans, want)
	}
}

func TestIgnoreDirectives(t *testing.T) {
	tests := []struct {
		name       string
		lines      []string
		ignored    []int
		notIgnored []int
		ignoreFile bool
	}{
		{
			name:       "alone ignores next non-blank line",
			lines:      []string{"a", "<!-- docrot:ignore -->", "", "b", "c"},
			ignored:    []int{4},
			notIgnored: []int{1, 2, 3, 5},
		},
		{
			name:       "end of line ignores that line",
			lines:      []string{"a", "b " + bt + "x" + bt + " <!-- docrot:ignore -->", "c"},
			ignored:    []int{2},
			notIgnored: []int{1, 3},
		},
		{
			name:       "range is inclusive",
			lines:      []string{"a", "<!-- docrot:ignore-start -->", "b", "c", "<!-- docrot:ignore-end -->", "d"},
			ignored:    []int{2, 3, 4, 5},
			notIgnored: []int{1, 6},
		},
		{
			name:       "unterminated range reaches end",
			lines:      []string{"a", "<!-- docrot:ignore-start -->", "b"},
			ignored:    []int{2, 3},
			notIgnored: []int{1},
		},
		{
			name:       "multi-line ignore comment",
			lines:      []string{"<!--", "docrot:ignore", "-->", "", "target", "after"},
			ignored:    []int{5},
			notIgnored: []int{1, 2, 3, 4, 6},
		},
		{
			name:       "ignore file",
			lines:      []string{"<!-- docrot:ignore-file -->", "a"},
			notIgnored: []int{1, 2},
			ignoreFile: true,
		},
		{
			name:       "unrelated comment does nothing",
			lines:      []string{"<!-- TODO -->", "a"},
			notIgnored: []int{1, 2},
		},
		{
			name:       "quoted directive in a code span does nothing",
			lines:      []string{"write " + bt + "<!-- docrot:ignore -->" + bt, "a"},
			notIgnored: []int{1, 2},
		},
		{
			name:       "rule-scoped directive does not set Ignored",
			lines:      []string{"<!-- docrot:ignore missing-path -->", "a", "b <!-- docrot:ignore-start unknown-flag, unknown-env -->", "c", "<!-- docrot:ignore-end -->", "d"},
			notIgnored: []int{1, 2, 3, 4, 5, 6},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := doc(tt.lines...)
			if d.IgnoreFile != tt.ignoreFile {
				t.Errorf("IgnoreFile = %v, want %v", d.IgnoreFile, tt.ignoreFile)
			}
			for _, l := range tt.ignored {
				if !d.Ignored(l) {
					t.Errorf("line %d should be ignored", l)
				}
			}
			for _, l := range tt.notIgnored {
				if d.Ignored(l) {
					t.Errorf("line %d should not be ignored", l)
				}
			}
		})
	}
}

func TestRuleScopedIgnores(t *testing.T) {
	d := doc("<!-- docrot:ignore missing-path -->", "a", "b <!-- docrot:ignore-start unknown-flag, unknown-env -->", "c", "<!-- docrot:ignore-end -->", "d", "e <!-- docrot:ignore missing-symbol unknown-flag -->")
	type key struct {
		line int
		rule string
	}
	want := map[key]bool{
		{2, "missing-path"}: true, {2, "unknown-flag"}: false, {1, "missing-path"}: false,
		{3, "unknown-flag"}: true, {4, "unknown-env"}: true, {5, "unknown-flag"}: true, {6, "unknown-flag"}: false,
		{7, "missing-symbol"}: true, {7, "unknown-flag"}: true, {7, "missing-path"}: false,
	}
	for k, v := range want {
		if got := d.RuleIgnored(k.line, k.rule); got != v {
			t.Errorf("RuleIgnored(%d, %s) = %v, want %v", k.line, k.rule, got, v)
		}
	}
}

func TestTables(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  []Table
	}{
		{
			name:  "basic",
			lines: []string{"| a | b |", "|---|---|", "| 1 | 2 |", "| 3 | 4 |", "", "after"},
			want:  []Table{{StartLine: 1, EndLine: 4, Rows: 2, Cols: 2}},
		},
		{
			name:  "alignment markers and no outer pipes",
			lines: []string{"a | b | c", ":--|:-:|--:", "1 | 2 | 3"},
			want:  []Table{{StartLine: 1, EndLine: 3, Rows: 1, Cols: 3}},
		},
		{
			name:  "header only",
			lines: []string{"| x |", "| --- |"},
			want:  []Table{{StartLine: 1, EndLine: 2, Rows: 0, Cols: 1}},
		},
		{
			name:  "cell count must match",
			lines: []string{"a | b", "---", "1 | 2"},
			want:  nil,
		},
		{
			name:  "two tables",
			lines: []string{"|a|", "|-|", "|1|", "", "|b|c|", "|-|-|", "|2|3|"},
			want: []Table{
				{StartLine: 1, EndLine: 3, Rows: 1, Cols: 1},
				{StartLine: 5, EndLine: 7, Rows: 1, Cols: 2},
			},
		},
		{
			name:  "inside a fence is not a table",
			lines: []string{"```", "| a | b |", "|---|---|", "```"},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := doc(tt.lines...).Tables
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tables = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBarePaths(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  []Span
	}{
		{
			name:  "simple",
			lines: []string{"See docs/foo.md now"},
			want:  []Span{{Line: 1, Col: 5, Text: "docs/foo.md", Kind: "bare"}},
		},
		{
			name:  "dot slash and parenthesised",
			lines: []string{"run ./scripts/verify.py (see internal/model/model.go)"},
			want: []Span{
				{Line: 1, Col: 5, Text: "./scripts/verify.py", Kind: "bare"},
				{Line: 1, Col: 30, Text: "internal/model/model.go", Kind: "bare"},
			},
		},
		{
			name:  "trailing sentence dot trimmed",
			lines: []string{"in docs/a.md."},
			want:  []Span{{Line: 1, Col: 4, Text: "docs/a.md", Kind: "bare"}},
		},
		{
			name:  "urls excluded",
			lines: []string{"https://x.example/a/b is a url"},
			want:  nil,
		},
		{
			name:  "link targets excluded",
			lines: []string{"[a](docs/a.md)"},
			want:  nil,
		},
		{
			name:  "code spans excluded",
			lines: []string{"the " + bt + "docs/a.md" + bt + " file"},
			want:  nil,
		},
		{
			name:  "fences excluded",
			lines: []string{"```", "docs/a.md", "```"},
			want:  nil,
		},
		{
			name:  "comments excluded",
			lines: []string{"<!-- docs/a.md -->"},
			want:  nil,
		},
		{
			name:  "numeric token is not a path",
			lines: []string{"about 1/2 of them"},
			want:  nil,
		},
		{
			name:  "glob kept",
			lines: []string{"match cmd/*/main.go here"},
			want:  []Span{{Line: 1, Col: 7, Text: "cmd/*/main.go", Kind: "bare"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := doc(tt.lines...).BarePaths
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("barepaths = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBarePathSection(t *testing.T) {
	d := doc("## Install", "see docs/a.md")
	if len(d.BarePaths) != 1 || d.BarePaths[0].Section != "Install" {
		t.Errorf("barepaths = %+v", d.BarePaths)
	}
}

func TestLinesPreserved(t *testing.T) {
	src := "# Title\r\n\r\n  indented\r\ntrailing  \r\n"
	d := Parse("a/b.md", []byte(src))
	want := []string{"# Title", "", "  indented", "trailing  "}
	if !reflect.DeepEqual(d.Lines, want) {
		t.Errorf("lines = %q, want %q", d.Lines, want)
	}
	if d.Path != "a/b.md" {
		t.Errorf("path = %q", d.Path)
	}
}

func TestEmptyDocument(t *testing.T) {
	d := Parse("empty.md", nil)
	if len(d.Lines) != 0 || len(d.Headings) != 0 {
		t.Errorf("empty doc = %+v", d)
	}
	if d.Ignored == nil || d.Ignored(1) {
		t.Error("Ignored must be usable on an empty document")
	}
	if got := d.Fingerprint(); got.Headings != nil || got.Numbers != nil {
		t.Errorf("fingerprint = %+v, want zero", got)
	}
}

func TestHTMLBlocksAreProse(t *testing.T) {
	d := doc("<details>", "<summary>More</summary>", "", "see [x](docs/x.md) and "+bt+"pkg.Fn"+bt, "", "</details>")
	if len(d.Links) != 1 || d.Links[0].Target != "docs/x.md" {
		t.Errorf("links = %+v", d.Links)
	}
	if len(d.Spans) != 1 || d.Spans[0].Text != "pkg.Fn" {
		t.Errorf("spans = %+v", d.Spans)
	}
}

func TestFingerprint(t *testing.T) {
	d := doc(
		"# Title",
		"Requires Go 1.26 and 3 workers.",
		"## Sub",
		"[x](http://e.example/1.2.3) and [y](#anchor)",
		"![i](img/a.png)",
		"```go",
		"x := 999   ",
		"```",
		"| a | b |",
		"|---|---|",
		"| 1 | 2 |",
	)
	f := d.Fingerprint()

	wantHeadings := []HeadingSig{{Level: 1, Ordinal: 1}, {Level: 2, Ordinal: 2}}
	if !reflect.DeepEqual(f.Headings, wantHeadings) {
		t.Errorf("headings = %+v, want %+v", f.Headings, wantHeadings)
	}
	if len(f.Codes) != 1 || f.Codes[0].Lang != "go" || len(f.Codes[0].SHA256) != 64 {
		t.Fatalf("codes = %+v", f.Codes)
	}
	// Trailing whitespace in a code line must not change the hash.
	same := doc("```go", "x := 999", "```").Fingerprint()
	if same.Codes[0].SHA256 != f.Codes[0].SHA256 {
		t.Error("trailing whitespace changed the code hash")
	}
	if want := []string{"http://e.example/1.2.3"}; !reflect.DeepEqual(f.Links, want) {
		t.Errorf("links = %v, want %v", f.Links, want)
	}
	if want := []string{"img/a.png"}; !reflect.DeepEqual(f.Images, want) {
		t.Errorf("images = %v, want %v", f.Images, want)
	}
	if want := []TableSig{{Rows: 1, Cols: 2}}; !reflect.DeepEqual(f.Tables, want) {
		t.Errorf("tables = %+v, want %+v", f.Tables, want)
	}
	// 999 lives in a fence, 1.2.3 in a link target; both are excluded.
	if want := []string{"1", "1.26", "2", "3"}; !reflect.DeepEqual(f.Numbers, want) {
		t.Errorf("numbers = %v, want %v", f.Numbers, want)
	}
}

func TestFingerprintPairsDiffer(t *testing.T) {
	en := doc("# Title", "## Usage", "```go", "x", "```")
	zh := doc("# 標題", "## 用法", "```go", "x", "```")
	a, b := en.Fingerprint(), zh.Fingerprint()
	if !reflect.DeepEqual(a.Headings, b.Headings) {
		t.Errorf("translated headings should match structurally: %+v vs %+v", a.Headings, b.Headings)
	}
	if !reflect.DeepEqual(a.Codes, b.Codes) {
		t.Errorf("translated code should match: %+v vs %+v", a.Codes, b.Codes)
	}
	zh2 := doc("# 標題", "```go", "y", "```")
	if reflect.DeepEqual(a.Codes, zh2.Fingerprint().Codes) {
		t.Error("different code should produce different hashes")
	}
}

// A link destination ending in a backslash at end of line used to run the
// destination scanner past the end of the line and panic.
func TestTrailingBackslashInLinkDestination(t *testing.T) {
	for _, line := range []string{`see [a](x\`, `![a](docs\`, `[a](a/b\`} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Parse(%q) panicked: %v", line, r)
				}
			}()
			if d := Parse("t.md", []byte(line)); d == nil {
				t.Errorf("Parse(%q) = nil", line)
			}
		}()
	}
}
