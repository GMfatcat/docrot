package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteJSONShape(t *testing.T) {
	m := decodeJSON(t, sampleReport(), Options{})

	for _, key := range []string{"version", "docrot", "summary", "findings", "coverage"} {
		if _, ok := m[key]; !ok {
			t.Errorf("top-level key %q missing", key)
		}
	}
	if got := m["version"]; got != float64(SchemaVersion) {
		t.Errorf("version = %v, want %d", got, SchemaVersion)
	}
	if got := m["docrot"]; got != "0.1.0" {
		t.Errorf("docrot = %v, want 0.1.0", got)
	}

	summary, ok := m["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary is %T, want an object", m["summary"])
	}
	wantSummary := map[string]any{
		"root":        "/repo",
		"docs":        float64(14),
		"references":  float64(1204),
		"errors":      float64(1),
		"warnings":    float64(1),
		"infos":       float64(1),
		"baselined":   float64(1),
		"fixed":       float64(1),
		"duration_ms": float64(830),
		"git":         "ok",
	}
	for k, want := range wantSummary {
		if got, ok := summary[k]; !ok {
			t.Errorf("summary key %q missing", k)
		} else if got != want {
			t.Errorf("summary[%q] = %v, want %v", k, got, want)
		}
	}
	for k := range summary {
		if strings.ToLower(k) != k || strings.Contains(k, " ") {
			t.Errorf("summary key %q is not snake_case", k)
		}
	}
	extra, ok := summary["extra"].(map[string]any)
	if !ok || extra["go packages"] != "14" {
		t.Errorf("summary.extra = %v, want {go packages: 14}", summary["extra"])
	}
}

func TestWriteJSONIncludesBaselined(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{"default", Options{}},
		{"show baselined", Options{ShowBaselined: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := decodeJSON(t, sampleReport(), tt.opts)
			findings, ok := m["findings"].([]any)
			if !ok {
				t.Fatalf("findings is %T, want an array", m["findings"])
			}
			if len(findings) != 4 {
				t.Fatalf("len(findings) = %d, want 4 (baselined ones are always included)", len(findings))
			}
			baselined := 0
			for _, raw := range findings {
				f := raw.(map[string]any)
				for _, key := range []string{"rule", "severity", "message", "loc", "fingerprint"} {
					if _, ok := f[key]; !ok {
						t.Errorf("finding missing key %q: %v", key, f)
					}
				}
				if f["baselined"] == true {
					baselined++
				}
			}
			if baselined != 1 {
				t.Errorf("baselined findings = %d, want 1", baselined)
			}
		})
	}
}

func TestWriteJSONSorted(t *testing.T) {
	m := decodeJSON(t, sampleReport(), Options{})
	findings := m["findings"].([]any)
	var files []string
	for _, raw := range findings {
		loc := raw.(map[string]any)["loc"].(map[string]any)
		files = append(files, loc["file"].(string))
	}
	want := []string{"README-zh.md", "README.md", "README.md", "llms.txt"}
	for i := range want {
		if files[i] != want[i] {
			t.Errorf("findings[%d].loc.file = %q, want %q", i, files[i], want[i])
		}
	}
}

func TestWriteJSONCoverage(t *testing.T) {
	m := decodeJSON(t, sampleReport(), Options{})
	cov, ok := m["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage is %T, want an object", m["coverage"])
	}
	pkgs := cov["packages"].([]any)
	if len(pkgs) != 1 {
		t.Fatalf("len(coverage.packages) = %d, want 1", len(pkgs))
	}
	p := pkgs[0].(map[string]any)
	if p["package"] != "httpx" || p["total"] != float64(4) || p["documented"] != float64(3) {
		t.Errorf("coverage.packages[0] = %v", p)
	}
	envs := cov["envs"].(map[string]any)
	if missing, ok := envs["missing"].([]any); !ok || len(missing) != 0 {
		t.Errorf("coverage.envs.missing = %v, want []", envs["missing"])
	}
}

func TestWriteJSONNilCoverage(t *testing.T) {
	r := sampleReport()
	r.Coverage = nil
	m := decodeJSON(t, r, Options{})
	v, ok := m["coverage"]
	if !ok {
		t.Fatal("coverage key missing; it should be present and null")
	}
	if v != nil {
		t.Errorf("coverage = %v, want null", v)
	}
}

func TestWriteJSONFormatting(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, &Report{Summary: Summary{Git: GitOK}}, Options{}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "}\n") {
		t.Error("JSON output should end with a newline")
	}
	if !strings.Contains(out, "\n  \"version\": 1,") {
		t.Errorf("JSON output should use two-space indentation:\n%s", out)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if fs, ok := m["findings"].([]any); !ok || len(fs) != 0 {
		t.Errorf("findings = %v, want an empty array rather than null", m["findings"])
	}
}
