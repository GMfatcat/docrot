package pairs

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"docrot/internal/gitx"
	"docrot/internal/markdown"
	"docrot/internal/model"
)

// maxListed caps how many items a message enumerates.
const (
	maxListedItems   = 5
	maxListedNumbers = 10
)

// defaultSeverity is the severity of each pair rule when Options.Severity
// does not override it.
var defaultSeverity = map[string]model.Severity{
	model.RulePairHeading: model.SevWarning,
	model.RulePairCode:    model.SevWarning,
	model.RulePairLink:    model.SevWarning,
	model.RulePairTable:   model.SevWarning,
	model.RulePairNumber:  model.SevInfo,
	model.RulePairLag:     model.SevWarning,
	model.RulePairMissing: model.SevInfo,
	model.RulePairOrphan:  model.SevWarning,
}

// Options tunes [Compare].
type Options struct {
	// Severity overrides the default severity of a pair rule, by rule name.
	// Missing or empty entries fall back to the defaults: warning for
	// pair-heading, pair-code, pair-link, pair-table, pair-lag and
	// pair-orphan, info for pair-number and pair-missing.
	Severity map[string]model.Severity
	// Repo enables the git-backed pair-lag rule. When nil the rule is
	// skipped, and git errors never fail a comparison.
	Repo *gitx.Repo
}

// severity returns the configured severity for a rule.
func (o Options) severity(rule string) model.Severity {
	if s, ok := o.Severity[rule]; ok && s != "" {
		return s
	}
	return defaultSeverity[rule]
}

// Compare produces the findings of one pair. Every finding is reported on
// the translation, because that is the file a maintainer has to fix.
//
// The rules run in a fixed order (heading, code, link, table, number, lag)
// so the output is deterministic. Structural comparisons use
// [markdown.Doc.Fingerprint], which ignores heading text and link anchors.
// Findings that cannot be tied to a position — a differing count, a link
// that exists in only one file — are reported at line 1.
func Compare(p Pair, src, tr *markdown.Doc, opts Options) []model.Finding {
	if src == nil || tr == nil {
		return nil
	}
	sf, tf := src.Fingerprint(), tr.Fingerprint()

	var out []model.Finding
	out = append(out, headingFindings(p, tr, sf, tf, opts)...)
	out = append(out, codeFindings(p, tr, sf, tf, opts)...)
	out = append(out, linkFindings(p, sf, tf, opts)...)
	out = append(out, tableFindings(p, tr, sf, tf, opts)...)
	out = append(out, numberFindings(p, sf, tf, opts)...)
	out = append(out, lagFindings(p, opts)...)
	return out
}

// headingFindings reports a different number of headings, or the first
// heading whose level differs.
func headingFindings(p Pair, tr *markdown.Doc, sf, tf markdown.Fingerprint, opts Options) []model.Finding {
	if len(tf.Headings) != len(sf.Headings) {
		return []model.Finding{newFinding(model.RulePairHeading, p.Translation, "count",
			opts.severity(model.RulePairHeading), 1,
			fmt.Sprintf("translation has %s, source has %d",
				plural(len(tf.Headings), "heading"), len(sf.Headings)))}
	}
	for i := range tf.Headings {
		if tf.Headings[i].Level == sf.Headings[i].Level {
			continue
		}
		line := 1
		if i < len(tr.Headings) {
			line = tr.Headings[i].Line
		}
		return []model.Finding{newFinding(model.RulePairHeading, p.Translation,
			fmt.Sprintf("level#%d", i+1), opts.severity(model.RulePairHeading), line,
			fmt.Sprintf("heading #%d is level %d in translation but level %d in source",
				i+1, tf.Headings[i].Level, sf.Headings[i].Level))}
	}
	return nil
}

// codeFindings reports a different number of fenced code blocks, or every
// block whose content hash differs.
func codeFindings(p Pair, tr *markdown.Doc, sf, tf markdown.Fingerprint, opts Options) []model.Finding {
	sev := opts.severity(model.RulePairCode)
	if len(tf.Codes) != len(sf.Codes) {
		return []model.Finding{newFinding(model.RulePairCode, p.Translation, "count", sev, 1,
			fmt.Sprintf("translation has %s, source has %d",
				plural(len(tf.Codes), "code block"), len(sf.Codes)))}
	}
	var out []model.Finding
	for i := range tf.Codes {
		if tf.Codes[i].SHA256 == sf.Codes[i].SHA256 {
			continue
		}
		line := 1
		if i < len(tr.Fences) {
			line = tr.Fences[i].StartLine
		}
		lang := tf.Codes[i].Lang
		if lang == "" {
			lang = sf.Codes[i].Lang
		}
		msg := fmt.Sprintf("code block #%d differs from source", i+1)
		if lang != "" {
			msg = fmt.Sprintf("code block #%d (%s) differs from source", i+1, lang)
		}
		out = append(out, newFinding(model.RulePairCode, p.Translation,
			fmt.Sprintf("block#%d", i+1), sev, line, msg))
	}
	return out
}

// linkFindings reports link targets present in only one file of the pair.
// Links between the two files themselves are legitimate and are ignored.
func linkFindings(p Pair, sf, tf markdown.Fingerprint, opts Options) []model.Finding {
	sev := opts.severity(model.RulePairLink)
	mates := map[string]bool{
		path.Base(p.Source):      true,
		path.Base(p.Translation): true,
	}
	srcLinks := stripLangSegments(dropMates(sf.Links, mates))
	trLinks := stripLangSegments(dropMates(tf.Links, mates))

	var out []model.Finding
	for _, t := range missing(srcLinks, trLinks) {
		out = append(out, newFinding(model.RulePairLink, p.Translation, t, sev, 1,
			"link only in source: "+t))
	}
	for _, t := range missing(trLinks, srcLinks) {
		out = append(out, newFinding(model.RulePairLink, p.Translation, t, sev, 1,
			"link only in translation: "+t))
	}
	return out
}

