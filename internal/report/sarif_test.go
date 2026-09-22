package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"docrot/internal/model"
)

// decodeSARIF runs WriteSARIF and decodes the log back into typed structs.
func decodeSARIF(t *testing.T, r *Report, o Options) sarifLog {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteSARIF(&buf, r, o); err != nil {
		t.Fatalf("WriteSARIF: %v", err)
	}
	var log sarifLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("WriteSARIF produced invalid JSON: %v\n%s", err, buf.String())
	}
	return log
}

func TestSARIFEnvelope(t *testing.T) {
	log := decodeSARIF(t, sampleReport(), Options{})
	if log.Schema != sarifSchema {
		t.Errorf("$schema = %q, want %q", log.Schema, sarifSchema)
	}
	if log.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", log.Version)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(log.Runs))
	}
	d := log.Runs[0].Tool.Driver
	if d.Name != "docrot" {
		t.Errorf("tool.driver.name = %q, want docrot", d.Name)
	}
	if d.Version != "0.1.0" {
		t.Errorf("tool.driver.version = %q, want 0.1.0", d.Version)
	}
	if d.InformationURI != "https://github.com/GMfatcat/docrot" {
		t.Errorf("tool.driver.informationUri = %q", d.InformationURI)
	}
}

func TestSARIFRules(t *testing.T) {
	log := decodeSARIF(t, sampleReport(), Options{})
	rules := log.Runs[0].Tool.Driver.Rules
	if len(rules) != len(model.AllRules) {
		t.Fatalf("len(rules) = %d, want %d", len(rules), len(model.AllRules))
	}
	for i, id := range model.AllRules {
		r := rules[i]
		if r.ID != id {
			t.Errorf("rules[%d].id = %q, want %q", i, r.ID, id)
		}
		if r.Name == "" || r.Name == id {
			t.Errorf("rules[%d].name = %q, want a PascalCase name", i, r.Name)
		}
		if r.ShortDescription.Text != model.RuleDescriptions[id] {
			t.Errorf("rules[%d].shortDescription = %q", i, r.ShortDescription.Text)
		}
		if r.FullDescription.Text == "" {
			t.Errorf("rules[%d].fullDescription is empty", i)
		}
		switch r.DefaultConfiguration.Level {
		case "error", "warning", "note", "none":
		default:
			t.Errorf("rules[%d].defaultConfiguration.level = %q", i, r.DefaultConfiguration.Level)
		}
	}
	if got := rules[0].Name; got != "MissingPath" {
		t.Errorf("rule name for missing-path = %q, want MissingPath", got)
	}
}

func TestSARIFUnknownRuleIsAdded(t *testing.T) {
	r := sampleReport()
	r.Findings = append(r.Findings, model.Finding{
		Rule: "zz-custom", Severity: model.SevError, Loc: model.Location{File: "z.md", Line: 1},
	})
	log := decodeSARIF(t, r, Options{})
	rules := log.Runs[0].Tool.Driver.Rules
	if len(rules) != len(model.AllRules)+1 {
		t.Fatalf("len(rules) = %d, want %d", len(rules), len(model.AllRules)+1)
	}
	if rules[len(rules)-1].ID != "zz-custom" {
		t.Errorf("last rule = %q, want zz-custom", rules[len(rules)-1].ID)
	}
	for _, res := range log.Runs[0].Results {
		if res.RuleIndex < 0 || res.RuleIndex >= len(rules) {
			t.Errorf("result ruleIndex %d out of range", res.RuleIndex)
		}
		if rules[res.RuleIndex].ID != res.RuleID {
			t.Errorf("ruleIndex %d points at %q, want %q", res.RuleIndex, rules[res.RuleIndex].ID, res.RuleID)
		}
	}
}

