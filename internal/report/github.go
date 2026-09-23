package report

import (
	"bufio"
	"io"
	"strconv"
	"strings"

	"docrot/internal/model"
)

// WriteGitHub writes one GitHub Actions workflow command per finding
// ("::error file=README.md,line=12,col=3,title=missing-path::message"), so
// that a job which runs `docrot check --format github` gets its findings
// annotated on the pull request without a SARIF upload. Gitea Actions reads
// the same syntax. Severities map to error, warning and notice; the run's
// summary line ends the output as a notice titled "docrot".
//
// Like the text report it hides baselined findings unless o.ShowBaselined
// and info-level findings unless o.ShowInfo: annotations are noisy, and a
// finding the baseline already accepted should not decorate a diff.
func WriteGitHub(w io.Writer, r *Report, o Options) error {
	bw := bufio.NewWriter(w)
	for _, f := range sorted(r) {
		if f.Baselined && !o.ShowBaselined {
			continue
		}
		if f.Severity == model.SevInfo && !o.ShowInfo {
			continue
		}
		bw.WriteString(githubLine(f, o))
		bw.WriteByte('\n')
	}
	bw.WriteString("::notice title=docrot::" + escapeGitHubData(SummaryLine(r.Summary)) + "\n")
	return bw.Flush()
}

// githubLine renders one finding as a workflow command.
func githubLine(f model.Finding, o Options) string {
	var b strings.Builder
	b.WriteString("::")
	b.WriteString(githubLevel(f.Severity))
	b.WriteString(" file=")
	b.WriteString(escapeGitHubProperty(relPath(o.Root, f.Loc.File)))
	if f.Loc.Line > 0 {
		b.WriteString(",line=")
		b.WriteString(strconv.Itoa(f.Loc.Line))
		if f.Loc.Col > 0 {
			b.WriteString(",col=")
			b.WriteString(strconv.Itoa(f.Loc.Col))
		}
	}
	if f.Rule != "" {
		b.WriteString(",title=")
		b.WriteString(escapeGitHubProperty(f.Rule))
	}
	b.WriteString("::")
	msg := f.Message
	if s := suggestionText(f.Suggestion); s != "" {
		if msg != "" {
			msg += " "
		}
		msg += s
	}
	if f.Baselined {
		msg = "[baselined] " + msg
	}
	b.WriteString(escapeGitHubData(msg))
	return b.String()
}

// githubLevel maps a severity to a workflow command name.
func githubLevel(s model.Severity) string {
	switch s {
	case model.SevError:
		return "error"
	case model.SevWarning:
		return "warning"
	}
	return "notice"
}

// escapeGitHubData escapes the message part of a workflow command.
func escapeGitHubData(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// escapeGitHubProperty escapes a property value, which additionally may
// not contain the separators.
func escapeGitHubProperty(s string) string {
	s = escapeGitHubData(s)
	s = strings.ReplaceAll(s, ":", "%3A")
	s = strings.ReplaceAll(s, ",", "%2C")
	return s
}
