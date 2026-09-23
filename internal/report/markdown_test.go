package report

import (
	"bytes"
	"strings"
	"testing"

	"docrot/internal/model"
)

// renderMD renders a report as Markdown or fails the test.
func renderMD(t *testing.T, r *Report, o Options) string {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteMarkdown(&buf, r, o); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	return buf.String()
}

// indexOf returns the byte offset of sub, failing the test when it is
// absent — so section-order assertions report the missing part, not -1.
func indexOf(t *testing.T, s, sub string) int {
	t.Helper()
	i := strings.Index(s, sub)
	if i < 0 {
		t.Fatalf("output is missing %q\n---\n%s", sub, s)
	}
	return i
}

func TestWriteMarkdownStructure(t *testing.T) {
	out := renderMD(t, sampleReport(), Options{Root: "/repo"})

	// The sections must appear once each, in the documented order.
	sections := []string{
		"# docrot report",
		"## How to read this",
		"## Findings",
		"## Baselined (pre-existing)",
		"## Rules seen",
		"## Coverage",
		"## Fix checklist",
	}
	prev := -1
	for _, s := range sections {
		at := indexOf(t, out, s)
		if at < prev {
			t.Errorf("section %q is out of order", s)
		}
		if n := strings.Count(out, s); n != 1 {
			t.Errorf("section %q appears %d times, want 1", s, n)
		}
		prev = at
	}

	// The summary line is the text report's, word for word.
	if want := SummaryLine(sampleReport().Summary); !strings.Contains(out, want) {
		t.Errorf("summary line %q missing", want)
	}
	for _, want := range []string{
		"- root: `/repo`",
		"- git: ok",
		"- docrot: 0.1.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("header is missing %q", want)
		}
	}

	// How to read this: 4–6 bullets, and every promise it makes.
	how := section(out, "## How to read this")
	if n := strings.Count(how, "\n- "); n < 4 || n > 6 {
		t.Errorf("How to read this has %d bullets, want 4–6:\n%s", n, how)
	}
	for _, want := range []string{"suggestion:", "context:", "docrot:ignore", ".docrot.json", "docrot check", "**error**"} {
		if !strings.Contains(how, want) {
			t.Errorf("How to read this never mentions %q", want)
		}
	}

	// Findings are grouped by file, each as one list item.
	for _, want := range []string{
		"### README.md",
		"### llms.txt",
		"### README-zh.md",
		"- L42:15 **error** `missing-symbol` — `httpx.WriteJSON` not found in package httpx",
		"  - suggestion: httpx.WriteData",
		"  - context: `see 'httpx.WriteJSON'`",
		"- L12:3 **warning** `missing-path` — `docs/contract.md` not found",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("findings are missing %q\n---\n%s", want, out)
		}
	}

	// Rules seen is a table of the rules this run produced, and nothing else.
	rules := section(out, "## Rules seen")
	if !strings.Contains(rules, "| rule | what it means |") {
		t.Error("Rules seen is not a table")
	}
	if !strings.Contains(rules, "| `missing-symbol` | "+model.RuleDescriptions[model.RuleMissingSymbol]+" |") {
		t.Errorf("Rules seen lacks the missing-symbol description:\n%s", rules)
	}
	if strings.Contains(rules, model.RuleBrokenURL) {
		t.Error("Rules seen lists a rule this run never produced")
	}
	// The baselined finding's rule is hidden with the finding itself.
	if strings.Contains(rules, model.RuleUnknownFlag) {
		t.Errorf("Rules seen lists a hidden baselined rule:\n%s", rules)
	}

	// Coverage carries the same figures as the text report.
	cov := section(out, "## Coverage")
	for _, want := range []string{"- httpx: 3/4 (75%) missing: httpx.Close", "- flags: 1/2 (50%)", "- envs: 0/0 (100%)"} {
		if !strings.Contains(cov, want) {
			t.Errorf("Coverage is missing %q:\n%s", want, cov)
		}
	}

	// The checklist is one box per (file, rule), most severe first.
	list := section(out, "## Fix checklist")
	wantOrder := []string{
		"- [ ] `README.md` — `missing-symbol` × 1 (error)",
		"- [ ] `llms.txt` — `missing-path` × 1 (warning)",
		"- [ ] `README-zh.md` — `pair-lag` × 1 (info)",
	}
	prev = -1
	for _, want := range wantOrder {
		at := indexOf(t, list, want)
		if at < prev {
			t.Errorf("checklist entry %q is out of severity order:\n%s", want, list)
		}
		prev = at
	}
}

