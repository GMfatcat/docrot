package report

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"docrot/internal/model"
)

// WriteMarkdown writes the agent-facing report: the same findings as every
// other writer, but laid out so that a language model which has never seen
// docrot can act on it without further explanation.
//
// The output is deterministic for a given Report — no colour, no wall-clock
// timestamps, findings ordered by Sort — so two runs over an unchanged
// repository produce byte-identical files. Info-level findings are always
// included: they are weak signals a human would rather not read, but an
// agent can weigh them itself.
//
// Baselined findings are listed separately, and only when o.ShowBaselined;
// otherwise the section shrinks to a count.
func WriteMarkdown(w io.Writer, r *Report, o Options) error {
	bw := bufio.NewWriter(w)
	var live, base []model.Finding
	for _, f := range sorted(r) {
		if f.Baselined {
			base = append(base, f)
		} else {
			live = append(live, f)
		}
	}
	mdHeader(bw, r, o)
	mdHowToRead(bw)
	mdFindings(bw, live, o)
	mdBaselined(bw, base, o)
	mdRules(bw, live, base, o)
	mdCoverage(bw, r.Coverage)
	mdChecklist(bw, live, o)
	return bw.Flush()
}

// mdHeader writes the title, the one-line summary and the run's context.
func mdHeader(w io.Writer, r *Report, o Options) {
	fmt.Fprintln(w, "# docrot report")
	fmt.Fprintln(w)
	fmt.Fprintln(w, mdText(SummaryLine(r.Summary)))
	fmt.Fprintln(w)
	root := r.Summary.Root
	if root == "" {
		root = o.Root
	}
	fmt.Fprintf(w, "- root: `%s`\n", mdCode(filepath.ToSlash(root)))
	fmt.Fprintf(w, "- git: %s\n", mdGitNote(r.Summary.Git))
	if r.Version != "" {
		fmt.Fprintf(w, "- docrot: %s\n", mdText(r.Version))
	}
	fmt.Fprintln(w)
}

// mdGitNote spells out what the git state meant for this run, because the
// bare word says nothing to a reader who does not know the rules.
func mdGitNote(state string) string {
	switch state {
	case GitOK:
		return "ok — git history was available, so the stale-section and pair-lag rules ran"
	case GitDisabled:
		return "disabled — `--no-git` was passed, so the git-based rules did not run"
	}
	return "unavailable — no git repository or no git binary, so the git-based rules did not run"
}

// mdHowToRead is the fixed preamble that makes the rest of the file
// self-explanatory. Everything it promises must stay true of the layout
// below it.
func mdHowToRead(w io.Writer) {
	fmt.Fprintln(w, "## How to read this")
	fmt.Fprintln(w)
	for _, b := range []string{
		"Every finding is one claim a document makes about this repository that docrot could not verify against the code: a file path, a code symbol, a CLI flag, an environment variable, a config key, a heading anchor, a shell command or a Go import. `L<line>:<col>` points at the character in the document, not in the code.",
		"Severity says how confident docrot is that the document is wrong, not how urgent the fix is: **error** — the reference was specific and definitely does not exist; **warning** — probable, but the reference could be about another program; **info** — a weak signal, worth a glance. Every finding still needs a judgement call.",
		"A `suggestion:` line is a did-you-mean, guessed from edit distance and git rename history over the names that do exist. Treat it as the most likely correction, never as a verified one; `context:` quotes the source line the claim came from, with backticks replaced by apostrophes.",
		"Findings under \"Baselined (pre-existing)\" were already there when the baseline was frozen. They are known debt rather than new breakage, and they never fail a run.",
		"To silence a false positive, put `<!-- docrot:ignore -->` on its own line directly before the offending line, or add a regular expression matching the reference text to the `ignore` list in `.docrot.json`.",
		"Re-run `docrot check` after editing the documents; this file is rewritten from scratch on every run, so a finding that disappears is a finding that is fixed.",
	} {
		fmt.Fprintln(w, "- "+b)
	}
	fmt.Fprintln(w)
}

