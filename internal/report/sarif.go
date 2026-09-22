package report

import (
	"encoding/json"
	"io"
	"sort"
	"strings"

	"docrot/internal/model"
)

// SARIF constants.
const (
	sarifSchema  = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion = "2.1.0"
	sarifToolURI = "https://github.com/GMfatcat/docrot"
	toolName     = "docrot"

	// fingerprintKey is the partialFingerprints key GitHub uses to track a
	// docrot result across commits.
	fingerprintKey = "docrot/v1"
)

// sarifDefaultLevels overrides the default level of the rules that are not
// errors by nature. Everything else defaults to "warning".
var sarifDefaultLevels = map[string]string{
	model.RulePairNumber:   "note",
	model.RuleUndocumented: "note",
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string           `json:"id"`
	Name                 string           `json:"name"`
	ShortDescription     sarifText        `json:"shortDescription"`
	FullDescription      sarifText        `json:"fullDescription"`
	DefaultConfiguration sarifRuleDefault `json:"defaultConfiguration"`
}

type sarifRuleDefault struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           int               `json:"ruleIndex"`
	Level               string            `json:"level"`
	Message             sarifText         `json:"message"`
	Locations           []sarifLocation   `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints,omitempty"`
	BaselineState       string            `json:"baselineState,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

// WriteSARIF writes a minimal but valid SARIF 2.1.0 log that GitHub code
// scanning accepts. Baselined findings are always included and marked
// baselineState "unchanged"; the rest are "new".
func WriteSARIF(w io.Writer, r *Report, o Options) error {
	findings := sorted(r)
	rules, index := sarifRules(findings)

	results := make([]sarifResult, 0, len(findings))
	for _, f := range findings {
		results = append(results, sarifResultOf(f, index[f.Rule], o))
	}

	log := sarifLog{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           toolName,
				Version:        r.Version,
				InformationURI: sarifToolURI,
				Rules:          rules,
			}},
			Results: results,
		}},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

// sarifRules builds the driver rule table from model.AllRules, appending any
// rule a finding used that the model does not know about, so every result's
// ruleIndex is valid. It returns the table and a rule id -> index map.
func sarifRules(findings []model.Finding) ([]sarifRule, map[string]int) {
	index := make(map[string]int, len(model.AllRules))
	rules := make([]sarifRule, 0, len(model.AllRules))
	add := func(id string) {
		if _, ok := index[id]; ok || id == "" {
			return
		}
		index[id] = len(rules)
		rules = append(rules, sarifRule{
			ID:                   id,
			Name:                 sarifRuleName(id),
			ShortDescription:     sarifText{Text: ruleDescription(id)},
			FullDescription:      sarifText{Text: ruleDescription(id)},
			DefaultConfiguration: sarifRuleDefault{Level: sarifDefaultLevel(id)},
		})
	}
	for _, id := range model.AllRules {
		add(id)
	}
	extra := make([]string, 0)
	for _, f := range findings {
		if _, ok := index[f.Rule]; !ok && f.Rule != "" {
			extra = append(extra, f.Rule)
		}
	}
	sort.Strings(extra)
	for _, id := range extra {
		add(id)
	}
	return rules, index
}

func sarifResultOf(f model.Finding, ruleIndex int, o Options) sarifResult {
	msg := f.Message
	if s := suggestionText(f.Suggestion); s != "" {
		if msg != "" {
			msg += " "
		}
		msg += s
	}
	res := sarifResult{
		RuleID:    f.Rule,
		RuleIndex: ruleIndex,
		Level:     sarifLevel(f.Severity),
		Message:   sarifText{Text: msg},
		Locations: []sarifLocation{{PhysicalLocation: sarifPhysicalLocation{
			ArtifactLocation: sarifArtifactLocation{URI: relPath(o.Root, f.Loc.File)},
			Region:           sarifRegionOf(f.Loc),
		}}},
		BaselineState: "new",
	}
	if f.Baselined {
		res.BaselineState = "unchanged"
	}
	if f.Fingerprint != "" {
		res.PartialFingerprints = map[string]string{fingerprintKey: f.Fingerprint}
	}
	return res
}

// sarifRegionOf returns the region for a location, or nil for a whole-file
// finding (SARIF regions must have a startLine).
func sarifRegionOf(l model.Location) *sarifRegion {
	if l.Line <= 0 {
		return nil
	}
	reg := &sarifRegion{StartLine: l.Line}
	if l.Col > 0 {
		reg.StartColumn = l.Col
	}
	return reg
}

// sarifLevel maps a docrot severity to a SARIF level.
func sarifLevel(s model.Severity) string {
	switch s {
	case model.SevError:
		return "error"
	case model.SevWarning:
		return "warning"
	case model.SevInfo:
		return "note"
	}
	return "warning"
}

// sarifDefaultLevel is the level a rule reports at unless configured otherwise.
func sarifDefaultLevel(rule string) string {
	if l, ok := sarifDefaultLevels[rule]; ok {
		return l
	}
	return "warning"
}

// ruleDescription returns the documented description of a rule, falling back
// to the rule id for rules the model does not describe.
func ruleDescription(rule string) string {
	if d, ok := model.RuleDescriptions[rule]; ok {
		return d
	}
	return rule
}

// sarifRuleName turns a rule id into the PascalCase opaque name SARIF wants:
// "missing-path" -> "MissingPath".
func sarifRuleName(rule string) string {
	parts := strings.FieldsFunc(rule, func(r rune) bool { return r == '-' || r == '_' })
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	if b.Len() == 0 {
		return rule
	}
	return b.String()
}
