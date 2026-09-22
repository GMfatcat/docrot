package resolve

import (
	"strings"
	"testing"

	"docrot/internal/model"
)

// fakeIndex is a map-backed model.Index for unit tests.
type fakeIndex struct {
	files, dirs map[string]bool
	module      string
	pkgs, types map[string]bool
	symbols     map[string]string // qualified → file
	flags, envs []string
	jsonKeys    []string
	cfgKeys     []string
	anchors     map[string][]string
	odin, py    map[string]bool
}

func (f *fakeIndex) FileExists(rel string) bool { return f.files[rel] }
func (f *fakeIndex) DirExists(rel string) bool  { return f.dirs[rel] }
func (f *fakeIndex) Glob(p string) []string {
	var out []string
	for x := range f.files {
		if strings.HasPrefix(x, strings.TrimSuffix(p, "*")) {
			out = append(out, x)
		}
	}
	return out
}
func (f *fakeIndex) SimilarPaths(rel string, n int) []string {
	base := rel[strings.LastIndex(rel, "/")+1:]
	var out []string
	for x := range f.files {
		if strings.HasSuffix(x, "/"+base) || x == base {
			out = append(out, x)
		}
	}
	return out
}
func (f *fakeIndex) TopLevelDirs() []string { return nil }
func (f *fakeIndex) ModulePath() string     { return f.module }
func (f *fakeIndex) GoPackages() []string {
	var out []string
	for p := range f.pkgs {
		out = append(out, p)
	}
	return out
}
func (f *fakeIndex) IsGoPackage(n string) bool { return f.pkgs[n] }
func (f *fakeIndex) GoPackageDir(ip string) (string, bool) {
	d := strings.TrimPrefix(strings.TrimPrefix(ip, f.module), "/")
	if f.dirs[d] {
		return d, true
	}
	return "", false
}
func (f *fakeIndex) HasGoSymbol(q string) bool { _, ok := f.symbols[q]; return ok }
func (f *fakeIndex) GoSymbolFile(q string) (string, bool) {
	s, ok := f.symbols[q]
	return s, ok
}
func (f *fakeIndex) SimilarGoSymbols(q string, n int) []string {
	pkg := q[:strings.Index(q+".", ".")]
	var out []string
	for s := range f.symbols {
		if strings.HasPrefix(s, pkg+".") && s != q {
			out = append(out, s)
		}
	}
	return out
}
func (f *fakeIndex) IsGoType(n string) bool { return f.types[n] }
func (f *fakeIndex) HasFlag(n string) bool {
	for _, x := range f.flags {
		if x == n {
			return true
		}
	}
	return false
}
func (f *fakeIndex) Flags() []string { return f.flags }
func (f *fakeIndex) HasEnv(n string) bool {
	for _, x := range f.envs {
		if x == n {
			return true
		}
	}
	return false
}
func (f *fakeIndex) Envs() []string { return f.envs }
func (f *fakeIndex) HasJSONKey(d string) bool {
	for _, x := range f.jsonKeys {
		if x == d {
			return true
		}
	}
	return false
}
func (f *fakeIndex) JSONKeys() []string                      { return f.jsonKeys }
func (f *fakeIndex) GoExported() []model.Exported            { return nil }
func (f *fakeIndex) HasOdin() bool                           { return len(f.odin) > 0 }
func (f *fakeIndex) OdinPackages() []string                  { return nil }
func (f *fakeIndex) HasOdinSymbol(q string) bool             { return f.odin[q] }
func (f *fakeIndex) SimilarOdinSymbols(string, int) []string { return nil }
func (f *fakeIndex) HasPython() bool                         { return len(f.py) > 0 }
func (f *fakeIndex) PyModules() []string                     { return nil }
func (f *fakeIndex) HasPySymbol(q string) bool               { return f.py[q] }
func (f *fakeIndex) SimilarPySymbols(string, int) []string   { return nil }
func (f *fakeIndex) HasAnchor(doc, slug string) bool {
	for _, a := range f.anchors[doc] {
		if a == slug {
			return true
		}
	}
	return false
}
func (f *fakeIndex) Anchors(doc string) []string { return f.anchors[doc] }
func (f *fakeIndex) HasConfigKey(d string) bool {
	for _, x := range f.cfgKeys {
		if x == d {
			return true
		}
	}
	return false
}
func (f *fakeIndex) ConfigKeys() []string { return f.cfgKeys }

