package extract

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"docrot/internal/markdown"
	"docrot/internal/model"
)

type fakeHints struct {
	module   string
	pkgs     map[string]bool
	types    map[string]bool
	top      []string
	odin     []string
	py       []string
	hasOdin  bool
	hasPy    bool
	langs    []model.Kind            // further present languages
	ns       map[model.Kind][]string // their namespaces
	proj     model.Project
	noModule bool
	routes   bool
	jsonKeys map[string]bool
	cfgKeys  map[string]bool
}

func (f fakeHints) ModulePath() string {
	if f.noModule {
		return ""
	}
	if f.module == "" {
		return "example.com/fixture"
	}
	return f.module
}
func (f fakeHints) IsGoPackage(n string) bool { return f.pkgs[n] }
func (f fakeHints) IsGoType(n string) bool    { return f.types[n] }
func (f fakeHints) TopLevelDirs() []string    { return f.top }
func (f fakeHints) Languages() []model.Kind {
	var out []model.Kind
	if f.hasOdin {
		out = append(out, model.KindOdinSym)
	}
	if f.hasPy {
		out = append(out, model.KindPySym)
	}
	return append(out, f.langs...)
}
func (f fakeHints) Namespaces(k model.Kind) []string {
	switch k {
	case model.KindOdinSym:
		return f.odin
	case model.KindPySym:
		return f.py
	}
	return f.ns[k]
}
func (f fakeHints) HasRoutes() bool            { return f.routes }
func (f fakeHints) Project() model.Project     { return f.proj }
func (f fakeHints) HasJSONKey(d string) bool   { return f.jsonKeys[d] }
func (f fakeHints) HasConfigKey(d string) bool { return f.cfgKeys[d] }

func goHints() fakeHints {
	return fakeHints{
		pkgs:  map[string]bool{"httpx": true, "cfg": true, "store": true, "main": true},
		types: map[string]bool{"Server": true, "Config": true},
		top:   []string{"cmd", "docs", "internal", "pkg", "scripts"},
	}
}

func run(t *testing.T, h Hints, md string) []model.Reference {
	t.Helper()
	doc := markdown.Parse("README.md", []byte(md))
	return Extract(doc, h, Options{})
}

func find(refs []model.Reference, kind model.Kind, norm string) *model.Reference {
	for i := range refs {
		if refs[i].Kind == kind && refs[i].Norm == norm {
			return &refs[i]
		}
	}
	return nil
}

