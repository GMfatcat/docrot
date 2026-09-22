package pairs

import (
	"reflect"
	"strings"
	"testing"

	"docrot/internal/markdown"
	"docrot/internal/model"
)

// readmePair is the pair used by the structural tests.
var readmePair = Pair{Source: "README.md", Translation: "README-zh.md"}

// doc parses the given lines as one Markdown document.
func doc(path string, lines ...string) *markdown.Doc {
	return markdown.Parse(path, []byte(strings.Join(lines, "\n")+"\n"))
}

// full is a document exercising every structure the pair rules look at.
var full = []string{
	"# Title",
	"",
	"Release 1.2.3 listens on port 8080.",
	"",
	"## Usage",
	"",
	"```sh",
	"run --addr :8080",
	"```",
	"",
	"| name | kind |",
	"| --- | --- |",
	"| a | b |",
	"",
	"See [guide](docs/guide.md).",
}

// got is the part of a finding the structural tests assert on.
type got struct {
	Rule string
	Line int
	Msg  string
}

func summarize(fs []model.Finding) []got {
	var out []got
	for _, f := range fs {
		out = append(out, got{Rule: f.Rule, Line: f.Loc.Line, Msg: f.Message})
	}
	return out
}

func TestCompareStructure(t *testing.T) {
	tests := []struct {
		name string
		pair Pair
		src  []string
		tr   []string
		want []got
	}{
		{
			name: "identical documents",
			pair: readmePair,
			src:  full,
			tr:   full,
			want: nil,
		},
		{
			name: "heading count",
			pair: readmePair,
			src:  []string{"# A", "", "## B"},
			tr:   []string{"# A"},
			want: []got{{model.RulePairHeading, 1, "translation has 1 heading, source has 2"}},
		},
		{
			name: "heading level of the first differing ordinal",
			pair: readmePair,
			src:  []string{"# A", "", "## B", "", "## C"},
			tr:   []string{"# A", "", "### B", "", "### C"},
			want: []got{{model.RulePairHeading, 3, "heading #2 is level 3 in translation but level 2 in source"}},
		},
		{
			name: "code block count",
			pair: readmePair,
			src:  []string{"# A", "", "```go", "x", "```", "", "```sh", "y", "```"},
			tr:   []string{"# A", "", "```go", "x", "```"},
			want: []got{{model.RulePairCode, 1, "translation has 1 code block, source has 2"}},
		},
		{
			name: "code block content",
			pair: readmePair,
			src:  []string{"# A", "", "```sh", "run a", "```"},
			tr:   []string{"# A", "", "```sh", "run b", "```"},
			want: []got{{model.RulePairCode, 3, "code block #1 (sh) differs from source"}},
		},
		{
			name: "code block without a language",
			pair: readmePair,
			src:  []string{"# A", "", "```", "run a", "```"},
			tr:   []string{"# A", "", "```", "run b", "```"},
			want: []got{{model.RulePairCode, 3, "code block #1 differs from source"}},
		},
		{
			name: "trailing whitespace does not count as a code difference",
			pair: readmePair,
			src:  []string{"# A", "", "```sh", "run a", "```"},
			tr:   []string{"# A", "", "```sh", "run a   ", "```"},
			want: nil,
		},
		{
			name: "link only in source",
			pair: readmePair,
			src:  []string{"# A", "", "See [x](docs/x.md)."},
			tr:   []string{"# A"},
			want: []got{{model.RulePairLink, 1, "link only in source: docs/x.md"}},
		},
		{
			name: "link only in translation",
			pair: readmePair,
			src:  []string{"# A"},
			tr:   []string{"# A", "", "See [x](docs/x.md)."},
			want: []got{{model.RulePairLink, 1, "link only in translation: docs/x.md"}},
		},
		{
			name: "links between the two files of the pair are ignored",
			pair: readmePair,
			src:  []string{"# A", "", "[zh](README-zh.md)"},
			tr:   []string{"# A", "", "[en](../README.md#usage)"},
			want: nil,
		},
		{
			name: "table count",
			pair: readmePair,
			src: []string{"# A", "", "| a | b |", "| --- | --- |", "| c | d |", "",
				"| e | f |", "| --- | --- |", "| g | h |"},
			tr:   []string{"# A", "", "| a | b |", "| --- | --- |", "| c | d |"},
			want: []got{{model.RulePairTable, 1, "translation has 1 table, source has 2"}},
		},
		{
			name: "table shape",
			pair: readmePair,
			src:  []string{"# A", "", "| a | b |", "| --- | --- |", "| c | d |", "| e | f |"},
			tr:   []string{"# A", "", "| a | b |", "| --- | --- |", "| c | d |"},
			want: []got{{model.RulePairTable, 3, "table #1 is 1×2 in translation but 2×2 in source"}},
		},
		{
			name: "numbers only in source",
			pair: readmePair,
			src:  []string{"# A", "", "Release 1.2.3 on port 8080."},
			tr:   []string{"# A", "", "Release 1.2.3."},
			want: []got{{model.RulePairNumber, 1, "numbers only in source: 8080"}},
		},
		{
			name: "numbers on both sides",
			pair: readmePair,
			src:  []string{"# A", "", "Release 1.2.3 on port 8080."},
			tr:   []string{"# A", "", "Release 2.0.0 on port 9090."},
			want: []got{
				{model.RulePairNumber, 1, "numbers only in source: 1.2.3, 8080"},
				{model.RulePairNumber, 1, "numbers only in translation: 2.0.0, 9090"},
			},
		},
		{
			name: "several rules at once, in rule order",
			pair: readmePair,
			src:  []string{"# A", "", "```sh", "run a", "```", "", "See [x](docs/x.md).", "", "Port 8080."},
			tr:   []string{"# A", "", "```sh", "run b", "```"},
			want: []got{
				{model.RulePairCode, 3, "code block #1 (sh) differs from source"},
				{model.RulePairLink, 1, "link only in source: docs/x.md"},
				{model.RulePairNumber, 1, "numbers only in source: 8080"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := doc(tt.pair.Source, tt.src...)
			tr := doc(tt.pair.Translation, tt.tr...)
			findings := Compare(tt.pair, src, tr, Options{})
			if g := summarize(findings); !reflect.DeepEqual(g, tt.want) {
				t.Errorf("Compare() = %+v\nwant %+v", g, tt.want)
			}
			for _, f := range findings {
				if f.Loc.File != tt.pair.Translation {
					t.Errorf("finding %q reported on %q, want the translation", f.Rule, f.Loc.File)
				}
				if f.Fingerprint == "" {
					t.Errorf("finding %q has no fingerprint", f.Rule)
				}
			}
		})
	}
}

func TestCompareSeverityAndFingerprint(t *testing.T) {
	src := doc("README.md", "# A", "", "See [x](docs/x.md).", "", "Port 8080.")
	tr := doc("README-zh.md", "# A")

	fs := Compare(readmePair, src, tr, Options{
		Severity: map[string]model.Severity{model.RulePairLink: model.SevError},
	})
	if len(fs) != 2 {
		t.Fatalf("Compare() = %d findings, want 2", len(fs))
	}
	if fs[0].Severity != model.SevError {
		t.Errorf("pair-link severity = %q, want the override %q", fs[0].Severity, model.SevError)
	}
	want := model.Fingerprint(model.RulePairLink, "README-zh.md", "docs/x.md")
	if fs[0].Fingerprint != want {
		t.Errorf("pair-link fingerprint = %q, want %q", fs[0].Fingerprint, want)
	}
	if fs[1].Severity != model.SevInfo {
		t.Errorf("pair-number severity = %q, want the default %q", fs[1].Severity, model.SevInfo)
	}
}

func TestCompareNilDocs(t *testing.T) {
	d := doc("README.md", "# A")
	if fs := Compare(readmePair, nil, d, Options{}); fs != nil {
		t.Errorf("Compare(nil source) = %v, want nil", fs)
	}
	if fs := Compare(readmePair, d, nil, Options{}); fs != nil {
		t.Errorf("Compare(nil translation) = %v, want nil", fs)
	}
}
