package pairs

import (
	"reflect"
	"testing"
)

// defaultPatterns mirrors the shipped configuration defaults.
var defaultPatterns = []string{
	"{stem}-zh.md", "{stem}_zh.md", "{stem}.zh.md", "{stem}.zh-TW.md", "{stem}-zh-TW.md",
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name     string
		docs     []string
		explicit []Pair
		patterns []string
		want     []Pair
	}{
		{
			name: "no candidates",
			docs: []string{"README.md", "docs/a.md"},
			want: nil,
		},
		{
			name:     "suffix patterns at the repo root and in a directory",
			docs:     []string{"README.md", "README-zh.md", "docs/a.md", "docs/a.zh-TW.md"},
			patterns: defaultPatterns,
			want: []Pair{
				{Source: "README.md", Translation: "README-zh.md"},
				{Source: "docs/a.md", Translation: "docs/a.zh-TW.md"},
			},
		},
		{
			name:     "one source with several translations",
			docs:     []string{"README.md", "README-zh.md", "README.zh-TW.md"},
			patterns: defaultPatterns,
			want: []Pair{
				{Source: "README.md", Translation: "README-zh.md"},
				{Source: "README.md", Translation: "README.zh-TW.md"},
			},
		},
		{
			name:     "pattern without the placeholder is ignored",
			docs:     []string{"README.md", "README-zh.md"},
			patterns: []string{"README-zh.md"},
			want:     nil,
		},
		{
			name:     "a pattern may not pair a document with itself",
			docs:     []string{"README.md"},
			patterns: []string{"{stem}.md"},
			want:     nil,
		},
		{
			name:     "missing translation is not invented",
			docs:     []string{"README.md"},
			patterns: defaultPatterns,
			want:     nil,
		},
		{
			name: "directory convention at any depth",
			docs: []string{
				"docs/en/guide.md", "docs/zh/guide.md",
				"en/intro.md", "ja/intro.md",
				"site/en/deep/x.md", "site/ko/deep/x.md",
			},
			want: []Pair{
				{Source: "docs/en/guide.md", Translation: "docs/zh/guide.md"},
				{Source: "en/intro.md", Translation: "ja/intro.md"},
				{Source: "site/en/deep/x.md", Translation: "site/ko/deep/x.md"},
			},
		},
		{
			name:     "explicit pair wins over the pattern (first match wins)",
			docs:     []string{"x.md", "y.md", "x-zh.md"},
			explicit: []Pair{{Source: "y.md", Translation: "x-zh.md"}},
			patterns: defaultPatterns,
			want:     []Pair{{Source: "y.md", Translation: "x-zh.md"}},
		},
		{
			name:     "explicit pair is not duplicated by a pattern",
			docs:     []string{"README.md", "README-zh.md"},
			explicit: []Pair{{Source: "README.md", Translation: "README-zh.md"}},
			patterns: defaultPatterns,
			want:     []Pair{{Source: "README.md", Translation: "README-zh.md"}},
		},
		{
			name:     "explicit pair whose files are absent is dropped",
			docs:     []string{"README.md"},
			explicit: []Pair{{Source: "README.md", Translation: "README-zh.md"}},
			want:     nil,
		},
		{
			name:     "paths are normalised before matching",
			docs:     []string{"./README.md", `docs\a.md`, "docs/a-zh.md"},
			explicit: []Pair{{Source: "./README.md", Translation: "docs/a-zh.md"}},
			patterns: defaultPatterns,
			want:     []Pair{{Source: "README.md", Translation: "docs/a-zh.md"}},
		},
		{
			name: "result is sorted by source then translation",
			docs: []string{"b.md", "b-zh.md", "a.md", "a.zh-TW.md", "a-zh.md"},
			patterns: []string{
				"{stem}.zh-TW.md", "{stem}-zh.md",
			},
			want: []Pair{
				{Source: "a.md", Translation: "a-zh.md"},
				{Source: "a.md", Translation: "a.zh-TW.md"},
				{Source: "b.md", Translation: "b-zh.md"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Detect(tt.docs, tt.explicit, tt.patterns)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Detect() = %v\nwant %v", got, tt.want)
			}
		})
	}
}

func TestLangDirs(t *testing.T) {
	got := langDirs("docs/en/a.md")
	want := []string{
		"docs/zh/a.md", "docs/zh-TW/a.md", "docs/zh-CN/a.md", "docs/ja/a.md", "docs/ko/a.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("langDirs() = %v\nwant %v", got, want)
	}
	if got := langDirs("docs/english/a.md"); got != nil {
		t.Errorf("langDirs() = %v, want nil for a segment that is not exactly \"en\"", got)
	}
}