// mdFindings writes the non-baselined findings, grouped by document.
func mdFindings(w io.Writer, findings []model.Finding, o Options) {
	fmt.Fprintln(w, "## Findings")
	fmt.Fprintln(w)
	if len(findings) == 0 {
		fmt.Fprintln(w, "None. Every claim these documents make checks out.")
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, "%s in %s, info included.\n", plural(len(findings), "finding"), plural(mdDocCount(findings, o), "document"))
	file := ""
	for _, f := range findings {
		if p := relPath(o.Root, f.Loc.File); p != file {
			file = p
			fmt.Fprintln(w)
			fmt.Fprintf(w, "### %s\n", mdText(file))
			fmt.Fprintln(w)
		}
		mdFinding(w, f)
	}
	fmt.Fprintln(w)
}

// mdDocCount counts the distinct documents the findings sit in.
func mdDocCount(findings []model.Finding, o Options) int {
	seen := map[string]bool{}
	for _, f := range findings {
		seen[relPath(o.Root, f.Loc.File)] = true
	}
	return len(seen)
}

// mdFinding writes one finding as a list item plus its optional detail
// lines, indented two spaces so they nest under it.
func mdFinding(w io.Writer, f model.Finding) {
	fmt.Fprintf(w, "- %s **%s** `%s` — %s\n",
		mdPos(f.Loc), f.Severity, mdCode(f.Rule), mdText(f.Message))
	if f.Suggestion != "" {
		fmt.Fprintf(w, "  - suggestion: %s\n", mdText(f.Suggestion))
	}
	if f.Ref != nil && strings.TrimSpace(f.Ref.Context) != "" {
		fmt.Fprintf(w, "  - context: `%s`\n", mdCode(f.Ref.Context))
	}
}

// mdPos renders a location as "L12:3", "L12" or "(whole file)".
func mdPos(l model.Location) string {
	switch {
	case l.Line == 0:
		return "(whole file)"
	case l.Col == 0:
		return "L" + strconv.Itoa(l.Line)
	}
	return "L" + strconv.Itoa(l.Line) + ":" + strconv.Itoa(l.Col)
}

// mdBaselined writes the pre-existing findings, one line each, or just
// their count when o.ShowBaselined is off. Nothing is written when the
// baseline matched nothing.
func mdBaselined(w io.Writer, base []model.Finding, o Options) {
	if len(base) == 0 {
		return
	}
	fmt.Fprintln(w, "## Baselined (pre-existing)")
	fmt.Fprintln(w)
	if !o.ShowBaselined {
		fmt.Fprintf(w, "%s frozen by `.docrot-baseline.json`, not listed here. Run `docrot check --all` to see them.\n",
			plural(len(base), "finding"))
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, "%s frozen by `.docrot-baseline.json`. They do not fail a run.\n", plural(len(base), "finding"))
	fmt.Fprintln(w)
	for _, f := range base {
		fmt.Fprintf(w, "- %s %s **%s** `%s` — %s\n",
			mdText(relPath(o.Root, f.Loc.File)), mdPos(f.Loc), f.Severity, mdCode(f.Rule), mdText(f.Message))
	}
	fmt.Fprintln(w)
}

// mdRules writes the description table for the rules this run actually
// produced, so the reader never has to look a rule id up elsewhere.
func mdRules(w io.Writer, live, base []model.Finding, o Options) {
	seen := map[string]bool{}
	for _, f := range live {
		seen[f.Rule] = true
	}
	if o.ShowBaselined {
		for _, f := range base {
			seen[f.Rule] = true
		}
	}
	if len(seen) == 0 {
		return
	}
	rules := make([]string, 0, len(seen))
	for r := range seen {
		rules = append(rules, r)
	}
	sort.Strings(rules)
	fmt.Fprintln(w, "## Rules seen")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| rule | what it means |")
	fmt.Fprintln(w, "|---|---|")
	for _, r := range rules {
		desc := model.RuleDescriptions[r]
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(w, "| `%s` | %s |\n", mdCode(r), mdCell(desc))
	}
	fmt.Fprintln(w)
}

