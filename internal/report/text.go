package report

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"docrot/internal/model"
)

// ANSI escape sequences used by the text report.
const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

// maxMissingListed caps how many undocumented names the coverage table
// prints per group before summarising the rest.
const maxMissingListed = 10

// WriteText writes the compiler-style report: one finding per line as
//
//	FILE:LINE:COL: SEVERITY RULE MESSAGE (did you mean X?)
//
// with the ":COL" and ":LINE" parts dropped when they are zero, followed by a
// blank line and the summary. Baselined findings are hidden unless
// o.ShowBaselined, in which case they are prefixed with "[baselined] ".
// Coverage, when present, is printed after the findings.
func WriteText(w io.Writer, r *Report, o Options) error {
	bw := bufio.NewWriter(w)
	c := palette(o.Color)

	shown := 0
	for _, f := range sorted(r) {
		if f.Baselined && !o.ShowBaselined {
			continue
		}
		fmt.Fprintln(bw, textLine(f, o, c))
		shown++
	}
	if shown > 0 {
		fmt.Fprintln(bw)
	}
	fmt.Fprintln(bw, SummaryLine(r.Summary))
	if extra := extraLine(r.Summary.Extra); extra != "" {
		fmt.Fprintln(bw, extra)
	}
	if r.Coverage != nil {
		writeCoverageText(bw, r.Coverage)
	}
	return bw.Flush()
}

// textLine renders one finding as a single report line.
func textLine(f model.Finding, o Options, c colors) string {
	var b strings.Builder
	if f.Baselined {
		b.WriteString(c.dim("[baselined] "))
	}
	b.WriteString(locString(o.Root, f.Loc))
	b.WriteString(": ")
	b.WriteString(c.severity(f.Severity))
	b.WriteByte(' ')
	b.WriteString(c.rule(f.Rule))
	if f.Message != "" {
		b.WriteByte(' ')
		b.WriteString(f.Message)
	}
	if s := suggestionText(f.Suggestion); s != "" {
		b.WriteByte(' ')
		b.WriteString(c.suggestion(s))
	}
	return b.String()
}

// SummaryLine renders the one-line run summary, for example:
//
//	3 errors, 2 warnings, 5 info (12 baselined, 1 fixed) — 14 docs, 1,204 references, 0.83s
//
// A "(git: …)" note is appended when git was not used.
func SummaryLine(s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s, %s, %d info",
		plural(s.Errors, "error"), plural(s.Warnings, "warning"), s.Infos)

	var notes []string
	if s.Baselined > 0 {
		notes = append(notes, fmt.Sprintf("%d baselined", s.Baselined))
	}
	if s.Fixed > 0 {
		notes = append(notes, fmt.Sprintf("%d fixed", s.Fixed))
	}
	if len(notes) > 0 {
		b.WriteString(" (" + strings.Join(notes, ", ") + ")")
	}

	fmt.Fprintf(&b, " — %s, %s, %s",
		plural(s.Docs, "doc"), plural2(s.References, "reference"), humanDuration(s.Duration))

	if s.Git != "" && s.Git != GitOK {
		fmt.Fprintf(&b, " (git: %s)", s.Git)
	}
	return b.String()
}

// extraLine renders Summary.Extra as "key: value" pairs in key order, or ""
// when there is nothing extra.
func extraLine(extra map[string]string) string {
	if len(extra) == 0 {
		return ""
	}
	parts := make([]string, 0, len(extra))
	for _, k := range sortedKeys(extra) {
		parts = append(parts, k+": "+extra[k])
	}
	return strings.Join(parts, ", ")
}

// writeCoverageText prints the per-package, flag and env coverage table.
func writeCoverageText(w io.Writer, cov *Coverage) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Coverage:")
	for _, p := range cov.Packages {
		fmt.Fprintf(w, "  %s: %s\n", p.Package, coverageCell(p.Documented, p.Total, p.Missing))
	}
	fmt.Fprintf(w, "  flags: %s\n", coverageCell(cov.Flags.Documented, cov.Flags.Total, cov.Flags.Missing))
	fmt.Fprintf(w, "  envs: %s\n", coverageCell(cov.Envs.Documented, cov.Envs.Total, cov.Envs.Missing))
}

// coverageCell renders "documented/total (pct%)" plus up to
// maxMissingListed undocumented names.
func coverageCell(documented, total int, missing []string) string {
	s := fmt.Sprintf("%d/%d (%d%%)", documented, total, pct(documented, total))
	if len(missing) == 0 {
		return s
	}
	list := missing
	rest := 0
	if len(list) > maxMissingListed {
		rest = len(list) - maxMissingListed
		list = list[:maxMissingListed]
	}
	s += " missing: " + strings.Join(list, ", ")
	if rest > 0 {
		s += fmt.Sprintf(", … and %d more", rest)
	}
	return s
}

// plural renders "1 error" / "2 errors".
func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// plural2 is plural with a thousands-separated count.
func plural2(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%s %s", humanInt(n), word)
	}
	return fmt.Sprintf("%s %ss", humanInt(n), word)
}

// colors paints report fragments, or leaves them alone when colour is off.
type colors struct{ on bool }

func palette(on bool) colors { return colors{on: on} }

func (c colors) wrap(code, s string) string {
	if !c.on || s == "" {
		return s
	}
	return code + s + ansiReset
}

func (c colors) dim(s string) string        { return c.wrap(ansiDim, s) }
func (c colors) rule(s string) string       { return c.wrap(ansiDim, s) }
func (c colors) suggestion(s string) string { return c.wrap(ansiGreen, s) }

func (c colors) severity(s model.Severity) string {
	switch s {
	case model.SevError:
		return c.wrap(ansiRed, string(s))
	case model.SevWarning:
		return c.wrap(ansiYellow, string(s))
	case model.SevInfo:
		return c.wrap(ansiCyan, string(s))
	}
	return string(s)
}
