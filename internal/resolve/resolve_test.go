package resolve

import (
	"os"
	"path/filepath"
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
	pyMods      map[string]bool
	more        map[model.Kind]map[string]bool // further languages' symbols
	nsOf        map[model.Kind]map[string]bool // their namespaces
	similar     map[model.Kind][]string        // canned SimilarSymbols answers
	defaults    map[string]string              // "flag:addr" → ":8080"
	routes      []string                       // "GET /x" or "/x"
	literals    map[string]bool
	project     model.Project
}

func (f *fakeIndex) FileExists(rel string) bool { return f.files[rel] }
func (f *fakeIndex) DirExists(rel string) bool  { return f.dirs[rel] }
func (f *fakeIndex) Glob(p string) []string {
	var out []string
	for x := range f.files {
		switch {
		case strings.HasPrefix(p, "**/*"):
			if strings.HasSuffix(x, strings.TrimPrefix(p, "**/*")) {
				out = append(out, x)
			}
		case strings.HasPrefix(x, strings.TrimSuffix(p, "*")):
			out = append(out, x)
		}
	}
	return out
}
func (f *fakeIndex) SimilarPaths(rel string, n int) []string {
	base := rel[strings.LastIndex(rel, "/")+1:]
	var out []string
	for x := range f.files {
		if strings.EqualFold(x, rel) {
			out = append([]string{x}, out...)
			continue
		}
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
func (f *fakeIndex) HasGoMember(n string) bool {
	for q := range f.symbols {
		if i := strings.LastIndex(q, "."); i >= 0 && q[i+1:] == n && strings.Count(q, ".") >= 1 && f.types[q[:i][strings.LastIndex(q[:i], ".")+1:]] {
			return true
		}
	}
	return false
}
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
func (f *fakeIndex) JSONKeys() []string           { return f.jsonKeys }
func (f *fakeIndex) GoExported() []model.Exported { return nil }
func (f *fakeIndex) langs() map[model.Kind]map[string]bool {
	m := map[model.Kind]map[string]bool{}
	if len(f.odin) > 0 {
		m[model.KindOdinSym] = f.odin
	}
	if len(f.py) > 0 {
		m[model.KindPySym] = f.py
	}
	for k, v := range f.more {
		m[k] = v
	}
	return m
}
func (f *fakeIndex) Languages() []model.Kind {
	var out []model.Kind
	for _, l := range model.Langs {
		if _, ok := f.langs()[l.Kind]; ok {
			out = append(out, l.Kind)
		}
	}
	return out
}
func (f *fakeIndex) HasLang(k model.Kind) bool      { _, ok := f.langs()[k]; return ok }
func (f *fakeIndex) Namespaces(model.Kind) []string { return nil }
func (f *fakeIndex) IsNamespace(k model.Kind, q string) bool {
	return k == model.KindPySym && f.pyMods[q] || f.nsOf[k] != nil && f.nsOf[k][q]
}
func (f *fakeIndex) IsExample(model.Kind, string) bool     { return false }
func (f *fakeIndex) HasSymbol(k model.Kind, q string) bool { return f.langs()[k][q] }
func (f *fakeIndex) Opaque(model.Kind, string) bool        { return true }
func (f *fakeIndex) SimilarSymbols(k model.Kind, q string, n int) []string {
	if f.similar != nil {
		return f.similar[k]
	}
	return nil
}
func (f *fakeIndex) HasLiteral(s string) bool { return f.literals[s] }
func (f *fakeIndex) Project() model.Project   { return f.project }
func (f *fakeIndex) Default(kind, name string) (string, bool) {
	v, ok := f.defaults[kind+":"+name]
	return v, ok
}
func (f *fakeIndex) HasRoutes() bool { return len(f.routes) > 0 }
func (f *fakeIndex) MatchRoute(method, p string) model.RouteMatch {
	var methods []string
	for _, r := range f.routes { // "GET /x" or "/x"
		m, rp, ok := strings.Cut(r, " ")
		if !ok {
			m, rp = "", r
		}
		if rp != p {
			continue
		}
		if m == "" || method == "" || m == method {
			return model.RouteMatch{OK: true, File: "routes.go"}
		}
		methods = append(methods, m)
	}
	return model.RouteMatch{Methods: methods}
}
func (f *fakeIndex) Routes() []string { return f.routes }
func (f *fakeIndex) SimilarRoutes(p string, n int) []string {
	var out []string
	for _, r := range f.routes {
		_, rp, ok := strings.Cut(r, " ")
		if !ok {
			rp = r
		}
		if len(p) > 3 && strings.HasPrefix(rp, p[:3]) && rp != p {
			out = append(out, rp)
		}
	}
	return out
}
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
		files:    map[string]bool{"README.md": true, "docs/guide.md": true, "pkg/httpx/server.go": true, "scripts/verify.ps1": true, "cmd/app/main.go": true, "pkg/httpx/server_test.go": true},
		dirs:     map[string]bool{"docs": true, "pkg": true, "pkg/httpx": true, "cmd": true, "cmd/app": true, "scripts": true},
		module:   "example.com/fixture",
		pkgs:     map[string]bool{"httpx": true, "main": true, "cfg": true},
		types:    map[string]bool{"Server": true},
		symbols:  map[string]string{"httpx.NewServer": "pkg/httpx/server.go", "httpx.WriteData": "pkg/httpx/server.go", "Server.Addr": "pkg/httpx/server.go", "httpx.Server.Addr": "pkg/httpx/server.go", "NewServer": "pkg/httpx/server.go"},
		flags:    []string{"addr", "config", "verbose"},
		envs:     []string{"FIXTURE_DEBUG"},
		jsonKeys: []string{"server", "server.addr"},
		anchors:  map[string][]string{"docs/guide.md": {"setup", "install"}, "README.md": {"usage"}},
		routes:   []string{"GET /v1/items", "POST /v1/items", "/healthz"},
		literals: map[string]bool{"/openapi.json": true, "http.requests": true, "emit_event": true, "dry-run": true, "FIXTURE_HOME": true, "server.tls": true},
		defaults: map[string]string{"flag:addr": ":8080", "key:log.level": "info", "flag:timeout": "30s"},
		project: model.Project{
			GoModule: "example.com/fixture", GoVersion: "1.22",
			PyName: "my-tool", PyRequires: ">=3.10",
			NPMName:     "@acme/tool",
			Targets:     map[string][]string{"make": {"build", "test"}},
			TargetFiles: map[string]string{"make": "Makefile"},
		},
	}
}