// mdCoverage writes the same figures as the text report's coverage table.
func mdCoverage(w io.Writer, cov *Coverage) {
	if cov == nil {
		return
	}
	fmt.Fprintln(w, "## Coverage")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exported symbols, flags, environment variables, routes and config keys no document mentions.")
	fmt.Fprintln(w)
	for _, p := range cov.Packages {
		fmt.Fprintf(w, "- %s: %s\n", mdText(p.Package), mdText(coverageCell(p.Documented, p.Total, p.Missing)))
	}
	fmt.Fprintf(w, "- flags: %s\n", mdText(coverageCell(cov.Flags.Documented, cov.Flags.Total, cov.Flags.Missing)))
	fmt.Fprintf(w, "- envs: %s\n", mdText(coverageCell(cov.Envs.Documented, cov.Envs.Total, cov.Envs.Missing)))
	if cov.Routes.Total > 0 {
		fmt.Fprintf(w, "- routes: %s\n", mdText(coverageCell(cov.Routes.Documented, cov.Routes.Total, cov.Routes.Missing)))
	}
	if cov.Configs.Total > 0 {
		fmt.Fprintf(w, "- config keys: %s\n", mdText(coverageCell(cov.Configs.Documented, cov.Configs.Total, cov.Configs.Missing)))
	}
	fmt.Fprintln(w)
}

// mdTask is one (file, rule) pair of the fix checklist.
type mdTask struct {
	File  string
	Rule  string
	Count int
	Sev   model.Severity // the most severe finding in the group
}

// mdChecklist writes the to-do list: one box per distinct (file, rule)
// pair, most severe first, so an agent can work down it.
func mdChecklist(w io.Writer, findings []model.Finding, o Options) {
	if len(findings) == 0 {
		return
	}
	index := map[string]int{}
	var tasks []mdTask
	for _, f := range findings {
		file := relPath(o.Root, f.Loc.File)
		key := file + "\x00" + f.Rule
		if i, ok := index[key]; ok {
			tasks[i].Count++
			if f.Severity.Rank() > tasks[i].Sev.Rank() {
				tasks[i].Sev = f.Severity
			}
			continue
		}
		index[key] = len(tasks)
		tasks = append(tasks, mdTask{File: file, Rule: f.Rule, Count: 1, Sev: f.Severity})
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		switch {
		case a.Sev.Rank() != b.Sev.Rank():
			return a.Sev.Rank() > b.Sev.Rank()
		case a.Count != b.Count:
			return a.Count > b.Count
		case a.File != b.File:
			return a.File < b.File
		}
		return a.Rule < b.Rule
	})
	fmt.Fprintln(w, "## Fix checklist")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "One box per document and rule, most severe first.")
	fmt.Fprintln(w)
	for _, t := range tasks {
		fmt.Fprintf(w, "- [ ] `%s` — `%s` × %d (%s)\n", mdCode(t.File), mdCode(t.Rule), t.Count, t.Sev)
	}
	fmt.Fprintln(w)
}

// mdText makes s safe to drop into Markdown prose. The angle brackets are
// what matter: "<" starts a raw HTML tag, and a message containing an HTML
// comment would vanish from the rendered page entirely. Backticks are left
// alone: resolvers put them around the referenced text on purpose, so they
// already render as code spans.
func mdText(s string) string {
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return strings.ReplaceAll(s, "\n", " ")
}

// mdCell is mdText for a table cell, where "|" would start a new column.
func mdCell(s string) string {
	return strings.ReplaceAll(mdText(s), "|", "\\|")
}

// mdCode makes s safe inside a single-backtick code span. A code span
// cannot contain a bare backtick and a longer fence would only move the
// problem, so backticks become apostrophes — the content is quoted source,
// not something anyone will copy back out verbatim.
func mdCode(s string) string {
	s = strings.ReplaceAll(s, "`", "'")
	return strings.ReplaceAll(s, "\n", " ")
}