func TestClassifyWhole(t *testing.T) {
	h := goHints()
	tests := []struct {
		in   string
		kind model.Kind
		norm string
		conf model.Confidence
	}{
		{"https://example.com/x", model.KindURL, "https://example.com/x", model.Low},
		{"docs/guide.md#setup", model.KindAnchor, "docs/guide.md#setup", model.High},
		{"--addr", model.KindFlag, "addr", model.High},
		{"--timeout_ms=5", model.KindFlag, "timeout-ms", model.High},
		{"-v", model.KindFlag, "v", model.Medium},
		{"FIXTURE_DEBUG", model.KindEnv, "FIXTURE_DEBUG", model.High},
		{"pkg/httpx/server.go", model.KindPath, "pkg/httpx/server.go", model.High},
		{"./scripts/verify.ps1", model.KindPath, "scripts/verify.ps1", model.High},
		{`docs\guide.md`, model.KindPath, "docs/guide.md", model.High},
		{"docs/", model.KindPath, "docs", model.High},
		{"docs", model.KindPath, "docs", model.High},
		{"internal/store", model.KindPath, "internal/store", model.High},
		{"main.go", model.KindPath, "main.go", model.Medium},
		{"a/b", "", "", 0},
		{"health/ready", "", "", 0},
		{"net/http", "", "", 0},
		{"internal/api.Server", model.KindGoSymbol, "api.Server", model.Low},
		{"httpx/server.go:90:", model.KindPath, "httpx/server.go", model.High},
		{"localhost:8080/healthz", "", "", 0},
		{"path/to/file.go", "", "", 0},
		{"/api/v1/users", "", "", 0},
		{"C:/Users/x", "", "", 0},
		{"httpx.NewServer(addr)", model.KindGoSymbol, "httpx.NewServer", model.High},
		{"httpx.Server.Addr", model.KindGoSymbol, "httpx.Server.Addr", model.High},
		{"*httpx.Server", model.KindGoSymbol, "httpx.Server", model.High},
		{"Server.Addr()", model.KindGoSymbol, "Server.Addr", model.High},
		{"NewServer()", model.KindGoSymbol, "NewServer", model.Medium},
		{"http.Handler", "", "", 0},
		{"context.Context", "", "", 0},
		{"app.Run(ctx)", model.KindGoSymbol, "app.Run", model.Low},
		{"server.addr", model.KindConfigKey, "server.addr", model.Low},
		{"jsonx.ReadJSONL[T](path, fn)", "", "", 0}, // jsonx unknown here → Low gosym
		{"example.com/fixture/pkg/httpx", model.KindImport, "example.com/fixture/pkg/httpx", model.High},
		{"example.com/fixture/pkg/httpx.WriteData", model.KindGoSymbol, "httpx.WriteData", model.High},
		{"<path>", "", "", 0},
		{"Node.js", "", "", 0},
		{"e.g.", "", "", 0},
		{"README", "", "", 0},
		{"go.mod", model.KindPath, "go.mod", model.Medium},
		{".gitignore", model.KindPath, ".gitignore", model.Medium},
		{"cmd/app/...", model.KindPath, "cmd/app", model.High},
	}
	x := &extractor{doc: markdown.Parse("README.md", nil), hints: h, seen: map[string]bool{}}
	for _, tc := range tests {
		r := x.classifyWhole(tc.in)
		if tc.kind == "" {
			if r != nil && !(tc.in == "jsonx.ReadJSONL[T](path, fn)" && r.Confidence == model.Low) {
				t.Errorf("%q: expected no reference, got %+v", tc.in, r)
			}
			continue
		}
		if r == nil {
			t.Errorf("%q: expected %s %q, got nil", tc.in, tc.kind, tc.norm)
			continue
		}
		if r.Kind != tc.kind || r.Norm != tc.norm || r.Confidence != tc.conf {
			t.Errorf("%q: got kind=%s norm=%q conf=%s, want %s %q %s", tc.in, r.Kind, r.Norm, r.Confidence, tc.kind, tc.norm, tc.conf)
		}
	}
}