func ref(kind model.Kind, norm string, conf model.Confidence, file string) model.Reference {
	return model.Reference{Kind: kind, Text: norm, Norm: norm, Confidence: conf, Loc: model.Location{File: file, Line: 1}}
}

func jsonRef(key string, conf model.Confidence) model.Reference {
	r := ref(model.KindConfigKey, key, conf, "README.md")
	r.Lang = "json"
	return r
}

func envRef(name, context string) model.Reference {
	r := ref(model.KindEnv, name, model.High, "README.md")
	r.Context = context
	return r
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
		{"bare filename elsewhere is info with suggestion", ref(model.KindPath, "main.go", model.Medium, "README.md"), false, false, model.RuleMissingPath, model.SevInfo, "cmd/app/main.go", ""},
		{"bare glob matches anywhere", ref(model.KindPath, "*_test.go", model.Medium, "README.md"), true, false, "", "", "", ""},
		{"gosym receiver collides with package", ref(model.KindGoSymbol, "cfg.Addr", model.High, "README.md"), false, false, model.RuleMissingSymbol, model.SevInfo, "", ""},
		{"gosym real miss in long package stays error", ref(model.KindGoSymbol, "httpx.Addr", model.High, "README.md"), false, false, model.RuleMissingSymbol, model.SevError, "", ""},
		{"gosym bare call medium is info", ref(model.KindGoSymbol, "Shutdown", model.Medium, "README.md"), false, false, model.RuleMissingSymbol, model.SevInfo, "", ""},
		{"gosym snake key falls to config", ref(model.KindGoSymbol, "httpx.max_conns", model.High, "README.md"), false, true, "", "", "", ""},
		{"import module root", ref(model.KindImport, "example.com/fixture", model.High, "README.md"), true, false, "", "", "", ""},
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
		{"env unknown warning", envRef("FIXTURE_TRACE", "set the FIXTURE_TRACE env var"), false, false, model.RuleUnknownEnv, model.SevWarning, "", ""},
		{"env without env context skipped", envRef("OUT_OF_RANGE", "error code OUT_OF_RANGE is returned"), false, true, "", "", "", ""},
		{"env external skipped", ref(model.KindEnv, "GIT_AUTHOR_DATE", model.High, "README.md"), false, true, "", "", "", ""},
		{"configkey ok", ref(model.KindConfigKey, "server.addr", model.Low, "README.md"), true, false, "", "", "", ""},
		{"configkey missing info", ref(model.KindConfigKey, "server.port", model.Low, "README.md"), false, false, model.RuleUnknownConfigKey, model.SevInfo, "", ""},
		{"configkey unknown section skipped", ref(model.KindConfigKey, "rec.status", model.Low, "README.md"), false, true, "", "", "", ""},
		{"configkey domain skipped", ref(model.KindConfigKey, "example.com", model.Low, "README.md"), false, true, "", "", "", ""},
		{"anchor ok", ref(model.KindAnchor, "docs/guide.md#setup", model.High, "README.md"), true, false, "", "", "", "docs/guide.md"},
		{"anchor same doc", ref(model.KindAnchor, "#usage", model.High, "README.md"), true, false, "", "", "", "README.md"},
		{"anchor broken suggests", ref(model.KindAnchor, "docs/guide.md#instal", model.High, "README.md"), false, false, model.RuleBrokenAnchor, model.SevError, "#install", ""},
		{"anchor missing file skipped", ref(model.KindAnchor, "docs/nope.md#x", model.High, "README.md"), false, true, "", "", "", ""},
		{"anchor root-relative from a nested doc", rootAnchorRef("docs/README.md#nope", "README.md#nope"), false, false, model.RuleBrokenAnchor, model.SevError, "", ""},
		{"route ok with method", ref(model.KindRoute, "GET /v1/items", model.High, "README.md"), true, false, "", "", "", ""},
		{"route ok any method", ref(model.KindRoute, "/healthz", model.Medium, "README.md"), true, false, "", "", "", ""},
		{"route missing high is error", ref(model.KindRoute, "GET /v1/item", model.High, "README.md"), false, false, model.RuleMissingRoute, model.SevError, "/v1/items", ""},
		{"route missing medium is warning", ref(model.KindRoute, "/readyz", model.Medium, "README.md"), false, false, model.RuleMissingRoute, model.SevWarning, "", ""},
		{"route method mismatch", ref(model.KindRoute, "DELETE /v1/items", model.High, "README.md"), false, false, model.RuleMissingRoute, model.SevError, "", ""},
		{"route known as a literal", ref(model.KindRoute, "/openapi.json", model.Medium, "README.md"), true, false, "", "", "", ""},
		{"gosym dotted literal", ref(model.KindGoSymbol, "http.requests", model.Low, "README.md"), true, false, "", "", "", ""},
		{"flag known as a literal", ref(model.KindFlag, "dry-run", model.High, "README.md"), true, false, "", "", "", ""},
		{"env known as a literal", envRef("FIXTURE_HOME", "set the FIXTURE_HOME env var"), true, false, "", "", "", ""},
		{"configkey known as a literal", ref(model.KindConfigKey, "server.tls", model.Low, "README.md"), true, false, "", "", "", ""},
		{"json example key missing is warning", jsonRef("server.timeout", model.High), false, false, model.RuleUnknownConfigKey, model.SevWarning, "", ""},
		{"json example key medium is info", jsonRef("server.timeout", model.Medium), false, false, model.RuleUnknownConfigKey, model.SevInfo, "", ""},
		{"json example unknown top-level key still reported", jsonRef("retention", model.High), false, false, model.RuleUnknownConfigKey, model.SevWarning, "", ""},
		{"json example key present", jsonRef("server.addr", model.High), true, false, "", "", "", ""},
		{"target ok", ref(model.KindTarget, "make:build", model.High, "README.md"), true, false, "", "", "", ""},
		{"target typo", ref(model.KindTarget, "make:buidl", model.High, "README.md"), false, false, model.RuleMissingTarget, model.SevError, "make build", ""},
		{"target inline is warning", ref(model.KindTarget, "make:lint", model.Medium, "README.md"), false, false, model.RuleMissingTarget, model.SevWarning, "", ""},
		{"target unknown tool skipped", ref(model.KindTarget, "just:x", model.High, "README.md"), false, true, "", "", "", ""},
		{"install go ok", ref(model.KindInstall, "go:example.com/fixture/pkg/httpx", model.High, "README.md"), true, false, "", "", "", ""},
		{"install go module root ok", ref(model.KindInstall, "go:example.com/fixture", model.High, "README.md"), true, false, "", "", "", ""},
		{"install go missing dir", ref(model.KindInstall, "go:example.com/fixture/pkg/router", model.High, "README.md"), false, false, model.RuleInstallMismatch, model.SevError, "", ""},
		{"install go wrong module", ref(model.KindInstall, "go:github.com/acme/fixture", model.High, "README.md"), false, false, model.RuleInstallMismatch, model.SevError, "example.com/fixture", ""},
		{"install go dependency skipped", ref(model.KindInstall, "go:github.com/other/lib", model.High, "README.md"), false, true, "", "", "", ""},
		{"install pip ok normalised", ref(model.KindInstall, "pip:My_Tool", model.High, "README.md"), true, false, "", "", "", ""},
		{"install pip close", ref(model.KindInstall, "pip:my-tools", model.Medium, "README.md"), false, false, model.RuleInstallMismatch, model.SevWarning, "my-tool", ""},
		{"install pip other skipped", ref(model.KindInstall, "pip:requests", model.High, "README.md"), false, true, "", "", "", ""},
		{"install npm ok", ref(model.KindInstall, "npm:@acme/tool", model.High, "README.md"), true, false, "", "", "", ""},
		{"install npm unscoped same name", ref(model.KindInstall, "npm:tool", model.High, "README.md"), false, false, model.RuleInstallMismatch, model.SevError, "@acme/tool", ""},
		{"toolchain go equal", ref(model.KindToolchain, "go:1.22", model.Medium, "README.md"), true, false, "", "", "", ""},
		{"toolchain go too low", ref(model.KindToolchain, "go:1.21", model.Medium, "README.md"), false, false, model.RuleToolchain, model.SevWarning, "1.22", ""},
		{"toolchain go higher is info", ref(model.KindToolchain, "go:1.23", model.Medium, "README.md"), false, false, model.RuleToolchain, model.SevInfo, "1.22", ""},
		{"toolchain python too low", ref(model.KindToolchain, "python:3.9", model.Medium, "README.md"), false, false, model.RuleToolchain, model.SevWarning, "3.10", ""},
		{"toolchain unknown skipped", ref(model.KindToolchain, "rust:1.0", model.Medium, "README.md"), false, true, "", "", "", ""},
		{"default same", ref(model.KindDefault, "flag:addr|:8080", model.Medium, "README.md"), true, false, "", "", "", ""},
		{"default same quoted", ref(model.KindDefault, "key:log.level|\"info\"", model.High, "README.md"), true, false, "", "", "", ""},
		{"default duration same", ref(model.KindDefault, "flag:timeout|30000ms", model.Medium, "README.md"), true, false, "", "", "", ""},
		{"default differs", ref(model.KindDefault, "flag:addr|:9090", model.Medium, "README.md"), false, false, model.RuleDefaultMismatch, model.SevWarning, ":8080", ""},
		{"default unknown skipped", ref(model.KindDefault, "flag:nope|1", model.Medium, "README.md"), false, true, "", "", "", ""},
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
	res := r.Resolve(ref(model.KindPath, "nope/thing.go", model.High, "README.md"))
	if res.Finding == nil || res.Finding.Severity != model.SevInfo {
		t.Fatalf("override not applied: %+v", res.Finding)
	}
	if res := r.Resolve(ref(model.KindPath, "nope/thing.go", model.Low, "README.md")); !res.Skipped {
		t.Fatal("low confidence should be skipped with MinConfidence=medium")
	}
}

