package report

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"docrot/internal/model"
)

// junitSuites is the <testsuites> root. GitLab, Jenkins and Gitea all read
// this shape.
type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Time     string       `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Time     string      `xml:"time,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	ClassName string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

// WriteJUnit writes the run as JUnit XML. A JUnit test case fails at most
// once, so each case is one document and one rule: <testcase
// classname="README.md" name="missing-path"> with a <failure> whose message
// is the first finding and whose body lists them all. Info-level findings
// are <skipped> cases, and baselined findings are skipped too unless
// o.ShowBaselined. One extra passing case, "docrot summary", carries the
// summary line so that a clean run still reports a test.
func WriteJUnit(w io.Writer, r *Report, o Options) error {
	type group struct {
		file, rule string
		findings   []model.Finding
	}
	var groups []*group
	byKey := map[string]*group{}
	for _, f := range sorted(r) {
		file := relPath(o.Root, f.Loc.File)
		key := file + "\x00" + f.Rule
		g := byKey[key]
		if g == nil {
			g = &group{file: file, rule: f.Rule}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.findings = append(g.findings, f)
	}

	suite := junitSuite{Name: "docrot", Time: fmt.Sprintf("%.3f", r.Summary.Duration.Seconds())}
	for _, g := range groups {
		c := junitCase{ClassName: g.file, Name: g.rule, Time: "0"}
		var lines []string
		worst := model.SevInfo
		allBaselined := true
		for _, f := range g.findings {
			lines = append(lines, junitFindingLine(f, o))
			if !f.Baselined {
				allBaselined = false
			}
			if (!f.Baselined || o.ShowBaselined) && f.Severity.Rank() > worst.Rank() {
				worst = f.Severity
			}
		}
		body := strings.Join(lines, "\n")
		switch {
		case allBaselined && !o.ShowBaselined:
			c.Skipped = &junitSkipped{Message: fmt.Sprintf("%s: baselined", plural(len(g.findings), "finding"))}
			c.SystemOut = body
		case worst == model.SevInfo:
			c.Skipped = &junitSkipped{Message: fmt.Sprintf("%s at info level", plural(len(g.findings), "finding"))}
			c.SystemOut = body
		default:
			c.Failure = &junitFailure{Message: lines[0], Type: string(worst), Body: body}
		}
		suite.Cases = append(suite.Cases, c)
	}
	suite.Cases = append(suite.Cases, junitCase{
		ClassName: "docrot", Name: "summary", Time: suite.Time,
		SystemOut: SummaryLine(r.Summary),
	})
	for _, c := range suite.Cases {
		suite.Tests++
		if c.Failure != nil {
			suite.Failures++
		}
		if c.Skipped != nil {
			suite.Skipped++
		}
	}

	root := junitSuites{
		Tests: suite.Tests, Failures: suite.Failures, Skipped: suite.Skipped, Time: suite.Time,
		Suites: []junitSuite{suite},
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(root); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// junitFindingLine renders a finding the way the text report does, minus
// colour, for the failure body.
func junitFindingLine(f model.Finding, o Options) string {
	var b strings.Builder
	if f.Baselined {
		b.WriteString("[baselined] ")
	}
	b.WriteString(locString(o.Root, f.Loc))
	b.WriteString(": ")
	b.WriteString(string(f.Severity))
	b.WriteByte(' ')
	b.WriteString(f.Rule)
	if f.Message != "" {
		b.WriteByte(' ')
		b.WriteString(f.Message)
	}
	if s := suggestionText(f.Suggestion); s != "" {
		b.WriteByte(' ')
		b.WriteString(s)
	}
	return b.String()
}
