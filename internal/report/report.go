// Package report turns a finished docrot run into something a human, an
// agent or a machine can read: compiler-style text for a terminal, Markdown
// written for a language model, a stable JSON schema for scripts, SARIF
// 2.1.0 for GitHub code scanning, a single-file HTML page with filtering,
// GitHub Actions workflow commands (inline pull-request annotations) and
// JUnit XML for the test panels of GitLab, Jenkins and Gitea.
//
// All the writers share the Report value built by the engine. Each writer
// sorts a copy of the findings with Sort, so output is deterministic no
// matter what order the resolvers produced them in.
package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"docrot/internal/model"
)

// SchemaVersion is the version field of the JSON report. Bump it only for
// incompatible changes to that schema.
const SchemaVersion = 1

// Git status values for Summary.Git.
const (
	GitOK          = "ok"          // git was used
	GitUnavailable = "unavailable" // git is missing, or this is not a repository
	GitDisabled    = "disabled"    // the user passed --no-git
)

// Summary is the headline of a run. The severity counts cover non-baselined
// findings only; baselined ones are counted separately.
type Summary struct {
	Root       string
	Docs       int
	References int
	Errors     int
	Warnings   int
	Infos      int
	Baselined  int
	Fixed      int // baseline entries no longer present
	Duration   time.Duration
	Git        string            // GitOK | GitUnavailable | GitDisabled
	Extra      map[string]string // free-form, e.g. "go packages": "14"
}

// CoverageGroup is one flat documented/total group, used for flags and
// environment variables.
type CoverageGroup struct {
	Total      int
	Documented int
	Missing    []string
}

// PackageCoverage is the documented/total figure for one code package.
type PackageCoverage struct {
	Package    string
	Total      int
	Documented int
	Missing    []string
}

// Coverage is the optional "what does no document mention?" section.
type Coverage struct {
	Packages []PackageCoverage
	Flags    CoverageGroup
	Envs     CoverageGroup
}

// Report is everything the writers need.
type Report struct {
	Summary  Summary
	Findings []model.Finding // all of them, including baselined ones
	Coverage *Coverage       // nil when coverage was not requested
	Version  string          // docrot version string
}

// Options tune a writer.
type Options struct {
	ShowBaselined bool   // text/markdown/HTML: also show findings frozen by the baseline
	ShowInfo      bool   // text: also list info-level findings (always counted)
	Color         bool   // text: emit ANSI colour
	Root          string // repository root, used to relativise paths
}

// Formats lists the accepted format names for Write, in help order.
var Formats = []string{"text", "md", "json", "sarif", "html", "github", "junit"}

// Write dispatches on format, which must be one of Formats.
func Write(format string, w io.Writer, r *Report, o Options) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "text", "":
		return WriteText(w, r, o)
	case "md", "markdown":
		return WriteMarkdown(w, r, o)
	case "json":
		return WriteJSON(w, r, o)
	case "sarif":
		return WriteSARIF(w, r, o)
	case "html":
		return WriteHTML(w, r, o)
	case "github":
		return WriteGitHub(w, r, o)
	case "junit":
		return WriteJUnit(w, r, o)
	}
	return fmt.Errorf("unknown report format %q (want one of %s)", format, strings.Join(Formats, ", "))
}

// Sort orders findings the way every report shows them: by file, then line,
// then column, then severity (most severe first), then rule, then message.
// The sort is stable, so findings that compare equal keep their input order.
func Sort(findings []model.Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		switch {
		case a.Loc.File != b.Loc.File:
			return a.Loc.File < b.Loc.File
		case a.Loc.Line != b.Loc.Line:
			return a.Loc.Line < b.Loc.Line
		case a.Loc.Col != b.Loc.Col:
			return a.Loc.Col < b.Loc.Col
		case a.Severity.Rank() != b.Severity.Rank():
			return a.Severity.Rank() > b.Severity.Rank()
		case a.Rule != b.Rule:
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})
}

// sorted returns a sorted copy of r's findings, leaving the report untouched.
func sorted(r *Report) []model.Finding {
	out := slices.Clone(r.Findings)
	Sort(out)
	return out
}

// ColorEnabled reports whether ANSI colour is appropriate for w: w must be a
// terminal, NO_COLOR must be unset, and TERM must not be "dumb".
func ColorEnabled(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// relPath normalises a finding location for display: relative to root when
// possible, always with forward slashes.
func relPath(root, file string) string {
	p := file
	if root != "" && filepath.IsAbs(p) {
		if rel, err := filepath.Rel(root, p); err == nil {
			p = rel
		}
	}
	return filepath.ToSlash(p)
}

// locString renders a location as "file", "file:line" or "file:line:col",
// dropping the zero parts, exactly as the text report needs it.
func locString(root string, l model.Location) string {
	p := relPath(root, l.File)
	switch {
	case l.Line == 0:
		return p
	case l.Col == 0:
		return p + ":" + strconv.Itoa(l.Line)
	}
	return p + ":" + strconv.Itoa(l.Line) + ":" + strconv.Itoa(l.Col)
}

// suggestionText renders the parenthesised hint, or "" when there is none.
func suggestionText(s string) string {
	if s == "" {
		return ""
	}
	return "(did you mean " + s + "?)"
}

// humanInt formats n with commas every three digits: 1204 -> "1,204".
func humanInt(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// humanDuration formats a duration as seconds with two decimals.
func humanDuration(d time.Duration) string {
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// pct returns documented/total as a whole percentage. An empty group counts
// as fully documented.
func pct(documented, total int) int {
	if total <= 0 {
		return 100
	}
	return documented * 100 / total
}

// sortedKeys returns the keys of m in ascending order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