func newFake() *fakeIndex {
	return &fakeIndex{
		files:    map[string]bool{"README.md": true, "docs/guide.md": true, "pkg/httpx/server.go": true, "scripts/verify.ps1": true, "cmd/app/main.go": true},
		dirs:     map[string]bool{"docs": true, "pkg": true, "pkg/httpx": true, "cmd": true, "cmd/app": true, "scripts": true},
		module:   "example.com/fixture",
		pkgs:     map[string]bool{"httpx": true, "main": true},
		types:    map[string]bool{"Server": true},
		symbols:  map[string]string{"httpx.NewServer": "pkg/httpx/server.go", "httpx.WriteData": "pkg/httpx/server.go", "Server.Addr": "pkg/httpx/server.go", "httpx.Server.Addr": "pkg/httpx/server.go", "NewServer": "pkg/httpx/server.go"},
		flags:    []string{"addr", "config", "verbose"},
		envs:     []string{"FIXTURE_DEBUG"},
		jsonKeys: []string{"server", "server.addr"},
		anchors:  map[string][]string{"docs/guide.md": {"setup", "install"}, "README.md": {"usage"}},
	}
}

func ref(kind model.Kind, norm string, conf model.Confidence, file string) model.Reference {
	return model.Reference{Kind: kind, Text: norm, Norm: norm, Confidence: conf, Loc: model.Location{File: file, Line: 1}}
}

func TestResolvePolicy(t *testing.T) {
	ix := newFake()
	r := New(ix, Options{Renames: map[string]string{"old/name.go": "pkg/httpx/server.go"}})
	tests := []struct {
		name    string
		ref     model.Reference
		ok      bool
		skipped bool
		rule    string
		sev     model.Severity
		sugg    string
		file    string
	}{
		{"path root", ref(model.KindPath, "pkg/httpx/server.go", model.High, "README.md"), true, false, "", "", "", "pkg/httpx/server.go"},
		{"path doc-relative", ref(model.KindPath, "../README.md", model.High, "docs/guide.md"), true, false, "", "", "", "README.md"},
		{"path missing high", ref(model.KindPath, "pkg/httpx/router.go", model.High, "README.md"), false, false, model.RuleMissingPath, model.SevError, "", ""},
		{"path missing suggests same basename", ref(model.KindPath, "docs/main.go", model.High, "README.md"), false, false, model.RuleMissingPath, model.SevError, "cmd/app/main.go", ""},
		{"bare filename nowhere is info", ref(model.KindPath, "nothing.go", model.Medium, "README.md"), false, false, model.RuleMissingPath, model.SevInfo, "", ""},
		{"renamed", ref(model.KindPath, "old/name.go", model.High, "README.md"), false, false, model.RuleMissingPath, model.SevError, "pkg/httpx/server.go", ""},
		{"glob ok", ref(model.KindPath, "pkg/httpx/*", model.High, "README.md"), true, false, "", "", "", "pkg/httpx"},
		{"command missing", ref(model.KindCommand, "scripts/build.ps1", model.High, "README.md"), false, false, model.RuleMissingCommand, model.SevError, "", ""},
		{"gosym ok", ref(model.KindGoSymbol, "httpx.NewServer", model.High, "README.md"), true, false, "", "", "", "pkg/httpx/server.go"},
		{"gosym missing in pkg", ref(model.KindGoSymbol, "httpx.WriteJSON", model.High, "README.md"), false, false, model.RuleMissingSymbol, model.SevError, "", ""},
		{"gosym type method missing", ref(model.KindGoSymbol, "Server.Address", model.High, "README.md"), false, false, model.RuleMissingSymbol, model.SevError, "", ""},
		{"gosym receiver var skipped", ref(model.KindGoSymbol, "app.Run", model.Low, "README.md"), false, true, "", "", "", ""},
		{"gosym unknown pkg capitalised is info", ref(model.KindGoSymbol, "Foo.Bar", model.Low, "README.md"), false, false, model.RuleMissingSymbol, model.SevInfo, "", ""},
		{"flag ok", ref(model.KindFlag, "addr", model.High, "README.md"), true, false, "", "", "", ""},
		{"flag high unknown is warning", ref(model.KindFlag, "port", model.High, "README.md"), false, false, model.RuleUnknownFlag, model.SevWarning, "", ""},
		{"flag typo suggests", ref(model.KindFlag, "confg", model.High, "README.md"), false, false, model.RuleUnknownFlag, model.SevWarning, "--config", ""},
		{"flag medium is info", ref(model.KindFlag, "port", model.Medium, "README.md"), false, false, model.RuleUnknownFlag, model.SevInfo, "", ""},
		{"flag low skipped", ref(model.KindFlag, "race", model.Low, "README.md"), false, true, "", "", "", ""},
		{"env ok", ref(model.KindEnv, "FIXTURE_DEBUG", model.High, "README.md"), true, false, "", "", "", ""},
		{"env unknown warning", ref(model.KindEnv, "FIXTURE_TRACE", model.High, "README.md"), false, false, model.RuleUnknownEnv, model.SevWarning, "", ""},
		{"env external skipped", ref(model.KindEnv, "GIT_AUTHOR_DATE", model.High, "README.md"), false, true, "", "", "", ""},
		{"configkey ok", ref(model.KindConfigKey, "server.addr", model.Low, "README.md"), true, false, "", "", "", ""},
		{"configkey missing info", ref(model.KindConfigKey, "server.port", model.Low, "README.md"), false, false, model.RuleUnknownConfigKey, model.SevInfo, "", ""},
		{"configkey domain skipped", ref(model.KindConfigKey, "example.com", model.Low, "README.md"), false, true, "", "", "", ""},
		{"anchor ok", ref(model.KindAnchor, "docs/guide.md#setup", model.High, "README.md"), true, false, "", "", "", "docs/guide.md"},
		{"anchor same doc", ref(model.KindAnchor, "#usage", model.High, "README.md"), true, false, "", "", "", "README.md"},
		{"anchor broken suggests", ref(model.KindAnchor, "docs/guide.md#instal", model.High, "README.md"), false, false, model.RuleBrokenAnchor, model.SevError, "#install", ""},
		{"anchor missing file skipped", ref(model.KindAnchor, "docs/nope.md#x", model.High, "README.md"), false, true, "", "", "", ""},
		{"import ok", ref(model.KindImport, "example.com/fixture/pkg/httpx", model.High, "README.md"), true, false, "", "", "", "pkg/httpx"},
		{"import missing", ref(model.KindImport, "example.com/fixture/pkg/router", model.High, "README.md"), false, false, model.RuleMissingImport, model.SevError, "", ""},
		{"import foreign skipped", ref(model.KindImport, "github.com/x/y", model.High, "README.md"), false, true, "", "", "", ""},
		{"url without net skipped", ref(model.KindURL, "https://example.com", model.Low, "README.md"), false, true, "", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := r.Resolve(tc.ref)
			if res.OK != tc.ok || res.Skipped != tc.skipped {
				t.Fatalf("ok=%v skipped=%v, want ok=%v skipped=%v (finding=%+v)", res.OK, res.Skipped, tc.ok, tc.skipped, res.Finding)
			}
			if tc.file != "" && res.File != tc.file {
				t.Errorf("file=%q want %q", res.File, tc.file)
			}
			if tc.rule == "" {
				if res.Finding != nil {
					t.Fatalf("unexpected finding %+v", res.Finding)
				}
				return
			}
			if res.Finding == nil {
				t.Fatalf("expected finding %s", tc.rule)
			}
			if res.Finding.Rule != tc.rule || res.Finding.Severity != tc.sev {
				t.Errorf("rule=%s sev=%s, want %s %s", res.Finding.Rule, res.Finding.Severity, tc.rule, tc.sev)
			}
			if tc.sugg != "" && res.Finding.Suggestion != tc.sugg {
				t.Errorf("suggestion=%q want %q", res.Finding.Suggestion, tc.sugg)
			}
			if res.Finding.Fingerprint == "" {
				t.Error("empty fingerprint")
			}
		})
	}
}