func TestExistsExactIsCaseSensitive(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Docs", "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]bool{
		"Docs/README.md": true, "docs/README.md": false, "Docs/readme.md": false, "Docs/Readme.md": false,
		"Docs": true, "docs": false, "Docs/": true, "nope/README.md": false, ".": true,
	} {
		if got := ExistsExact(root, rel); got != want {
			t.Errorf("ExistsExact(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestCaseMismatchMessage(t *testing.T) {
	ix := newFake()
	r := New(ix, Options{})
	res := r.Resolve(ref(model.KindPath, "readme.md", model.High, "docs/guide.md"))
	if res.Finding == nil {
		t.Fatal("expected a finding for readme.md")
	}
	if !strings.Contains(res.Finding.Message, "letter case") || res.Finding.Suggestion != "README.md" {
		t.Fatalf("message %q suggestion %q", res.Finding.Message, res.Finding.Suggestion)
	}
	if res.Finding.Data["fix"] != "README.md" {
		t.Errorf("root-frame fix = %v, want README.md", res.Finding.Data["fix"])
	}
	// written relative to the document's directory: the fix is too
	res = r.Resolve(ref(model.KindPath, "../readme.md", model.High, "docs/guide.md"))
	if res.Finding == nil || res.Finding.Data["fix"] != "../README.md" {
		t.Errorf("doc-frame fix = %+v", res.Finding)
	}
}

func TestRenameFix(t *testing.T) {
	ix := newFake()
	r := New(ix, Options{Renames: map[string]string{"old/name.go": "pkg/httpx/server.go", "old/dir": "pkg/httpx"}})
	cases := map[string]string{"old/name.go": "pkg/httpx/server.go", "./old/name.go": "./pkg/httpx/server.go", "old/dir/": "pkg/httpx/"}
	for text, want := range cases {
		res := r.Resolve(ref(model.KindPath, text, model.High, "README.md"))
		if res.Finding == nil || res.Finding.Data["fix"] != want {
			t.Errorf("%s: fix = %+v, want %q", text, res.Finding, want)
		}
	}
	// a fuzzy suggestion is never a fix
	res := r.Resolve(ref(model.KindPath, "docs/main.go", model.High, "README.md"))
	if res.Finding == nil || res.Finding.Suggestion == "" || res.Finding.Data["fix"] != nil {
		t.Errorf("fuzzy suggestion marked as a fix: %+v", res.Finding)
	}
}

func TestRelSlash(t *testing.T) {
	cases := []struct{ dir, target, want string }{
		{".", "README.md", "README.md"},
		{"docs", "README.md", "../README.md"},
		{"docs/howto", "README.md", "../../README.md"},
		{"docs", "docs/guide.md", "guide.md"},
		{"docs/howto", "docs/guide.md", "../guide.md"},
		{"docs", "pkg/x.go", "../pkg/x.go"},
	}
	for _, c := range cases {
		if got := relSlash(c.dir, c.target); got != c.want {
			t.Errorf("relSlash(%q, %q) = %q, want %q", c.dir, c.target, got, c.want)
		}
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

// rootAnchorRef builds the anchor reference the extractor produces for a
// nested document linking to a root-level file: Norm is joined with the
// document's directory, Text keeps the spelling from the document.
func rootAnchorRef(norm, text string) model.Reference {
	r := ref(model.KindAnchor, norm, model.High, "docs/guide.md")
	r.Text = text
	return r
}