func TestClassifyTokens(t *testing.T) {
	h := goHints()
	x := &extractor{doc: markdown.Parse("README.md", nil), hints: h, seen: map[string]bool{}}
	refs := x.classifyTokens("cfg.LoadJSON(path, &c); cfg.Validate(&c) --addr :9090 FIXTURE_DEBUG=1 ./scripts/x.ps1 3.14")
	want := map[string]bool{
		"gosym|cfg.LoadJSON": true, "gosym|cfg.Validate": true, "flag|addr": true,
		"env|FIXTURE_DEBUG": true, "path|scripts/x.ps1": true,
	}
	got := map[string]bool{}
	for _, r := range refs {
		got[string(r.Kind)+"|"+r.Norm] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("missing %s in %v", k, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected extra refs: %v", got)
	}
}

func TestDocumentExtraction(t *testing.T) {
	md := `# Fixture

## Usage

Run ` + "`go run ./cmd/app --addr :9090`" + `. See ` + "`pkg/httpx/router.go`" + ` and [guide](docs/guide.md#setup)
and [missing](docs/guid.md) and ![logo](assets/logo.png) and [ext](https://example.com).

` + "```sh" + `
$ ./scripts/verify.ps1
go run ./cmd/server -v
python tools/helper.py --fast
odin build gbench -out:gbench.exe
go build -o dist/app ./cmd/app
` + "```" + `

` + "```go" + `
import (
	"fmt"
	"example.com/fixture/pkg/httpx"
	"example.com/fixture/pkg/router"
)
s := httpx.NewServer(":8080") // httpx.Old
` + "```" + `

<!-- docrot:ignore -->
Ignored ` + "`docs/nonexistent.md`" + `.

Prose path docs/prose.md here and ` + "`Server.Address()`" + `.
`
	refs := run(t, goHints(), md)
	checks := []struct {
		kind model.Kind
		norm string
		line int
	}{
		{model.KindPath, "cmd/app", 5},
		{model.KindFlag, "addr", 5},
		{model.KindPath, "pkg/httpx/router.go", 5},
		{model.KindPath, "docs/guide.md", 5},
		{model.KindAnchor, "docs/guide.md#setup", 5},
		{model.KindPath, "docs/guid.md", 6},
		{model.KindPath, "assets/logo.png", 6},
		{model.KindURL, "https://example.com", 6},
		{model.KindCommand, "scripts/verify.ps1", 9},
		{model.KindCommand, "cmd/server", 10},
		{model.KindCommand, "tools/helper.py", 11},
		{model.KindCommand, "gbench", 12},
		{model.KindCommand, "cmd/app", 13},
		{model.KindImport, "example.com/fixture/pkg/httpx", 16},
		{model.KindImport, "example.com/fixture/pkg/router", 16},
		{model.KindGoSymbol, "httpx.NewServer", 16},
		{model.KindPath, "docs/prose.md", 28},
		{model.KindGoSymbol, "Server.Address", 28},
	}
	for _, c := range checks {
		r := find(refs, c.kind, c.norm)
		if r == nil {
			t.Errorf("missing %s %q", c.kind, c.norm)
			continue
		}
		if r.Loc.Line != c.line {
			t.Errorf("%s %q: line %d, want %d", c.kind, c.norm, r.Loc.Line, c.line)
		}
		if r.Section != "Usage" {
			t.Errorf("%s %q: section %q, want Usage", c.kind, c.norm, r.Section)
		}
	}
	if find(refs, model.KindPath, "docs/nonexistent.md") != nil {
		t.Error("ignored line leaked")
	}
	if find(refs, model.KindPath, "dist/app") != nil || find(refs, model.KindCommand, "dist/app") != nil {
		t.Error("output sink -o dist/app must not be a reference")
	}
	if find(refs, model.KindGoSymbol, "httpx.Old") != nil {
		t.Error("comment in go block leaked")
	}
	if find(refs, model.KindPath, "fmt") != nil || find(refs, model.KindImport, "fmt") != nil {
		t.Error("stdlib import leaked")
	}
}

func TestIgnoreFileAndRegex(t *testing.T) {
	doc := markdown.Parse("x.md", []byte("<!-- docrot:ignore-file -->\n`docs/x.md`\n"))
	if got := Extract(doc, goHints(), Options{}); len(got) != 0 {
		t.Fatalf("ignore-file: got %d refs", len(got))
	}
	doc = markdown.Parse("x.md", []byte("`docs/x.md` `docs/y.md`\n"))
	got := Extract(doc, goHints(), Options{Ignore: []*regexp.Regexp{regexp.MustCompile(`x\.md$`)}})
	if len(got) != 1 || got[0].Norm != "docs/y.md" {
		t.Fatalf("ignore regex: got %+v", got)
	}
}

func TestOdinAndPython(t *testing.T) {
	h := fakeHints{noModule: true, hasOdin: true, odin: []string{"fixture_odin"}, hasPy: true, py: []string{"tools.helper", "helper"}}
	refs := run(t, h, "`fixture_odin.render_frame` `render_frames()` `helper.summarize` `Runner.run_async` `server.addr`\n")
	if r := find(refs, model.KindOdinSym, "fixture_odin.render_frame"); r == nil || r.Confidence != model.High {
		t.Errorf("odin pkg symbol: %+v", r)
	}
	if r := find(refs, model.KindOdinSym, "render_frames"); r == nil || r.Confidence != model.Medium {
		t.Errorf("odin snake call: %+v", r)
	}
	if r := find(refs, model.KindPySym, "helper.summarize"); r == nil || r.Confidence != model.High {
		t.Errorf("py module symbol: %+v", r)
	}
	if r := find(refs, model.KindPySym, "Runner.run_async"); r == nil {
		t.Errorf("py Class.method: %+v", r)
	}
	if r := find(refs, model.KindOdinSym, "server.addr"); r == nil || r.Confidence != model.Low {
		t.Errorf("lowercase dotted in odin repo should be Low odinsym: %+v", refs)
	}
}

func TestShellPrompts(t *testing.T) {
	x := &extractor{doc: markdown.Parse("README.md", nil), hints: goHints(), seen: map[string]bool{}}
	for _, line := range []string{
		"$ ./scripts/a.ps1",
		"PS C:\\repo> .\\scripts\\a.ps1",
		"user@host:~/repo$ ./scripts/a.ps1 --flag",
		"./scripts/a.ps1 && go test ./...",
	} {
		refs := x.shellRefs(line, false)
		if len(refs) == 0 || refs[0].Norm != "scripts/a.ps1" || refs[0].Kind != model.KindCommand {
			t.Errorf("%q: got %+v", line, refs)
		}
	}
	if refs := x.shellRefs("# ./scripts/comment.ps1", false); len(refs) != 0 {
		t.Errorf("comment line: %+v", refs)
	}
	if refs := x.shellRefs("go test ./...", false); len(refs) != 0 {
		t.Errorf("./... must not be a reference: %+v", refs)
	}
}

func TestContextTruncation(t *testing.T) {
	long := strings.Repeat("x", 300)
	refs := run(t, goHints(), "`docs/a.md` "+long+"\n")
	if len(refs) != 1 || len(refs[0].Context) > 160 {
		t.Fatalf("context not truncated: %d", len(refs[0].Context))
	}
}

// A long CJK line must not be cut in the middle of a rune when the context
// is truncated, or the JSON/SARIF/HTML reports carry U+FFFD.
func TestContextTruncationKeepsValidUTF8(t *testing.T) {
	line := strings.Repeat("很長的中文說明文字 ", 20) + "`internal/gone.go`"
	d := markdown.Parse("README.md", []byte("# 標題\n\n"+line+"\n"))
	refs := Extract(d, fakeHints{}, Options{})
	if len(refs) == 0 {
		t.Fatal("no references extracted")
	}
	for _, r := range refs {
		if !utf8.ValidString(r.Context) {
			t.Errorf("context is not valid UTF-8: %q", r.Context)
		}
		if strings.ContainsRune(r.Context, utf8.RuneError) {
			t.Errorf("context contains U+FFFD: %q", r.Context)
		}
	}
}

func TestRustSymbols(t *testing.T) {
	h := fakeHints{noModule: true, langs: []model.Kind{model.KindRustSym}, ns: map[model.Kind][]string{model.KindRustSym: {"mycrate", "mycrate::io"}}}
	src := strings.Join([]string{
		"`mycrate::io::read_all` `crate::Config::new()` `Config::new()` `std::env::var` `ServiceBuilder::layer` `Poll::Ready` `shout_it!()`",
		"",
		"```rust",
		"use tower::{ServiceBuilder, Layer};",
		"use std::task::Poll;",
		"use crate::Config;",
		"```",
		"",
		"[the extractors](crate::extract) and [`Path`](crate::extract::Path) and [x](Router::fallback)",
		"",
	}, "\n")
	refs := run(t, h, src)
	if r := find(refs, model.KindRustSym, "mycrate::io::read_all"); r == nil || r.Confidence != model.High {
		t.Errorf("crate path: %+v", r)
	}
	if r := find(refs, model.KindRustSym, "crate::Config::new"); r == nil || r.Confidence != model.High {
		t.Errorf("crate:: path is High: %+v", r)
	}
	if r := find(refs, model.KindRustSym, "Config::new"); r == nil || r.Confidence != model.Medium {
		t.Errorf("Type::method is Medium: %+v", r)
	}
	for _, norm := range []string{"std::env::var", "ServiceBuilder::layer", "Poll::Ready"} {
		if r := find(refs, model.KindRustSym, norm); r != nil {
			t.Errorf("%s should be skipped (std or imported from another crate): %+v", norm, r)
		}
	}
	if r := find(refs, model.KindRustSym, "shout_it"); r == nil || r.Confidence != model.Medium {
		t.Errorf("macro call: %+v", refs)
	}
	for _, norm := range []string{"crate::extract", "crate::extract::Path", "Router::fallback"} {
		if r := find(refs, model.KindRustSym, norm); r == nil {
			t.Errorf("intra-doc link %s should be a Rust symbol: %+v", norm, refs)
		}
	}
	for _, r := range refs {
		if r.Kind == model.KindPath && strings.Contains(r.Norm, "::") {
			t.Errorf("a :: link target must not be a path claim: %+v", r)
		}
	}
}