// tableFindings reports a different number of tables, or every table whose
// shape differs.
func tableFindings(p Pair, tr *markdown.Doc, sf, tf markdown.Fingerprint, opts Options) []model.Finding {
	sev := opts.severity(model.RulePairTable)
	if len(tf.Tables) != len(sf.Tables) {
		return []model.Finding{newFinding(model.RulePairTable, p.Translation, "count", sev, 1,
			fmt.Sprintf("translation has %s, source has %d",
				plural(len(tf.Tables), "table"), len(sf.Tables)))}
	}
	var out []model.Finding
	for i := range tf.Tables {
		if tf.Tables[i] == sf.Tables[i] {
			continue
		}
		line := 1
		if i < len(tr.Tables) {
			line = tr.Tables[i].StartLine
		}
		out = append(out, newFinding(model.RulePairTable, p.Translation,
			fmt.Sprintf("table#%d", i+1), sev, line,
			fmt.Sprintf("table #%d is %d×%d in translation but %d×%d in source",
				i+1, tf.Tables[i].Rows, tf.Tables[i].Cols, sf.Tables[i].Rows, sf.Tables[i].Cols)))
	}
	return out
}

// numberFindings reports numbers and version strings present in only one
// file of the pair, at most one finding per side.
func numberFindings(p Pair, sf, tf markdown.Fingerprint, opts Options) []model.Finding {
	sev := opts.severity(model.RulePairNumber)
	var out []model.Finding
	if only := missing(sf.Numbers, tf.Numbers); len(only) > 0 {
		out = append(out, newFinding(model.RulePairNumber, p.Translation, "source", sev, 1,
			"numbers only in source: "+join(only, maxListedNumbers)))
	}
	if only := missing(tf.Numbers, sf.Numbers); len(only) > 0 {
		out = append(out, newFinding(model.RulePairNumber, p.Translation, "translation", sev, 1,
			"numbers only in translation: "+join(only, maxListedNumbers)))
	}
	return out
}

// lagFindings reports commits made to the source after the translation was
// last committed. Any git error silently disables the rule: a repository
// quirk must never fail the run.
func lagFindings(p Pair, opts Options) []model.Finding {
	if opts.Repo == nil {
		return nil
	}
	since, ok, err := opts.Repo.LastCommitTime(p.Translation)
	if err != nil || !ok {
		return nil
	}
	commits, err := opts.Repo.CommitsSince(p.Source, since)
	if err != nil || len(commits) == 0 {
		return nil
	}

	parts := make([]string, 0, maxListedItems)
	data := make([]map[string]any, 0, len(commits))
	for i, c := range commits {
		if i < maxListedItems {
			parts = append(parts, fmt.Sprintf("%s %q", c.Hash, c.Subject))
		}
		data = append(data, map[string]any{
			"hash":    c.Hash,
			"time":    c.Time.UTC().Format(time.RFC3339),
			"subject": c.Subject,
		})
	}
	listed := strings.Join(parts, ", ")
	if len(commits) > maxListedItems {
		listed += ", …"
	}

	f := newFinding(model.RulePairLag, p.Translation, "lag",
		opts.severity(model.RulePairLag), 1,
		fmt.Sprintf("%s to %s since %s last changed (%s)",
			plural(len(commits), "commit"), p.Source, p.Translation, listed))
	f.Data = map[string]any{
		"commits": data,
		"since":   since.UTC().Format(time.RFC3339),
	}
	return []model.Finding{f}
}

// newFinding builds a pair finding on the translation. detail is the stable
// part of the fingerprint: a link target, a block index or a bucket name.
func newFinding(rule, translation, detail string, sev model.Severity, line int, msg string) model.Finding {
	return model.Finding{
		Rule:        rule,
		Severity:    sev,
		Message:     msg,
		Loc:         model.Location{File: translation, Line: line},
		Fingerprint: model.Fingerprint(rule, translation, detail),
	}
}

var reLangSeg = regexp.MustCompile(`^(https?://[^/]+)/[a-z]{2}(?:-(?:[a-zA-Z]{2}|Hans|Hant))?(/|$)`)

// stripLangSegments removes a leading language path segment from absolute
// URLs ("https://site/ja/tutorial/" → "https://site/tutorial/") so that a
// translation linking to its own language variant is not reported as drift.
func stripLangSegments(targets []string) []string {
	out := make([]string, 0, len(targets))
	seen := map[string]bool{}
	for _, t := range targets {
		n := reLangSeg.ReplaceAllString(t, "$1$2")
		n = strings.TrimSuffix(n, "/")
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// dropMates removes the links that point at the other file of the pair,
// compared by base name so that "../README.md" and "README.md" both match.
func dropMates(targets []string, mates map[string]bool) []string {
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		if mates[linkBase(t)] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// linkBase returns the file name a link target points at, without any
// fragment or query string.
func linkBase(target string) string {
	s := target
	if i := strings.IndexAny(s, "#?"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, "/")
	if s == "" {
		return ""
	}
	return path.Base(strings.ReplaceAll(s, "\\", "/"))
}

// missing returns the elements of a that are not in b, keeping a's order.
func missing(a, b []string) []string {
	if len(a) == 0 {
		return nil
	}
	have := make(map[string]bool, len(b))
	for _, s := range b {
		have[s] = true
	}
	var out []string
	for _, s := range a {
		if !have[s] {
			out = append(out, s)
		}
	}
	return out
}

// join lists at most n items, appending an ellipsis when it truncates.
func join(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + ", …"
}

// plural formats a count with its noun: "1 table", "2 tables".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
