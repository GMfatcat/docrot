package report

import (
	"encoding/json"
	"io"

	"docrot/internal/model"
)

// jsonReport is the stable machine-readable schema. Baselined findings are
// always included, carrying their "baselined" flag, so consumers can filter
// however they like.
type jsonReport struct {
	Version  int             `json:"version"`
	Docrot   string          `json:"docrot"`
	Summary  jsonSummary     `json:"summary"`
	Findings []model.Finding `json:"findings"`
	Coverage *jsonCoverage   `json:"coverage"`
}

// jsonSummary mirrors Summary with snake_case keys and a millisecond
// duration, which is easier to consume than Go's duration string.
type jsonSummary struct {
	Root       string            `json:"root"`
	Docs       int               `json:"docs"`
	References int               `json:"references"`
	Errors     int               `json:"errors"`
	Warnings   int               `json:"warnings"`
	Infos      int               `json:"infos"`
	Baselined  int               `json:"baselined"`
	Fixed      int               `json:"fixed"`
	DurationMS int64             `json:"duration_ms"`
	Git        string            `json:"git"`
	Extra      map[string]string `json:"extra,omitempty"`
}

type jsonCoverage struct {
	Packages []jsonPackageCoverage `json:"packages"`
	Flags    jsonCoverageGroup     `json:"flags"`
	Envs     jsonCoverageGroup     `json:"envs"`
}

type jsonPackageCoverage struct {
	Package    string   `json:"package"`
	Total      int      `json:"total"`
	Documented int      `json:"documented"`
	Missing    []string `json:"missing"`
}

type jsonCoverageGroup struct {
	Total      int      `json:"total"`
	Documented int      `json:"documented"`
	Missing    []string `json:"missing"`
}

// WriteJSON writes the report as indented JSON with a trailing newline.
// o.ShowBaselined is ignored: every finding is emitted.
func WriteJSON(w io.Writer, r *Report, o Options) error {
	out := jsonReport{
		Version:  SchemaVersion,
		Docrot:   r.Version,
		Summary:  jsonSummaryOf(r.Summary),
		Findings: sorted(r),
		Coverage: jsonCoverageOf(r.Coverage),
	}
	if out.Findings == nil {
		out.Findings = []model.Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func jsonSummaryOf(s Summary) jsonSummary {
	return jsonSummary{
		Root:       s.Root,
		Docs:       s.Docs,
		References: s.References,
		Errors:     s.Errors,
		Warnings:   s.Warnings,
		Infos:      s.Infos,
		Baselined:  s.Baselined,
		Fixed:      s.Fixed,
		DurationMS: s.Duration.Milliseconds(),
		Git:        s.Git,
		Extra:      s.Extra,
	}
}

func jsonCoverageOf(c *Coverage) *jsonCoverage {
	if c == nil {
		return nil
	}
	out := &jsonCoverage{
		Packages: make([]jsonPackageCoverage, 0, len(c.Packages)),
		Flags:    jsonCoverageGroup{Total: c.Flags.Total, Documented: c.Flags.Documented, Missing: nonNil(c.Flags.Missing)},
		Envs:     jsonCoverageGroup{Total: c.Envs.Total, Documented: c.Envs.Documented, Missing: nonNil(c.Envs.Missing)},
	}
	for _, p := range c.Packages {
		out.Packages = append(out.Packages, jsonPackageCoverage{
			Package:    p.Package,
			Total:      p.Total,
			Documented: p.Documented,
			Missing:    nonNil(p.Missing),
		})
	}
	return out
}

// nonNil turns a nil slice into an empty one so JSON shows [] rather than null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