// section returns the part of out starting at heading and ending before the
// next "## " heading.
func section(out, heading string) string {
	i := strings.Index(out, heading)
	if i < 0 {
		return ""
	}
	rest := out[i+len(heading):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// A finding message may contain "<", which would start a raw HTML tag (and
// an HTML comment would swallow the rest of the line), and a context line
// is quoted inside a code span, which cannot hold a bare backtick.
func TestWriteMarkdownEscaping(t *testing.T) {
	r := &Report{
		Summary: Summary{Root: "/repo", Git: GitOK},
		Findings: []model.Finding{{
			Rule:     model.RuleMissingPath,
			Severity: model.SevError,
			Message:  "`docs/<name>.md` not found; see <!-- hidden -->",
			Loc:      model.Location{File: "README.md", Line: 3, Col: 1},
			Ref: &model.Reference{
				Kind:    model.KindPath,
				Text:    "docs/<name>.md",
				Context: "run `docrot check` on `docs/<name>.md` first",
			},
			Suggestion: "docs/<name>-zh.md",
		}},
	}
	out := renderMD(t, r, Options{Root: "/repo"})

	for _, want := range []string{
		// Prose: angle brackets escaped, so the HTML comment cannot swallow
		// the rest of the line and "<name>" is not read as a tag.
		"- L3:1 **error** `missing-path` — `docs/&lt;name&gt;.md` not found; see &lt;!-- hidden --&gt;",
		"  - suggestion: docs/&lt;name&gt;-zh.md",
		// Code span: backticks become apostrophes, angle brackets stay
		// literal, which is exactly how a code span renders them.
		"  - context: `run 'docrot check' on 'docs/<name>.md' first`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	// No prose line may carry a raw "<": the report is rendered as Markdown.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "- L") && strings.Contains(line, "<") {
			t.Errorf("a raw \"<\" survived into a finding line: %q", line)
		}
		// The context code span must open and close on its own line; an odd
		// number of backticks would leak into the rest of the document.
		if strings.HasPrefix(line, "  - context: ") {
			if n := strings.Count(line, "`"); n != 2 {
				t.Errorf("context line has %d backticks, want 2: %q", n, line)
			}
		}
	}
}

// The agent report is rewritten on every run; two runs over the same data
// must produce the same bytes, or every run looks like a change.
func TestWriteMarkdownDeterministic(t *testing.T) {
	o := Options{Root: "/repo", ShowBaselined: true}
	a := renderMD(t, sampleReport(), o)
	b := renderMD(t, sampleReport(), o)
	if a != b {
		t.Error("two renders of the same report differ")
	}
	// Input order must not matter either: Sort decides the layout.
	r := sampleReport()
	f := r.Findings
	for i, j := 0, len(f)-1; i < j; i, j = i+1, j-1 {
		f[i], f[j] = f[j], f[i]
	}
	if c := renderMD(t, r, o); c != a {
		t.Error("reversing the input findings changed the output")
	}
}

// Info findings are hidden from the terminal by default; the agent report
// always lists them, because an agent can weigh a weak signal itself.
func TestWriteMarkdownIncludesInfo(t *testing.T) {
	for _, showInfo := range []bool{false, true} {
		out := renderMD(t, sampleReport(), Options{Root: "/repo", ShowInfo: showInfo})
		if !strings.Contains(out, "**info** `pair-lag`") {
			t.Errorf("ShowInfo=%v: the info finding is missing:\n%s", showInfo, out)
		}
		if !strings.Contains(out, "info included") {
			t.Errorf("ShowInfo=%v: the findings section does not say info is included", showInfo)
		}
	}
}

func TestWriteMarkdownBaselined(t *testing.T) {
	const listed = "**warning** `unknown-flag` — `--verbose` is not defined"

	hidden := renderMD(t, sampleReport(), Options{Root: "/repo"})
	sec := section(hidden, "## Baselined (pre-existing)")
	if strings.Contains(sec, listed) {
		t.Errorf("a baselined finding was listed without ShowBaselined:\n%s", sec)
	}
	if !strings.Contains(sec, "1 finding frozen by") || !strings.Contains(sec, "docrot check --all") {
		t.Errorf("the collapsed baseline section should count and explain:\n%s", sec)
	}

	shown := renderMD(t, sampleReport(), Options{Root: "/repo", ShowBaselined: true})
	sec = section(shown, "## Baselined (pre-existing)")
	if !strings.Contains(sec, "README.md L8:1 "+listed) {
		t.Errorf("ShowBaselined did not list the finding on one line:\n%s", sec)
	}
	// A baselined finding is not breakage, so it stays out of the to-do list.
	if strings.Contains(section(shown, "## Fix checklist"), "unknown-flag") {
		t.Error("a baselined finding leaked into the fix checklist")
	}
	// Its rule does become explainable once it is shown.
	if !strings.Contains(section(shown, "## Rules seen"), "`unknown-flag`") {
		t.Error("ShowBaselined did not add unknown-flag to Rules seen")
	}
}

// A clean run still has to be readable: no findings, no empty sections.
func TestWriteMarkdownEmpty(t *testing.T) {
	r := &Report{Summary: Summary{Root: "/repo", Docs: 3, Git: GitDisabled}, Version: "test"}
	out := renderMD(t, r, Options{Root: "/repo"})
	if !strings.Contains(out, "None. Every claim") {
		t.Errorf("a clean run should say so:\n%s", out)
	}
	for _, unwanted := range []string{"## Baselined", "## Rules seen", "## Coverage", "## Fix checklist"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("a clean run should not emit %q:\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "- git: disabled") {
		t.Errorf("the git state should be explained:\n%s", out)
	}
}

// Write must dispatch "md" like any other format name.
func TestWriteDispatchesMarkdown(t *testing.T) {
	if !contains(Formats, "md") {
		t.Fatalf("Formats does not list md: %v", Formats)
	}
	var buf bytes.Buffer
	if err := Write("md", &buf, sampleReport(), Options{Root: "/repo"}); err != nil {
		t.Fatalf("Write(md): %v", err)
	}
	if !strings.HasPrefix(buf.String(), "# docrot report\n") {
		t.Errorf("Write(md) did not produce the Markdown report:\n%s", buf.String())
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
