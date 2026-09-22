package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v, want nil", err)
	}
	d := Default()
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"docs", d.Docs, []string{"**/*.md", "llms.txt"}},
		{"exclude", d.Exclude, []string{"vendor/**", "node_modules/**", "third_party/**", "3rdparty/**", "external/**", "**/testdata/**", "dist/**", ".git/**", ".*/**"}},
		{"ignore", d.Ignore, []string{}},
		{"pairs", d.Pairs, []Pair{}},
		{"stale", d.Stale, Stale{Enabled: true, MinChurn: 3, MinDays: 90, Exclude: []string{"CHANGELOG*.md", "CHANGES*.md", "HISTORY*.md", "**/superpowers/**", "**/specs/**", "**/plans/**", "**/*-report.md", "**/adr/**"}}},
		{"coverage", d.Coverage, Coverage{Report: false, IncludeInternal: false}},
		{"net", d.Net, false},
		{"failOn", d.FailOn, "error"},
		{"minConfidence", d.MinConfidence, "low"},
		{"severity", d.Severity, map[string]string{
			"stale-section": "warning", "pair-lag": "warning", "pair-number": "info"}},
		{"pairPatterns", d.PairPatterns, []string{
			"{stem}-zh.md", "{stem}_zh.md", "{stem}.zh.md", "{stem}.zh-TW.md", "{stem}-zh-TW.md"}},
		{"configSamples", d.ConfigSamples, []string{
			"config.json", "config*.json", "*.example.json", "*.sample.json", "configs/**/*.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("%s = %#v, want %#v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestDefaultRoundTrip(t *testing.T) {
	b, err := Default().Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.HasSuffix(string(b), "\n") {
		t.Error("Marshal output should end with a newline")
	}
	got, err := Parse("default.json", b)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Errorf("round trip changed the config:\n got %#v\nwant %#v", got, Default())
	}
	// The written JSON must use the documented key names.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"docs", "exclude", "ignore", "pairs", "pairPatterns",
		"configSamples", "stale", "coverage", "severity", "net", "failOn", "minConfidence"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("default JSON is missing key %q", key)
		}
	}
	if len(raw) != 13 {
		t.Errorf("default JSON has %d keys, want 13: %v", len(raw), raw)
	}
}

