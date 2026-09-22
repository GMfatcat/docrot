package report

import (
	"fmt"
	"html/template"
	"io"
	"sort"
	"strconv"
	"strings"

	"docrot/internal/model"
)

// htmlView is the data the HTML template renders. Everything is
// pre-formatted here so the template stays declarative.
type htmlView struct {
	Version  string
	Root     string
	Cards    []htmlCard
	Files    []htmlFileGroup
	Pairs    []htmlFinding
	Rules    []string
	Coverage *htmlCoverage
	Shown    int
	Hidden   int // baselined findings left out of the table
}

// htmlCard is one summary tile.
type htmlCard struct {
	Label string
	Value string
	Tone  string // css modifier: error | warning | info | muted | plain
}

// htmlFileGroup is the findings of one document.
type htmlFileGroup struct {
	File  string
	Items []htmlFinding
}

// htmlFinding is one row of the findings table.
type htmlFinding struct {
	File       string
	Pos        string
	Severity   string
	Rule       string
	Message    string
	Suggestion string
	Context    string
	Baselined  bool
	Filter     string // lower-cased haystack for the free-text filter
}

// htmlCoverage is the coverage section.
type htmlCoverage struct {
	Packages []htmlCoverageRow
	Flags    htmlCoverageRow
	Envs     htmlCoverageRow
}

// htmlCoverageRow is one documented/total bar.
type htmlCoverageRow struct {
	Name       string
	Documented int
	Total      int
	Pct        int
	Missing    []string
	More       int
	BarStyle   template.CSS
}

// WriteHTML writes a self-contained HTML report: inline CSS, a small vanilla
// JavaScript filter bar, no external resources. Baselined findings are
// included only when o.ShowBaselined.
func WriteHTML(w io.Writer, r *Report, o Options) error {
	return htmlTemplate.Execute(w, buildHTMLView(r, o))
}

// buildHTMLView flattens a report into the template's view model.
func buildHTMLView(r *Report, o Options) htmlView {
	v := htmlView{
		Version: r.Version,
		Root:    r.Summary.Root,
		Cards:   htmlCards(r.Summary),
	}
	if v.Root == "" {
		v.Root = o.Root
	}

	rules := make(map[string]struct{})
	var group *htmlFileGroup
	for _, f := range sorted(r) {
		if f.Baselined && !o.ShowBaselined {
			v.Hidden++
			continue
		}
		item := htmlFindingOf(f, o)
		rules[f.Rule] = struct{}{}
		v.Shown++
		if group == nil || group.File != item.File {
			v.Files = append(v.Files, htmlFileGroup{File: item.File})
			group = &v.Files[len(v.Files)-1]
		}
		group.Items = append(group.Items, item)
		if strings.HasPrefix(f.Rule, "pair-") {
			v.Pairs = append(v.Pairs, item)
		}
	}
	for rule := range rules {
		v.Rules = append(v.Rules, rule)
	}
	sort.Strings(v.Rules)
	v.Coverage = htmlCoverageOf(r.Coverage)
	return v
}

// htmlFindingOf converts one finding into a table row.
func htmlFindingOf(f model.Finding, o Options) htmlFinding {
	file := relPath(o.Root, f.Loc.File)
	pos := ""
	if f.Loc.Line > 0 {
		pos = strconv.Itoa(f.Loc.Line)
		if f.Loc.Col > 0 {
			pos += ":" + strconv.Itoa(f.Loc.Col)
		}
	}
	ctx := ""
	if f.Ref != nil {
		ctx = f.Ref.Context
	}
	return htmlFinding{
		File:       file,
		Pos:        pos,
		Severity:   string(f.Severity),
		Rule:       f.Rule,
		Message:    f.Message,
		Suggestion: f.Suggestion,
		Context:    ctx,
		Baselined:  f.Baselined,
		Filter: strings.ToLower(strings.Join(
			[]string{file, f.Rule, f.Message, f.Suggestion, ctx}, " ")),
	}
}

// htmlCards builds the summary tiles, in display order.
func htmlCards(s Summary) []htmlCard {
	cards := []htmlCard{
		{Label: "errors", Value: humanInt(s.Errors), Tone: "error"},
		{Label: "warnings", Value: humanInt(s.Warnings), Tone: "warning"},
		{Label: "info", Value: humanInt(s.Infos), Tone: "info"},
		{Label: "baselined", Value: humanInt(s.Baselined), Tone: "muted"},
		{Label: "fixed", Value: humanInt(s.Fixed), Tone: "muted"},
		{Label: "docs", Value: humanInt(s.Docs), Tone: "plain"},
		{Label: "references", Value: humanInt(s.References), Tone: "plain"},
		{Label: "duration", Value: humanDuration(s.Duration), Tone: "plain"},
	}
	git := s.Git
	if git == "" {
		git = GitUnavailable
	}
	tone := "plain"
	if git != GitOK {
		tone = "muted"
	}
	cards = append(cards, htmlCard{Label: "git", Value: git, Tone: tone})
	for _, k := range sortedKeys(s.Extra) {
		cards = append(cards, htmlCard{Label: k, Value: s.Extra[k], Tone: "plain"})
	}
	return cards
}

// htmlCoverageOf converts the coverage section, capping the listed missing
// names the same way the text report does.
func htmlCoverageOf(c *Coverage) *htmlCoverage {
	if c == nil {
		return nil
	}
	out := &htmlCoverage{
		Flags: htmlCoverageRowOf("flags", c.Flags.Documented, c.Flags.Total, c.Flags.Missing),
		Envs:  htmlCoverageRowOf("envs", c.Envs.Documented, c.Envs.Total, c.Envs.Missing),
	}
	for _, p := range c.Packages {
		out.Packages = append(out.Packages, htmlCoverageRowOf(p.Package, p.Documented, p.Total, p.Missing))
	}
	return out
}

func htmlCoverageRowOf(name string, documented, total int, missing []string) htmlCoverageRow {
	p := pct(documented, total)
	row := htmlCoverageRow{
		Name:       name,
		Documented: documented,
		Total:      total,
		Pct:        p,
		Missing:    missing,
		BarStyle:   template.CSS(fmt.Sprintf("width:%d%%", p)),
	}
	if len(row.Missing) > maxMissingListed {
		row.More = len(row.Missing) - maxMissingListed
		row.Missing = row.Missing[:maxMissingListed]
	}
	return row
}
