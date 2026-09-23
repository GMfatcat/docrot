package rust

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"docrot/internal/index/routes"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const libRS = `//! Crate docs.
use std::fmt;

pub mod io;
mod private;

/// Config holds settings.
#[derive(Debug, Clone)]
pub struct Config {
    pub port: u16,
    name: String,
}

impl Config {
    /// new builds a Config.
    pub fn new(port: u16) -> Self {
        fn helper() {} // a local: not indexed
        Config { port, name: "x".into() }
    }

    fn secret(&self) {}
}

impl fmt::Display for Config {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.port)
    }
}

pub trait Render {
    /// render draws.
    fn render(&self, frame: &mut [u8]) -> usize;
    fn name(&self) -> &str { "render" }
}

pub enum Mode { Fast, Slow }
pub type Result<T> = std::result::Result<T, Error>;
pub const MAX_ITEMS: usize = 10;
pub static mut COUNTER: u32 = 0;
pub(crate) fn internal_helper() {}
fn hidden() {}

#[macro_export]
macro_rules! shout {
    ($e:expr) => { println!("{}", $e) };
}

pub mod nested {
    pub fn deep() {}
}

pub use io::{read_all, write_all as write_everything};
pub use private::Hidden;
pub use self::net::{
    connect as net_connect,
    tcp,
};
pub use other_crate::*;
`

const ioRS = `//! io helpers
use std::env;

/// read_all reads a file.
pub fn read_all(path: &str) -> Vec<u8> {
    let home = env::var("APP_HOME").unwrap_or("/tmp".to_string());
    let _ = home;
    Vec::new()
}

/// write_all writes a file.
pub fn write_all(path: &str, data: &[u8]) {}
`

