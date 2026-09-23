package pairs

import (
	"strings"
	"testing"

	"docrot/internal/model"
)

func TestGaps(t *testing.T) {
	docs := []string{
		"README.md", "README-zh.md", // suffix pair: never a set
		"docs/en/intro.md", "docs/zh/intro.md", // the pair that makes docs/ a set
		"docs/en/extra.md",     // missing in zh
		"docs/zh/old.md",       // orphan
		"docs/en/deep/page.md", // missing in zh, nested
		"docs/ja/intro.md",     // a second language: its own set
		"docs/ja/only.md",      // orphan in ja
		"docs/zh/notes.rst",    // orphan, not Markdown
		"guide/zh/lonely.md",   // no guide/en/ pair exists: not a set
		"docs/en/README.md", "docs/zh/README.md",
	}
	prs := Detect(docs, nil, defaultPatterns)
	got := Gaps(docs, prs, Options{})
	var lines []string
	for _, f := range got {
		lines = append(lines, f.Rule+" "+f.Loc.File+" "+string(f.Severity)+" "+f.Message)
	}
	want := []string{
		"pair-orphan docs/ja/only.md warning source docs/en/only.md does not exist; the translation is orphaned",
		"pair-missing docs/en/deep/page.md info no zh translation: docs/zh/deep/page.md does not exist",
		"pair-missing docs/en/extra.md info no zh translation: docs/zh/extra.md does not exist",
		"pair-orphan docs/zh/notes.rst warning source docs/en/notes.rst does not exist; the translation is orphaned",
		"pair-orphan docs/zh/old.md warning source docs/en/old.md does not exist; the translation is orphaned",
	}
	// ja is a set too, so every en page without a ja twin is missing there
	for _, d := range []string{"docs/en/README.md", "docs/en/deep/page.md", "docs/en/extra.md"} {
		want = append(want, "pair-missing "+d+" info no ja translation: "+strings.Replace(d, "/en/", "/ja/", 1)+" does not exist")
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d findings, want %d:\n  %s", len(lines), len(want), strings.Join(lines, "\n  "))
	}
	have := map[string]bool{}
	for _, l := range lines {
		have[l] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing finding: %s\ngot:\n  %s", w, strings.Join(lines, "\n  "))
		}
	}
	for _, f := range got {
		if f.Loc.Line != 1 || f.Fingerprint == "" || f.Data["counterpart"] == nil {
			t.Errorf("finding %+v lacks line 1, a fingerprint or the counterpart", f)
		}
	}
	// severity overrides apply
	got = Gaps(docs, prs, Options{Severity: map[string]model.Severity{model.RulePairMissing: model.SevError}})
	for _, f := range got {
		if f.Rule == model.RulePairMissing && f.Severity != model.SevError {
			t.Errorf("override ignored: %+v", f)
		}
	}
}

func TestGapsNothingWithoutDirectoryPairs(t *testing.T) {
	docs := []string{"README.md", "README-zh.md", "docs/zh/alone.md"}
	if got := Gaps(docs, Detect(docs, nil, defaultPatterns), Options{}); len(got) != 0 {
		t.Errorf("suffix pairs and a lone language directory must not produce gaps: %+v", got)
	}
}

func TestDirSet(t *testing.T) {
	cases := []struct {
		src, tr string
		prefix  string
		lang    string
		ok      bool
	}{
		{"docs/en/x.md", "docs/zh/x.md", "docs/", "zh", true},
		{"en/x.md", "zh-TW/x.md", "", "zh-TW", true},
		{"a/b/en/c/x.md", "a/b/ja/c/x.md", "a/b/", "ja", true},
		{"README.md", "README-zh.md", "", "", false},
		{"docs/en/x.md", "docs/zh/y.md", "", "", false},
		{"docs/en/x.md", "docs/fr/x.md", "", "", false},
	}
	for _, c := range cases {
		ts, ok := dirSet(Pair{Source: c.src, Translation: c.tr})
		if ok != c.ok || ts.prefix != c.prefix || ts.lang != c.lang {
			t.Errorf("dirSet(%s, %s) = %+v %v, want %q %q %v", c.src, c.tr, ts, ok, c.prefix, c.lang, c.ok)
		}
	}
}