func TestSARIFResults(t *testing.T) {
	log := decodeSARIF(t, sampleReport(), Options{})
	results := log.Runs[0].Results
	if len(results) != 4 {
		t.Fatalf("len(results) = %d, want 4 (baselined results are included)", len(results))
	}

	tests := []struct {
		name          string
		index         int
		ruleID        string
		level         string
		uri           string
		line, col     int
		wantRegion    bool
		message       string
		fingerprint   string
		baselineState string
	}{
		{
			name: "whole-file pair finding", index: 0,
			ruleID: model.RulePairLag, level: "note", uri: "README-zh.md",
			wantRegion:  false,
			message:     "4 commits to README.md since README-zh.md last changed",
			fingerprint: "cccc3333", baselineState: "new",
		},
		{
			name: "baselined warning", index: 1,
			ruleID: model.RuleUnknownFlag, level: "warning", uri: "README.md",
			line: 8, col: 1, wantRegion: true,
			message:     "`--verbose` is not defined",
			fingerprint: "dddd4444", baselineState: "unchanged",
		},
		{
			name: "error with suggestion", index: 2,
			ruleID: model.RuleMissingSymbol, level: "error", uri: "README.md",
			line: 42, col: 15, wantRegion: true,
			message:     "`httpx.WriteJSON` not found in package httpx (did you mean httpx.WriteData?)",
			fingerprint: "aaaa1111", baselineState: "new",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := results[tt.index]
			if res.RuleID != tt.ruleID {
				t.Errorf("ruleId = %q, want %q", res.RuleID, tt.ruleID)
			}
			if res.Level != tt.level {
				t.Errorf("level = %q, want %q", res.Level, tt.level)
			}
			if res.Message.Text != tt.message {
				t.Errorf("message.text = %q, want %q", res.Message.Text, tt.message)
			}
			if len(res.Locations) != 1 {
				t.Fatalf("len(locations) = %d, want 1", len(res.Locations))
			}
			phys := res.Locations[0].PhysicalLocation
			if phys.ArtifactLocation.URI != tt.uri {
				t.Errorf("uri = %q, want %q", phys.ArtifactLocation.URI, tt.uri)
			}
			switch {
			case !tt.wantRegion:
				if phys.Region != nil {
					t.Errorf("region = %+v, want none for a whole-file finding", phys.Region)
				}
			case phys.Region == nil:
				t.Fatal("region is missing")
			default:
				if phys.Region.StartLine != tt.line || phys.Region.StartColumn != tt.col {
					t.Errorf("region = %+v, want line %d col %d", phys.Region, tt.line, tt.col)
				}
			}
			if got := res.PartialFingerprints[fingerprintKey]; got != tt.fingerprint {
				t.Errorf("partialFingerprints[%q] = %q, want %q", fingerprintKey, got, tt.fingerprint)
			}
			if res.BaselineState != tt.baselineState {
				t.Errorf("baselineState = %q, want %q", res.BaselineState, tt.baselineState)
			}
		})
	}
}

func TestSARIFNoRegionWithoutColumn(t *testing.T) {
	r := &Report{Findings: []model.Finding{
		{Rule: model.RuleMissingPath, Severity: model.SevWarning, Loc: model.Location{File: "a.md", Line: 5}},
	}}
	log := decodeSARIF(t, r, Options{})
	reg := log.Runs[0].Results[0].Locations[0].PhysicalLocation.Region
	if reg == nil || reg.StartLine != 5 || reg.StartColumn != 0 {
		t.Errorf("region = %+v, want startLine 5 and no startColumn", reg)
	}
}

func TestSARIFLevelMapping(t *testing.T) {
	tests := []struct {
		sev  model.Severity
		want string
	}{
		{model.SevError, "error"},
		{model.SevWarning, "warning"},
		{model.SevInfo, "note"},
		{model.Severity(""), "warning"},
	}
	for _, tt := range tests {
		if got := sarifLevel(tt.sev); got != tt.want {
			t.Errorf("sarifLevel(%q) = %q, want %q", tt.sev, got, tt.want)
		}
	}
}

func TestSARIFRuleName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"missing-path", "MissingPath"},
		{"unknown-config-key", "UnknownConfigKey"},
		{"undocumented", "Undocumented"},
		{"pair_lag", "PairLag"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := sarifRuleName(tt.in); got != tt.want {
			t.Errorf("sarifRuleName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSARIFRelativeURI(t *testing.T) {
	r := &Report{Findings: []model.Finding{
		{Rule: model.RuleMissingPath, Severity: model.SevWarning, Loc: model.Location{File: "docs\\sub\\a.md", Line: 1}},
	}}
	log := decodeSARIF(t, r, Options{Root: "/repo"})
	if got := log.Runs[0].Results[0].Locations[0].PhysicalLocation.ArtifactLocation.URI; got != "docs/sub/a.md" {
		t.Errorf("uri = %q, want docs/sub/a.md", got)
	}
}