func buildTest(t *testing.T) *Index {
	t.Helper()
	root := writeTree(t, map[string]string{
		"Cargo.toml":               "[package]\nname = \"my-crate\"\nversion = \"0.1.0\"\nrust-version = \"1.70\"\n",
		"src/lib.rs":               libRS,
		"src/io.rs":                ioRS,
		"src/private.rs":           "pub struct Hidden;\n",
		"src/net/mod.rs":           "pub fn connect() {}\n",
		"src/net/tcp.rs":           "pub fn dial() {}\n",
		"crates/other/Cargo.toml":  "[package]\nname = \"other-crate\"\n",
		"crates/other/src/lib.rs":  "pub mod deep;\npub fn from_other() {}\npub enum Mode { Fast, Slow(u8), Custom { n: u8 } }\n",
		"crates/other/src/deep.rs": "pub fn deeper() {}\n",
		"src/bin/tool.rs":          "fn main() {}\n",
		"examples/demo.rs":         "pub fn demo_run() {}\nfn main() {}\n",
		"target/debug/x.rs":        "pub fn built() {}\n",
	})
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestBuildAndLookup(t *testing.T) {
	ix := buildTest(t)
	st := ix.Stats()
	if st.Files != 9 || st.Crates != 2 {
		t.Errorf("stats = %+v", st)
	}
	if got := ix.Crates(); !reflect.DeepEqual(got, []string{"my_crate", "other_crate"}) {
		t.Errorf("Crates() = %v", got)
	}
	for _, q := range []string{
		"my_crate::Config", "Config", "Config::new", "Config::new()", "my_crate::Config::new",
		"crate::Config::new", "Config::secret", "Config::fmt",
		"Render", "Render::render", "Render::name",
		"Mode", "Result", "MAX_ITEMS", "COUNTER", "internal_helper", "hidden", "shout", "shout!()",
		"nested::deep", "my_crate::nested::deep",
		"my_crate::io::read_all", "io::read_all", "read_all", "write_everything", "my_crate::write_everything",
		"my_crate::Hidden", "private::Hidden",
		"net::connect", "net::tcp::dial", "my_crate::net::tcp::dial", "tcp::dial",
		"tool::main", "demo::demo_run", "demo_run",
		"my_crate::net_connect", "my_crate::tcp::dial",
		// through `pub use other_crate::*`
		"my_crate::from_other", "my_crate::deep::deeper", "my_crate::Mode::Fast", "crate::Mode::Slow", "Mode::Custom",
		"other_crate::Mode::Fast",
		// a module is a valid name
		"my_crate::io", "io", "nested",
	} {
		if !ix.Has(q) {
			t.Errorf("Has(%q) = false", q)
		}
	}
	for _, q := range []string{"helper", "Config::helper", "built", "my_crate::io::nope", "other::read_all", "Config::render"} {
		if ix.Has(q) {
			t.Errorf("Has(%q) = true", q)
		}
	}
	if f, line, ok := ix.File("io::write_all"); !ok || f != "src/io.rs" || line != 12 {
		t.Errorf("File(io::write_all) = %q, %d, %v", f, line, ok)
	}
}

func TestNamespaces(t *testing.T) {
	ix := buildTest(t)
	want := []string{"my_crate", "my_crate::demo", "my_crate::io", "my_crate::nested", "my_crate::net", "my_crate::net::tcp", "my_crate::private", "my_crate::tool", "other_crate", "other_crate::deep"}
	if got := ix.Namespaces(); !reflect.DeepEqual(got, want) {
		t.Errorf("Namespaces() = %v, want %v", got, want)
	}
	for _, q := range []string{"my_crate", "crate", "crate::io", "io", "net::tcp", "tcp", "my_crate::nested", "my_crate::deep", "deep"} {
		if !ix.IsNamespace(q) {
			t.Errorf("IsNamespace(%q) = false", q)
		}
	}
	for _, q := range []string{"Config", "io::read_all", "std"} {
		if ix.IsNamespace(q) {
			t.Errorf("IsNamespace(%q) = true", q)
		}
	}
	if !ix.IsExample("my_crate::demo") || !ix.IsExample("demo") || ix.IsExample("my_crate::io") {
		t.Error("IsExample: examples/demo.rs should be an example module, src/io.rs not")
	}
}

func TestSpans(t *testing.T) {
	ix := buildTest(t)
	sp, ok := ix.Span("Config::new")
	if !ok {
		t.Fatal("Span(Config::new) not found")
	}
	if sp.Qualified != "my_crate::Config::new" || sp.File != "src/lib.rs" || sp.DeclLine != 16 || sp.BodyEnd != 19 || sp.DocStart != 15 || sp.DocEnd != 15 {
		t.Errorf("span = %+v", sp)
	}
	if !reflect.DeepEqual(sp.Doc, []string{"new builds a Config."}) || !reflect.DeepEqual(sp.Params, []string{"port"}) || !sp.Exported {
		t.Errorf("span doc/params/exported = %v %v %v", sp.Doc, sp.Params, sp.Exported)
	}
	sp, _ = ix.Span("Render::render")
	if !reflect.DeepEqual(sp.Params, []string{"self", "frame"}) || sp.BodyEnd != 32 {
		t.Errorf("trait method span = %+v", sp)
	}
	sp, _ = ix.Span("Config")
	if sp.DocStart != 7 || sp.BodyEnd != 12 {
		t.Errorf("struct span = %+v (attribute line must not break the doc comment)", sp)
	}
	sp, _ = ix.Span("Config::fmt")
	if !sp.Exported {
		t.Error("a trait impl method is exported")
	}
	if sp, _ := ix.Span("Config::secret"); sp.Exported {
		t.Error("a private inherent method is not exported")
	}
	var names []string
	for _, s := range ix.AllSpans() {
		names = append(names, s.Qualified)
	}
	want := []string{"my_crate::COUNTER", "my_crate::Config", "my_crate::Config::fmt", "my_crate::Config::new", "my_crate::MAX_ITEMS", "my_crate::Mode", "my_crate::Render", "my_crate::Render::name", "my_crate::Render::render", "my_crate::Result", "my_crate::io::read_all", "my_crate::io::write_all", "my_crate::nested::deep", "my_crate::net::connect", "my_crate::net::tcp::dial", "my_crate::private::Hidden", "my_crate::shout", "other_crate::Mode", "other_crate::deep::deeper", "other_crate::from_other"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("AllSpans() = %v\nwant %v", names, want)
	}
}

func TestSimilarAndSymbols(t *testing.T) {
	ix := buildTest(t)
	if got := ix.Similar("io::write_al", 3); len(got) == 0 || got[0] != "my_crate::io::write_all" {
		t.Errorf("Similar = %v", got)
	}
	if got := ix.Similar("Confg::new", 2); len(got) == 0 || got[0] != "my_crate::Config::new" {
		t.Errorf("Similar(Confg::new) = %v", got)
	}
	syms := ix.Symbols()
	if len(syms) == 0 || syms[0] != "my_crate::COUNTER" || !contains(syms, "other_crate::Mode::Slow") {
		t.Errorf("Symbols() = %v", syms)
	}
	if !ix.Literals().Has("APP_HOME") || !ix.Literals().Has("render") {
		t.Error("string literals not indexed")
	}
	if got := ix.Envs(); !reflect.DeepEqual(got, []string{"APP_HOME"}) {
		t.Errorf("Envs() = %v", got)
	}
	if v, ok := ix.Defaults().Get("env", "APP_HOME"); !ok || v != "/tmp" {
		t.Errorf("env default = %q, %v", v, ok)
	}
}

func TestModuleOf(t *testing.T) {
	cases := []struct {
		in, want string
		example  bool
	}{
		{"src/lib.rs", "c", false},
		{"src/main.rs", "c", false},
		{"src/io.rs", "c::io", false},
		{"src/io/mod.rs", "c::io", false},
		{"src/net/tcp.rs", "c::net::tcp", false},
		{"src/bin/tool.rs", "c::tool", false},
		{"tests/it.rs", "c::it", true},
		{"examples/demo.rs", "c::demo", true},
		{"benches/b.rs", "c::b", true},
		{"build.rs", "c::build", false},
	}
	for _, c := range cases {
		got, ex := moduleOf("c", c.in)
		if got != c.want || ex != c.example {
			t.Errorf("moduleOf(%q) = %q, %v; want %q, %v", c.in, got, ex, c.want, c.example)
		}
	}
}

func TestWorkspaceCrates(t *testing.T) {
	root := writeTree(t, map[string]string{
		"Cargo.toml":               "[workspace]\nmembers = [\"crates/*\"]\n",
		"crates/alpha/Cargo.toml":  "[package]\nname = \"alpha\"\n",
		"crates/alpha/src/lib.rs":  "pub fn a() {}\n",
		"crates/beta/Cargo.toml":   "[package]\nname = \"beta-lib\"\n",
		"crates/beta/src/lib.rs":   "pub fn b() {}\n",
		"crates/beta/src/inner.rs": "pub fn c() {}\n",
	})
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ix.Crates(); !reflect.DeepEqual(got, []string{"alpha", "beta_lib"}) {
		t.Errorf("Crates() = %v", got)
	}
	for _, q := range []string{"alpha::a", "beta_lib::b", "beta_lib::inner::c", "inner::c"} {
		if !ix.Has(q) {
			t.Errorf("Has(%q) = false", q)
		}
	}
	if ix.Has("alpha::b") {
		t.Error("alpha::b should not resolve: b lives in beta_lib")
	}
}

func TestMaskLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{`let s = "a { b } c"; // { comment`, `let s = "         ";             `},
		{`let c = '{'; let l: &'a str = x;`, `let c = ' '; let l: &'a str = x;`},
		{`let r = r#"x"y"#; fn f() {`, `let r = r#"   "#; fn f() {`},
		{`let e = "esc \" q"; {`, `let e = "        "; {`},
	}
	for _, c := range cases {
		if got, _ := maskLine(c.in, 0); got != c.want {
			t.Errorf("maskLine(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	masked := maskAll([]string{"/* a {", "b } */ fn x() {", "}"})
	if masked[0] != "      " || masked[1] != "       fn x() {" {
		t.Errorf("block comment: %q", masked)
	}
}

func TestStripLifetimes(t *testing.T) {
	if got := stripLifetimes(`fn f<'a>(x: &'a str) -> &'a str { let c = 'x'; "lit" }`); got != `fn f<  >(x: &   str) -> &   str { let c = 'x'; "lit" }` {
		t.Errorf("stripLifetimes = %q", got)
	}
}

func TestRoutes(t *testing.T) {
	lines := []string{
		`let app = Router::new()`,
		`    .route("/items", get(list).post(create))`,
		`    .route("/items/:id",`,
		`        get(one).delete(remove),`,
		`    )`,
		`    .route("/ws", any(ws))`,
		`    .route_service("/assets", svc)`,
		`    .nest("/api", api_router());`,
		`#[get("/users/<id>")]`,
		`fn user(id: u32) {}`,
		`#[post("/upload", data = "<form>")]`,
		`fn upload() {}`,
		`#[route("/legacy", method = "PUT")]`,
		`fn legacy() {}`,
		`web::scope("/admin").service(web::resource("/stats").route(web::get().to(stats)));`,
		`rocket::build().mount("/v1", routes![user])`,
		`app.at("/tide").get(t);`,
		`let x = client.get("https://x/y");`,
	}
	got := parseRoutes(lines, "src/main.rs")
	want := []routes.Route{
		{Method: "GET", Path: "/items", File: "src/main.rs", Line: 2},
		{Method: "POST", Path: "/items", File: "src/main.rs", Line: 2},
		{Method: "GET", Path: "/items/:id", File: "src/main.rs", Line: 3},
		{Method: "DELETE", Path: "/items/:id", File: "src/main.rs", Line: 3},
		{Method: "", Path: "/ws", File: "src/main.rs", Line: 6},
		{Method: "", Path: "/assets", File: "src/main.rs", Line: 7},
		{Method: "", Path: "/api", File: "src/main.rs", Line: 8, Prefix: true},
		{Method: "GET", Path: "/users/<id>", File: "src/main.rs", Line: 9},
		{Method: "POST", Path: "/upload", File: "src/main.rs", Line: 11},
		{Method: "PUT", Path: "/legacy", File: "src/main.rs", Line: 13},
		{Method: "", Path: "/admin", File: "src/main.rs", Line: 15, Prefix: true},
		{Method: "", Path: "/stats", File: "src/main.rs", Line: 15},
		{Method: "", Path: "/v1", File: "src/main.rs", Line: 16, Prefix: true},
		{Method: "", Path: "/tide", File: "src/main.rs", Line: 17},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseRoutes:\n got %+v\nwant %+v", got, want)
	}
}

func TestDefaults(t *testing.T) {
	lines := []string{
		`#[derive(Parser)]`,
		`struct Args {`,
		`    /// Port to listen on`,
		`    #[arg(long, short, env = "APP_PORT", default_value = "8080")]`,
		`    port: u16,`,
		`    #[arg(long = "log-level", default_value_t = 3)]`,
		`    verbosity: u8,`,
		`    #[clap(short)]`,
		`    quiet: bool,`,
		`    #[arg(`,
		`        long,`,
		`        env,`,
		`    )]`,
		`    pub data_dir: String,`,
		`}`,
		`let a = Arg::new("workers").long("workers").env("WORKERS").default_value("4");`,
		`let home = std::env::var("HOME").unwrap_or_else(|_| "/root".into());`,
		`let n = env::var("N").unwrap_or(2);`,
		`let k = option_env!("BUILD_KEY");`,
	}
	defs, flags, envs := parseDefaults(lines)
	if want := []string{"port", "log-level", "data-dir", "workers"}; !reflect.DeepEqual(flags, want) {
		t.Errorf("flags = %v, want %v", flags, want)
	}
	if want := []string{"APP_PORT", "DATA_DIR", "WORKERS", "HOME", "N", "BUILD_KEY"}; !reflect.DeepEqual(envs, want) {
		t.Errorf("envs = %v, want %v", envs, want)
	}
	want := [][3]string{{"flag", "port", "8080"}, {"env", "APP_PORT", "8080"}, {"flag", "log-level", "3"}, {"flag", "workers", "4"}, {"env", "HOME", "/root"}, {"env", "N", "2"}}
	if !reflect.DeepEqual(defs, want) {
		t.Errorf("defs = %v\nwant %v", defs, want)
	}
}

func TestUseNames(t *testing.T) {
	cases := []struct {
		in    string
		want  []useItem
		globs []string
	}{
		{"pub use io::{read_all, write_all as write_everything};", []useItem{{"read_all", "io::read_all"}, {"write_everything", "io::write_all"}}, nil},
		{"pub use private::Hidden;", []useItem{{"Hidden", "private::Hidden"}}, nil},
		{"pub use prelude::*;", nil, []string{"prelude"}},
		{"pub use clap_builder::*; // everything", nil, []string{"clap_builder"}},
		{"pub(crate) use a::{self, B, c::*};", []useItem{{"B", "a::B"}}, []string{"a::c"}},
		{"pub use self::from_fn::{ from_fn, FromFn, };", []useItem{{"from_fn", "self::from_fn::from_fn"}, {"FromFn", "self::from_fn::FromFn"}}, nil},
		{"pub use self::{ nested_path::NestedPath, path::{Path, RawPathParams}, state::State, };", []useItem{{"NestedPath", "self::nested_path::NestedPath"}, {"Path", "self::path::Path"}, {"RawPathParams", "self::path::RawPathParams"}, {"State", "self::state::State"}}, nil},
	}
	for _, c := range cases {
		got, globs := useNames(c.in)
		if !reflect.DeepEqual(got, c.want) || !reflect.DeepEqual(globs, c.globs) {
			t.Errorf("useNames(%q) = %v, %v; want %v, %v", c.in, got, globs, c.want, c.globs)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestImplHeader(t *testing.T) {
	cases := []struct {
		in       string
		typ      string
		hasTrait bool
		ok       bool
	}{
		{"impl Config {", "Config", false, true},
		{"impl<T> Iterator for Wrapper<T> {", "Wrapper", true, true},
		{"impl fmt::Display for Config {", "Config", true, true},
		{"impl<'s, F: Fn() -> clap::Command> CompleteEnv<'s, F> {", "CompleteEnv", false, true},
		{"impl<P, T> TypedPath for WithQueryParams<P, T>", "WithQueryParams", true, true},
		{"unsafe impl Send for Handle {}", "Handle", true, true},
		{"impl<T: Clone> From<Vec<T>> for List<T> {", "List", true, true},
		{"impl<'a> crate::io::Read for &'a mut Cursor {", "Cursor", true, true},
		{"impl_second_element_is!(T1);", "", false, false},
		{"implement(x);", "", false, false},
	}
	for _, c := range cases {
		typ, hasTrait, ok := implHeader(c.in)
		if typ != c.typ || hasTrait != c.hasTrait || ok != c.ok {
			t.Errorf("implHeader(%q) = %q, %v, %v; want %q, %v, %v", c.in, typ, hasTrait, ok, c.typ, c.hasTrait, c.ok)
		}
	}
}