func TestParseOverlay(t *testing.T) {
	tests := []struct {
		name  string
		json  string
		check func(t *testing.T, c Config)
	}{
		{
			name: "empty object keeps every default",
			json: `{}`,
			check: func(t *testing.T, c Config) {
				if !reflect.DeepEqual(c, Default()) {
					t.Errorf("got %#v, want the defaults", c)
				}
			},
		},
		{
			name: "docs replaces the default list",
			json: `{"docs": ["README.md"]}`,
			check: func(t *testing.T, c Config) {
				if !reflect.DeepEqual(c.Docs, []string{"README.md"}) {
					t.Errorf("Docs = %v", c.Docs)
				}
				if !reflect.DeepEqual(c.Exclude, Default().Exclude) {
					t.Errorf("Exclude should be untouched, got %v", c.Exclude)
				}
			},
		},
		{
			name: "empty array clears a list",
			json: `{"exclude": []}`,
			check: func(t *testing.T, c Config) {
				if len(c.Exclude) != 0 {
					t.Errorf("Exclude = %v, want empty", c.Exclude)
				}
			},
		},
		{
			name: "nested object merges field by field",
			json: `{"stale": {"minChurn": 7}}`,
			check: func(t *testing.T, c Config) {
				if !c.Stale.Enabled || c.Stale.MinChurn != 7 || c.Stale.MinDays != 90 || len(c.Stale.Exclude) == 0 {
					t.Errorf("Stale = %+v, want enabled/7/90 with default exclude", c.Stale)
				}
			},
		},
		{
			name: "stale can be disabled",
			json: `{"stale": {"enabled": false}}`,
			check: func(t *testing.T, c Config) {
				if c.Stale.Enabled {
					t.Error("Stale.Enabled = true, want false")
				}
			},
		},
		{
			name: "severity map merges with the defaults",
			json: `{"severity": {"missing-path": "info", "pair-lag": "error"}}`,
			check: func(t *testing.T, c Config) {
				want := map[string]string{
					"stale-section": "warning",
					"pair-lag":      "error",
					"pair-number":   "info",
					"missing-path":  "info",
				}
				if !reflect.DeepEqual(c.Severity, want) {
					t.Errorf("Severity = %v, want %v", c.Severity, want)
				}
			},
		},
		{
			name: "pairs and scalars",
			json: `{"pairs": [{"source": "README.md", "translation": "README-zh.md"}],
			        "net": true, "failOn": "warning", "minConfidence": "medium",
			        "coverage": {"report": true}}`,
			check: func(t *testing.T, c Config) {
				want := []Pair{{Source: "README.md", Translation: "README-zh.md"}}
				if !reflect.DeepEqual(c.Pairs, want) {
					t.Errorf("Pairs = %v, want %v", c.Pairs, want)
				}
				if !c.Net || c.FailOn != "warning" || c.MinConfidence != "medium" {
					t.Errorf("scalars = %v %q %q", c.Net, c.FailOn, c.MinConfidence)
				}
				if !c.Coverage.Report || c.Coverage.IncludeInternal {
					t.Errorf("Coverage = %+v", c.Coverage)
				}
			},
		},
		{
			name: "ignore regexes",
			json: `{"ignore": ["^foo\\.", "bar$"]}`,
			check: func(t *testing.T, c Config) {
				res, err := c.IgnoreRegexps()
				if err != nil {
					t.Fatalf("IgnoreRegexps: %v", err)
				}
				if len(res) != 2 || !res[0].MatchString("foo.bar") || !res[1].MatchString("a bar") {
					t.Errorf("compiled regexps do not behave as expected: %v", res)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Parse("test.json", []byte(tt.json))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			tt.check(t, c)
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		contains []string
	}{
		{
			name: "syntax error reports the line",
			json: "{\n  \"docs\": [\"a.md\"],\n  \"net\": true,\n  \"failOn\" \"error\"\n}\n",
			// the missing colon is on line 4
			contains: []string{"cfg.json:4:", "invalid character"},
		},
		{
			name:     "syntax error on the first line",
			json:     "not json at all",
			contains: []string{"cfg.json:1:", "invalid character"},
		},
		{
			name:     "unknown field",
			json:     "{\n  \"docs\": [\"a.md\"],\n  \"documents\": [\"b.md\"]\n}\n",
			contains: []string{"cfg.json:3:", `unknown field "documents"`},
		},
		{
			name:     "wrong type",
			json:     "{\n  \"docs\": \"a.md\"\n}\n",
			contains: []string{"cfg.json:2:", `field "docs"`},
		},
		{
			name:     "wrong nested type",
			json:     "{\n  \"stale\": {\n    \"minChurn\": \"three\"\n  }\n}\n",
			contains: []string{"cfg.json:3:", "minChurn"},
		},
		{
			name:     "truncated object",
			json:     "{\n  \"docs\": [\"a.md\"]\n",
			contains: []string{"cfg.json", "unexpected end of file"},
		},
		{
			name:     "trailing data",
			json:     "{}\n{}\n",
			contains: []string{"cfg.json:2:", "trailing data"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse("cfg.json", []byte(tt.json))
			if err == nil {
				t.Fatalf("Parse(%q) = %#v, want an error", tt.json, cfg)
			}
			for _, want := range tt.contains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err.Error(), want)
				}
			}
			if !reflect.DeepEqual(cfg, Default()) {
				t.Error("a failed Parse should return the defaults")
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr string // substring; "" means valid
	}{
		{"defaults", func(c *Config) {}, ""},
		{"failOn none", func(c *Config) { c.FailOn = "none" }, ""},
		{"failOn empty", func(c *Config) { c.FailOn = "" }, ""},
		{"failOn bad", func(c *Config) { c.FailOn = "fatal" }, "failOn"},
		{"minConfidence high", func(c *Config) { c.MinConfidence = "high" }, ""},
		{"minConfidence empty", func(c *Config) { c.MinConfidence = "" }, ""},
		{"minConfidence bad", func(c *Config) { c.MinConfidence = "maybe" }, "minConfidence"},
		{"severity ok", func(c *Config) { c.Severity["missing-path"] = "error" }, ""},
		{"severity bad value", func(c *Config) { c.Severity["missing-path"] = "fatal" }, "severity"},
		{"severity empty rule", func(c *Config) { c.Severity[""] = "error" }, "severity"},
		{"ignore ok", func(c *Config) { c.Ignore = []string{`^x\d+$`} }, ""},
		{"ignore bad regex", func(c *Config) { c.Ignore = []string{"("} }, "ignore[0]"},
		{"docs bad glob", func(c *Config) { c.Docs = []string{"[bad"} }, "docs"},
		{"exclude bad glob", func(c *Config) { c.Exclude = []string{"[bad"} }, "exclude"},
		{"configSamples bad glob", func(c *Config) { c.ConfigSamples = []string{"[bad"} }, "configSamples"},
		{"docs empty pattern", func(c *Config) { c.Docs = []string{" "} }, "docs"},
		{"pairPattern without stem", func(c *Config) { c.PairPatterns = []string{"x-zh.md"} }, "{stem}"},
		{"pair missing translation", func(c *Config) { c.Pairs = []Pair{{Source: "a.md"}} }, "pairs[0]"},
		{"negative minChurn", func(c *Config) { c.Stale.MinChurn = -1 }, "stale.minChurn"},
		{"negative minDays", func(c *Config) { c.Stale.MinDays = -5 }, "stale.minDays"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			tt.mutate(&c)
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadAndFind(t *testing.T) {
	t.Run("load from disk", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, ".docrot.json")
		if err := os.WriteFile(p, []byte(`{"failOn": "warning"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.FailOn != "warning" {
			t.Errorf("FailOn = %q", c.FailOn)
		}
		got, path, err := LoadOrDefault(dir)
		if err != nil || path != p || got.FailOn != "warning" {
			t.Errorf("LoadOrDefault = %v, %q, %v", got.FailOn, path, err)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := Load(filepath.Join(dir, ".docrot.json")); err == nil {
			t.Error("Load of a missing file should fail")
		}
		if p, ok := Find(dir); ok {
			t.Errorf("Find = %q, true; want not found", p)
		}
		c, path, err := LoadOrDefault(dir)
		if err != nil || path != "" || !reflect.DeepEqual(c, Default()) {
			t.Errorf("LoadOrDefault = %v, %q, %v; want the defaults", c, path, err)
		}
	})
	t.Run("find prefers the dotted name", func(t *testing.T) {
		dir := t.TempDir()
		dotted := filepath.Join(dir, ".docrot.json")
		plain := filepath.Join(dir, "docrot.json")
		for _, p := range []string{plain, dotted} {
			if err := os.WriteFile(p, []byte(`{}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if p, ok := Find(dir); !ok || p != dotted {
			t.Errorf("Find = %q, %v; want %q", p, ok, dotted)
		}
	})
	t.Run("find the plain name", func(t *testing.T) {
		dir := t.TempDir()
		plain := filepath.Join(dir, "docrot.json")
		if err := os.WriteFile(plain, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if p, ok := Find(dir); !ok || p != plain {
			t.Errorf("Find = %q, %v; want %q", p, ok, plain)
		}
	})
	t.Run("directory named like a config is ignored", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".docrot.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		if p, ok := Find(dir); ok {
			t.Errorf("Find = %q, true; want not found", p)
		}
	})
}

func TestWriteDefault(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".docrot.json")
	if err := WriteDefault(p); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(c, Default()) {
		t.Errorf("written config does not load back as the defaults: %#v", c)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\n  \"docs\": [") {
		t.Errorf("written config is not pretty printed:\n%s", b)
	}
	err = WriteDefault(p)
	if err == nil {
		t.Fatal("WriteDefault must refuse to overwrite an existing file")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to mention that the file exists", err)
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(b) {
		t.Error("the existing file was modified")
	}
}

func TestExcludePatternsAndSeverityFor(t *testing.T) {
	c := Default()
	ps, err := c.ExcludePatterns()
	if err != nil {
		t.Fatalf("ExcludePatterns: %v", err)
	}
	if len(ps) != len(c.Exclude) {
		t.Fatalf("got %d patterns, want %d", len(ps), len(c.Exclude))
	}
	c.Exclude = []string{"[bad"}
	if _, err := c.ExcludePatterns(); err == nil {
		t.Error("want an error for a malformed exclude pattern")
	}
	if sev, ok := Default().SeverityFor("pair-lag"); !ok || sev != "warning" {
		t.Errorf("SeverityFor(pair-lag) = %q, %v", sev, ok)
	}
	if _, ok := Default().SeverityFor("missing-path"); ok {
		t.Error("SeverityFor(missing-path) should report no override")
	}
}

func TestLineOf(t *testing.T) {
	data := []byte("a\nbb\nccc\n")
	tests := []struct {
		offset int64
		want   int
	}{
		{0, 1}, {1, 1}, {2, 2}, {4, 2}, {5, 3}, {9, 4}, {-1, 1}, {100, 4},
	}
	for _, tt := range tests {
		if got := lineOf(data, tt.offset); got != tt.want {
			t.Errorf("lineOf(%d) = %d, want %d", tt.offset, got, tt.want)
		}
	}
}
