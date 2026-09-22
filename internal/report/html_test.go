package report

import (
	"bytes"
	"strings"
	"testing"

	"docrot/internal/model"
)

// renderHTML runs WriteHTML and returns the page.
func renderHTML(t *testing.T, r *Report, o Options) string {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteHTML(&buf, r, o); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	return buf.String()
}

func TestHTMLStructure(t *testing.T) {
	out := renderHTML(t, sampleReport(), Options{})
	tests := []struct {
		name string
		want string
	}{
		{"doctype", "<!doctype html>"},
		{"title", "<title>docrot report</title>"},
		{"viewport", `name="viewport"`},
		{"version", "version 0.1.0"},
		{"root", "root /repo"},
		{"dark mode", "prefers-color-scheme: dark"},
		{"system fonts", "system-ui"},
		{"summary card", `<div class="l">references</div>`},
		{"reference count", "1,204"},
		{"duration", "0.83s"},
		{"git card", `<div class="l">git</div>`},
		{"extra card", "go packages"},
		{"filter bar", `class="sev-filter"`},
		{"rule select", `<select id="rule">`},
		{"free text filter", `id="q"`},
		{"file heading", "<h3>README.md</h3>"},
		{"severity badge", `<span class="badge error">error</span>`},
		{"suggestion", "(did you mean httpx.WriteData?)"},
		{"context", `<code class="ctx">see `},
		{"pairs section", "<h2>Pairs</h2>"},
		{"coverage section", "<h2>Coverage</h2>"},
		{"coverage bar", `style="width:75%"`},
		{"coverage missing", "missing: httpx.Close"},
		{"footer", "<footer>"},
		{"close", "</html>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(out, tt.want) {
				t.Errorf("HTML missing %q", tt.want)
			}
		})
	}
	if strings.Contains(out, "http://") || strings.Contains(out, "https://") {
		t.Error("HTML report must not reference external resources")
	}
}

func TestHTMLEscapesMessages(t *testing.T) {
	r := &Report{
		Version: "0.1.0",
		Findings: []model.Finding{{
			Rule:       model.RuleMissingPath,
			Severity:   model.SevError,
			Message:    `<script>alert("xss")</script> & "quoted"`,
			Loc:        model.Location{File: `<img src=x onerror=alert(1)>.md`, Line: 1},
			Suggestion: "<b>bold</b>",
			Ref:        &model.Reference{Context: "<script>ctx</script>"},
		}},
	}
	out := renderHTML(t, r, Options{})

	for _, bad := range []string{
		`<script>alert("xss")`,
		"<script>ctx",
		"<b>bold</b>",
		"<img src=x onerror",
	} {
		if strings.Contains(out, bad) {
			t.Errorf("raw %q leaked into the page unescaped", bad)
		}
	}
	for _, want := range []string{
		"&lt;script&gt;alert(",
		"&lt;b&gt;bold&lt;/b&gt;",
		"&lt;img src=x onerror",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML missing escaped form %q", want)
		}
	}
	// The page's own filter script is the only <script> element.
	if got := strings.Count(out, "<script>"); got != 1 {
		t.Errorf("page has %d <script> elements, want 1 (its own filter)", got)
	}
}

func TestHTMLBaselined(t *testing.T) {
	tests := []struct {
		name          string
		opts          Options
		wantContains  []string
		wantOmits     []string
		wantShownText string
	}{
		{
			name:          "hidden by default",
			opts:          Options{},
			wantOmits:     []string{"--verbose", `<span class="tag">baselined</span>`},
			wantShownText: `<span id="count">3</span> shown`,
		},
		{
			name:          "shown on request",
			opts:          Options{ShowBaselined: true},
			wantContains:  []string{"--verbose", `<span class="tag">baselined</span>`},
			wantShownText: `<span id="count">4</span> shown`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderHTML(t, sampleReport(), tt.opts)
			for _, w := range tt.wantContains {
				if !strings.Contains(out, w) {
					t.Errorf("HTML missing %q", w)
				}
			}
			for _, w := range tt.wantOmits {
				if strings.Contains(out, w) {
					t.Errorf("HTML unexpectedly contains %q", w)
				}
			}
			if !strings.Contains(out, tt.wantShownText) {
				t.Errorf("HTML missing %q", tt.wantShownText)
			}
		})
	}
	out := renderHTML(t, sampleReport(), Options{})
	if !strings.Contains(out, "1 baselined hidden") {
		t.Error("HTML should say how many baselined findings were hidden")
	}
}

