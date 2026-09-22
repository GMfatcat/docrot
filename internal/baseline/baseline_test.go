package baseline

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"docrot/internal/model"
)

// finding is a small helper for building test findings.
func finding(rule, file, fp string) model.Finding {
	return model.Finding{
		Rule:        rule,
		Severity:    model.SevError,
		Message:     rule + " in " + file,
		Loc:         model.Location{File: file, Line: 1},
		Fingerprint: fp,
	}
}

func TestEntries(t *testing.T) {
	tests := []struct {
		name     string
		findings []model.Finding
		want     []Entry
	}{
		{"empty", nil, []Entry{}},
		{
			name:     "sorted by fingerprint",
			findings: []model.Finding{finding("missing-path", "b.md", "ff"), finding("missing-symbol", "a.md", "0a")},
			want: []Entry{
				{Fingerprint: "0a", Rule: "missing-symbol", File: "a.md", Message: "missing-symbol in a.md"},
				{Fingerprint: "ff", Rule: "missing-path", File: "b.md", Message: "missing-path in b.md"},
			},
		},
		{
			name: "duplicate fingerprints collapse",
			findings: []model.Finding{
				finding("missing-path", "a.md", "aa"),
				finding("missing-path", "a.md", "aa"),
			},
			want: []Entry{{Fingerprint: "aa", Rule: "missing-path", File: "a.md", Message: "missing-path in a.md"}},
		},
		{
			name:     "no fingerprint is skipped",
			findings: []model.Finding{finding("pair-lag", "a.md", "")},
			want:     []Entry{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Entries(tt.findings); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Entries() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultName)
	findings := []model.Finding{
		finding("missing-path", "b.md", "ff11"),
		finding("missing-symbol", "a.md", "00aa"),
	}
	before := time.Now().UTC().Truncate(time.Second)
	if err := Save(path, findings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(raw)
	if !strings.HasSuffix(text, "}\n") {
		t.Errorf("baseline should end with a newline, got %q", text[max(0, len(text)-4):])
	}
	if !strings.Contains(text, "\n  \"version\": 1") {
		t.Errorf("baseline should be indented with two spaces, got:\n%s", text)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != Version {
		t.Errorf("Version = %d, want %d", got.Version, Version)
	}
	if got.Generated.Before(before) || got.Generated.Sub(got.Generated.Truncate(time.Second)) != 0 {
		t.Errorf("Generated = %v, want a whole second at or after %v", got.Generated, before)
	}
	if got.Generated.Location() != time.UTC {
		// JSON round-trips as UTC ("Z"); this guards Save writing local time.
		if _, off := got.Generated.Zone(); off != 0 {
			t.Errorf("Generated zone offset = %d, want UTC", off)
		}
	}
	want := []Entry{
		{Fingerprint: "00aa", Rule: "missing-symbol", File: "a.md", Message: "missing-symbol in a.md"},
		{Fingerprint: "ff11", Rule: "missing-path", File: "b.md", Message: "missing-path in b.md"},
	}
	if !reflect.DeepEqual(got.Findings, want) {
		t.Errorf("Findings = %+v, want %+v", got.Findings, want)
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file wraps fs.ErrNotExist", func(t *testing.T) {
		_, err := Load(filepath.Join(dir, "nope.json"))
		if err == nil {
			t.Fatal("Load of a missing file returned nil error")
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("errors.Is(err, fs.ErrNotExist) = false for %v", err)
		}
	})

	t.Run("bad json", func(t *testing.T) {
		p := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(p, []byte("{nope"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Error("Load of malformed JSON returned nil error")
		}
	})

	t.Run("wrong version", func(t *testing.T) {
		p := filepath.Join(dir, "v9.json")
		data, _ := json.Marshal(File{Version: 9})
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Load(p)
		if err == nil || !strings.Contains(err.Error(), "unsupported version") {
			t.Errorf("Load of version 9 = %v, want an unsupported-version error", err)
		}
	})
}

func TestApply(t *testing.T) {
	base := &File{Version: Version, Findings: []Entry{
		{Fingerprint: "aa", Rule: "missing-path", File: "a.md"},
		{Fingerprint: "bb", Rule: "missing-symbol", File: "b.md"},
	}}

	tests := []struct {
		name          string
		base          *File
		findings      []model.Finding
		wantBaselined int
		wantNew       int
		wantFixed     []string
		wantFlags     []bool
	}{
		{
			name:      "nil baseline: everything is new",
			base:      nil,
			findings:  []model.Finding{finding("missing-path", "a.md", "aa")},
			wantNew:   1,
			wantFlags: []bool{false},
		},
		{
			name: "mixed",
			base: base,
			findings: []model.Finding{
				finding("missing-path", "a.md", "aa"),
				finding("missing-path", "c.md", "cc"),
			},
			wantBaselined: 1,
			wantNew:       1,
			wantFixed:     []string{"bb"},
			wantFlags:     []bool{true, false},
		},
		{
			name:      "all fixed",
			base:      base,
			findings:  nil,
			wantFixed: []string{"aa", "bb"},
		},
		{
			name:      "empty fingerprint never matches",
			base:      base,
			findings:  []model.Finding{finding("pair-lag", "a.md", "")},
			wantNew:   1,
			wantFixed: []string{"aa", "bb"},
			wantFlags: []bool{false},
		},
		{
			name: "stale Baselined flag is cleared",
			base: base,
			findings: []model.Finding{
				func() model.Finding { f := finding("missing-path", "c.md", "cc"); f.Baselined = true; return f }(),
			},
			wantNew:   1,
			wantFixed: []string{"aa", "bb"},
			wantFlags: []bool{false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := append([]model.Finding(nil), tt.findings...)
			baselined, newCount, fixed := Apply(tt.base, findings)
			if baselined != tt.wantBaselined {
				t.Errorf("baselined = %d, want %d", baselined, tt.wantBaselined)
			}
			if newCount != tt.wantNew {
				t.Errorf("newCount = %d, want %d", newCount, tt.wantNew)
			}
			var gotFixed []string
			for _, e := range fixed {
				gotFixed = append(gotFixed, e.Fingerprint)
			}
			if !reflect.DeepEqual(gotFixed, tt.wantFixed) {
				t.Errorf("fixed = %v, want %v", gotFixed, tt.wantFixed)
			}
			for i, want := range tt.wantFlags {
				if findings[i].Baselined != want {
					t.Errorf("findings[%d].Baselined = %v, want %v", i, findings[i].Baselined, want)
				}
			}
		})
	}
}

func TestFileHas(t *testing.T) {
	var nilFile *File
	if nilFile.Has("aa") {
		t.Error("nil *File should freeze nothing")
	}
	f := &File{Findings: []Entry{{Fingerprint: "aa"}}}
	if !f.Has("aa") {
		t.Error(`Has("aa") = false, want true`)
	}
	if f.Has("bb") {
		t.Error(`Has("bb") = true, want false`)
	}
}

func TestDefaultName(t *testing.T) {
	if DefaultName != ".docrot-baseline.json" {
		t.Errorf("DefaultName = %q", DefaultName)
	}
}
