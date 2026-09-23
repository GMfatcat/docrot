package model

import (
	"encoding/json"
	"testing"
)

func TestConfidenceAndSeverityParsing(t *testing.T) {
	for in, want := range map[string]Confidence{"low": Low, "MED": Medium, " high ": High} {
		if got, ok := ParseConfidence(in); !ok || got != want {
			t.Errorf("ParseConfidence(%q) = %v %v", in, got, ok)
		}
	}
	if _, ok := ParseConfidence("certain"); ok {
		t.Error("ParseConfidence accepted junk")
	}
	for in, want := range map[string]Severity{"error": SevError, "Warn": SevWarning, "info": SevInfo, "none": "", "": ""} {
		if got, ok := ParseSeverity(in); !ok || got != want {
			t.Errorf("ParseSeverity(%q) = %q %v", in, got, ok)
		}
	}
	if _, ok := ParseSeverity("fatal"); ok {
		t.Error("ParseSeverity accepted junk")
	}
	if SevError.Rank() <= SevWarning.Rank() || SevWarning.Rank() <= SevInfo.Rank() || SevInfo.Rank() <= Severity("").Rank() {
		t.Error("severity ranks are not ordered")
	}
	if SeverityFor(High) != SevError || SeverityFor(Medium) != SevWarning || SeverityFor(Low) != SevInfo {
		t.Error("SeverityFor")
	}
}

func TestConfidenceJSON(t *testing.T) {
	b, err := json.Marshal(Reference{Kind: KindPath, Confidence: Medium})
	if err != nil || !contains(string(b), `"confidence":"medium"`) {
		t.Fatalf("marshal: %s %v", b, err)
	}
	var r Reference
	if err := json.Unmarshal([]byte(`{"kind":"path","confidence":"high"}`), &r); err != nil || r.Confidence != High {
		t.Errorf("unmarshal: %+v %v", r, err)
	}
	if err := json.Unmarshal([]byte(`{"confidence":"maybe"}`), &r); err == nil {
		t.Error("unmarshal accepted junk confidence")
	}
}

func TestFingerprintIgnoresPosition(t *testing.T) {
	a := NewFinding(RuleMissingPath, SevError, Reference{Kind: KindPath, Norm: "x.go", Loc: Location{File: "README.md", Line: 3, Col: 1}, Section: "A"}, "m")
	b := NewFinding(RuleMissingPath, SevError, Reference{Kind: KindPath, Norm: "x.go", Loc: Location{File: "README.md", Line: 90, Col: 7}, Section: "B"}, "m")
	c := NewFinding(RuleMissingPath, SevError, Reference{Kind: KindPath, Norm: "y.go", Loc: Location{File: "README.md", Line: 3}}, "m")
	if a.Fingerprint != b.Fingerprint {
		t.Error("line, column and section must not affect the fingerprint")
	}
	if a.Fingerprint == c.Fingerprint {
		t.Error("a different reference must fingerprint differently")
	}
	if a.Ref == nil || a.Ref.Norm != "x.go" || a.Loc.Line != 3 || len(a.Fingerprint) != 16 {
		t.Errorf("finding = %+v", a)
	}
	if Fingerprint("a", "b") != Fingerprint("a|b") || Fingerprint("a", "b") == Fingerprint("ab") {
		t.Error("parts are joined with '|'")
	}
}

func TestLocationString(t *testing.T) {
	cases := map[Location]string{
		{File: "f.md"}:                  "f.md",
		{File: "f.md", Line: 4}:         "f.md:4",
		{File: "f.md", Line: 4, Col: 2}: "f.md:4:2",
	}
	for l, want := range cases {
		if got := l.String(); got != want {
			t.Errorf("%+v → %q, want %q", l, got, want)
		}
	}
}

func TestRuleTablesAgree(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range AllRules {
		if seen[r] {
			t.Errorf("rule %q listed twice", r)
		}
		seen[r] = true
		if RuleDescriptions[r] == "" {
			t.Errorf("rule %q has no description", r)
		}
	}
	for r := range RuleDescriptions {
		if !seen[r] {
			t.Errorf("rule %q described but not listed in AllRules", r)
		}
	}
	kinds := map[Kind]bool{}
	for _, k := range AllKinds {
		if kinds[k] {
			t.Errorf("kind %q listed twice", k)
		}
		kinds[k] = true
	}
}

func TestProjectHasTarget(t *testing.T) {
	p := Project{Targets: map[string][]string{"make": {"build", "test", "test-%", "check-%-all"}, "just": {"run-%"}}}
	if !p.HasTarget("make", "build") || p.HasTarget("make", "lint") || p.HasTarget("npm", "build") {
		t.Error("HasTarget")
	}
	// a make pattern rule matches every name with something in place of %
	if !p.HasTarget("make", "test-full") || !p.HasTarget("make", "check-wasm-all") || p.HasTarget("make", "test-") || p.HasTarget("make", "check--all") || p.HasTarget("just", "run-x") {
		t.Error("HasTarget with a pattern rule")
	}
	if (Project{}).HasTarget("make", "build") {
		t.Error("zero Project has targets")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