func TestHTMLEmptyReport(t *testing.T) {
	out := renderHTML(t, &Report{}, Options{})
	if !strings.Contains(out, "No findings.") {
		t.Error("empty report should say so")
	}
	for _, unwanted := range []string{"<h2>Pairs</h2>", "<h2>Coverage</h2>"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("empty report should not render %q", unwanted)
		}
	}
	if !strings.Contains(out, "root (unknown)") {
		t.Error("empty report should fall back to an unknown root")
	}
}

func TestBuildHTMLView(t *testing.T) {
	v := buildHTMLView(sampleReport(), Options{})
	if v.Shown != 3 || v.Hidden != 1 {
		t.Errorf("Shown/Hidden = %d/%d, want 3/1", v.Shown, v.Hidden)
	}
	if len(v.Files) != 3 {
		t.Fatalf("len(Files) = %d, want 3 groups", len(v.Files))
	}
	wantFiles := []string{"README-zh.md", "README.md", "llms.txt"}
	for i, want := range wantFiles {
		if v.Files[i].File != want {
			t.Errorf("Files[%d].File = %q, want %q", i, v.Files[i].File, want)
		}
	}
	if len(v.Pairs) != 1 || v.Pairs[0].Rule != model.RulePairLag {
		t.Errorf("Pairs = %+v, want the one pair-lag finding", v.Pairs)
	}
	wantRules := []string{model.RuleMissingPath, model.RuleMissingSymbol, model.RulePairLag}
	if len(v.Rules) != len(wantRules) {
		t.Fatalf("Rules = %v, want %v", v.Rules, wantRules)
	}
	for i, want := range wantRules {
		if v.Rules[i] != want {
			t.Errorf("Rules[%d] = %q, want %q", i, v.Rules[i], want)
		}
	}
	if v.Coverage == nil || len(v.Coverage.Packages) != 1 {
		t.Fatalf("Coverage = %+v", v.Coverage)
	}
	if v.Coverage.Packages[0].Pct != 75 || string(v.Coverage.Packages[0].BarStyle) != "width:75%" {
		t.Errorf("package coverage = %+v", v.Coverage.Packages[0])
	}
	if v.Coverage.Envs.Pct != 100 {
		t.Errorf("empty env group Pct = %d, want 100", v.Coverage.Envs.Pct)
	}
}

func TestHTMLFilterHaystackIsLowercase(t *testing.T) {
	v := buildHTMLView(sampleReport(), Options{})
	f := v.Files[1].Items[0]
	if f.Filter != strings.ToLower(f.Filter) {
		t.Errorf("Filter = %q, want lower case", f.Filter)
	}
	for _, part := range []string{"readme.md", "missing-symbol", "httpx.writejson", "httpx.writedata"} {
		if !strings.Contains(f.Filter, part) {
			t.Errorf("Filter %q missing %q", f.Filter, part)
		}
	}
}

func TestHTMLCoverageCapsMissing(t *testing.T) {
	missing := make([]string, 15)
	for i := range missing {
		missing[i] = string(rune('a' + i))
	}
	row := htmlCoverageRowOf("pkg", 0, 15, missing)
	if len(row.Missing) != maxMissingListed || row.More != 5 {
		t.Errorf("row = %+v, want %d names and More 5", row, maxMissingListed)
	}
}