func TestSeverityOverrideAndMinConfidence(t *testing.T) {
	ix := newFake()
	r := New(ix, Options{Severity: map[string]model.Severity{model.RuleMissingPath: model.SevInfo}, MinConfidence: model.Medium})
	res := r.Resolve(ref(model.KindPath, "nope/x.go", model.High, "README.md"))
	if res.Finding == nil || res.Finding.Severity != model.SevInfo {
		t.Fatalf("override not applied: %+v", res.Finding)
	}
	if res := r.Resolve(ref(model.KindPath, "nope/x.go", model.Low, "README.md")); !res.Skipped {
		t.Fatal("low confidence should be skipped with MinConfidence=medium")
	}
}

func TestNoFlagsInRepoSkipsFlagRefs(t *testing.T) {
	ix := newFake()
	ix.flags = nil
	r := New(ix, Options{})
	if res := r.Resolve(ref(model.KindFlag, "whatever", model.High, "README.md")); !res.Skipped {
		t.Fatal("flags should be skipped when the code defines none")
	}
}

func TestOtherSymbolFallsBackToConfigKey(t *testing.T) {
	ix := newFake()
	ix.odin = map[string]bool{"fixture_odin.render_frame": true}
	r := New(ix, Options{})
	if res := r.Resolve(ref(model.KindOdinSym, "server.addr", model.Low, "README.md")); !res.OK {
		t.Fatalf("low odinsym matching a config key should be ok: %+v", res.Finding)
	}
	if res := r.Resolve(ref(model.KindOdinSym, "render_frames", model.Medium, "README.md")); res.Finding == nil || res.Finding.Severity != model.SevWarning {
		t.Fatalf("medium odinsym miss should warn: %+v", res.Finding)
	}
}
