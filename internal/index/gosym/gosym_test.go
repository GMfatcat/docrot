package gosym

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fixture is a tiny multi-package Go module written into a temporary
// directory. Struct tags are spelled with "~" instead of a back quote so the
// sources can live in raw string literals; writeFixture swaps them back.
var fixture = map[string]string{
	"go.mod": `module example.com/demo

go 1.26
`,

	"main.go": `package main

import "fmt"

// Version is the build version.
const Version = "1.0"

func main() { fmt.Println(helper()) }

func helper() string { return "hi" }
`,

	"httpx/httpx.go": `package httpx

import "errors"

// ErrClosed is returned once the client is closed.
var ErrClosed = errors.New("closed")

// MaxRetries caps the number of attempts.
const MaxRetries = 3

// Logger prefixes log lines.
type Logger struct {
	Prefix string
}

// Client talks to a server.
type Client struct {
	Timeout int
	baseURL string
	Logger
}

// Do performs a request.
func (c *Client) Do(path string) error { return nil }

// Close releases the client.
func (c Client) Close() error { return nil }

func (c *Client) reset() {}

// Handler serves requests.
type Handler interface {
	Serve(path string) error
	Name() string
}

// WriteData writes a payload.
func WriteData(v any) error { return nil }

func writeRaw(v any) error { return nil }
`,

	"httpx/httpx_test.go": `package httpx

import "testing"

func helperOnlyInTest() string { return "" }

func TestWriteData(t *testing.T) { _ = WriteData(nil) }
`,

	"internal/store/store.go": `package store

// DefaultName is the name used when none is given.
const DefaultName = "store"

// Store keeps records.
type Store struct {
	Name string
}

// Get fetches one record.
func (s *Store) Get(key string) (string, bool) { return "", false }

// WriteData deliberately shares its name with httpx.WriteData.
func WriteData(v any) error { return nil }
`,

	"internal/store/config.go": `package store

// Config is the on-disk configuration.
type Config struct {
	Server  ServerConfig      ~json:"server" yaml:"server"~
	Peers   []PeerConfig      ~json:"peers"~
	Labels  map[string]string ~json:"labels"~
	Debug   bool
	Ignored string ~json:"-"~
	Meta
}

// ServerConfig configures the listener.
type ServerConfig struct {
	Addr string     ~json:"addr" yaml:"address" toml:"addr"~
	Port int        ~json:"port,omitempty"~
	TLS  *TLSConfig ~json:"tls"~
}

// TLSConfig configures transport security and is recursive on purpose.
type TLSConfig struct {
	CertFile string     ~json:"cert_file"~
	Next     *TLSConfig ~json:"next"~
}

// PeerConfig describes one peer.
type PeerConfig struct {
	Host string ~json:"host"~
}

// Meta is embedded without a tag, so its keys are inlined.
type Meta struct {
	Version string ~json:"version"~
}

// L1 starts a chain deeper than the key depth cap.
type L1 struct {
	N *L2 ~json:"l2"~
}

type L2 struct {
	N *L3 ~json:"l3"~
}

type L3 struct {
	N *L4 ~json:"l4"~
}

type L4 struct {
	N *L5 ~json:"l5"~
}

type L5 struct {
	N *L6 ~json:"l6"~
}

type L6 struct {
	N *L7 ~json:"l7"~
}

type L7 struct {
	Leaf string ~json:"leaf"~
}
`,

	"generic/stack.go": `package generic

// Stack is a last-in first-out stack.
type Stack[T any] struct {
	items []T
}

// Push adds one item.
func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }

// Pop removes the newest item.
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	return zero, false
}

// Pair holds two values.
type Pair[K comparable, V any] struct {
	Key K
	Val V
}

// First returns the key.
func (p Pair[K, V]) First() K { return p.Key }
`,

	"cli/flags.go": `package cli

import (
	"flag"
	"time"
)

var configPath string

type level struct{}

func (l *level) String() string     { return "" }
func (l *level) Set(s string) error { return nil }

func newLevel() *level { return &level{} }

// Setup defines the command line interface.
func Setup() {
	flag.StringVar(&configPath, "config", "config.json", "path to the config file")
	verbose := flag.Bool("verbose", false, "verbose output")
	_ = verbose
	flag.DurationVar(new(time.Duration), "timeout", time.Second, "request timeout")
	flag.Func("mode", "operating mode", func(string) error { return nil })
	flag.Var(newLevel(), "log-level", "log level")

	fs := flag.NewFlagSet("build", flag.ExitOnError)
	fs.String("out", "dist", "output directory")
	fs.BoolVar(new(bool), "dry-run", false, "do nothing")

	// Decoys: a Stringer call has too few arguments to be a flag.
	_ = time.Second.String()
	flag.String(configPath, "", "not a literal")
}
`,

	"cli/env.go": `package cli

import (
	"os"
	"syscall"
)

type envGetter struct{}

func (envGetter) Get(name string) string { return os.Getenv(name) }

var envx envGetter

func getenvDefault(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func plain(s string) string { return s }

// Load reads configuration from the environment.
func Load() {
	_ = os.Getenv("HOME_DIR")
	if v, ok := os.LookupEnv("DOCROT_DEBUG"); ok {
		_ = v
	}
	_ = os.Setenv("SET_ME", "1")
	_ = os.Unsetenv("DROP_ME")
	_, _ = syscall.Getenv("SYS_VAR")
	_ = os.ExpandEnv("NOT_A_VAR")
	_ = envx.Get("API_KEY")
	_ = getenvDefault("TIMEOUT_MS", "500")
	_ = plain("lowercase_thing")
	_ = os.Getenv(configPath)
}
`,

	"extra/keep.go": `package extra

// KeepMe survives the exclude list.
func KeepMe() {}
`,

	"extra/skip.go": `package extra

// SkipMe is excluded by exact file path.
func SkipMe() {}
`,

	"excluded/ex.go": `package excluded

// ExcludedOnly lives in an excluded directory.
func ExcludedOnly() {}
`,

	"broken/broken.go": `package broken

func Oops( {
`,

	"vendor/skipme/skip.go": `package skipme

// VendorOnly must never be indexed.
func VendorOnly() {}
`,

	"testdata/skipme.go": `package testdata

// TestdataOnly must never be indexed.
func TestdataOnly() {}
`,

	"_ignored/ig.go": `package ignored

// IgnoredOnly must never be indexed.
func IgnoredOnly() {}
`,

	".hidden/hid.go": `package hidden

// HiddenOnly must never be indexed.
func HiddenOnly() {}
`,
}

// writeFixture materialises the fixture module in a temporary directory and
// returns its root.
func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, src := range fixture {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(strings.ReplaceAll(src, "~", "`")), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// defaultOptions excludes one directory and one exact file.
func defaultOptions() Options {
	return Options{Exclude: []string{"excluded", "extra/skip.go"}}
}

// build indexes the fixture with the given options.
func build(t *testing.T, opts Options) (*Index, []error) {
	t.Helper()
	ix, errs := Build(writeFixture(t), opts)
	if ix == nil {
		t.Fatal("Build returned a nil index")
	}
	return ix, errs
}

// lineOfText returns the 1-based line of the first line containing sub.
func lineOfText(t *testing.T, rel, sub string) int {
	t.Helper()
	for i, line := range strings.Split(strings.ReplaceAll(fixture[rel], "~", "`"), "\n") {
		if strings.Contains(line, sub) {
			return i + 1
		}
	}
	t.Fatalf("%s: no line contains %q", rel, sub)
	return 0
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestBuildParseErrors(t *testing.T) {
	ix, errs := build(t, defaultOptions())
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exactly 1 parse failure", errs)
	}
	if !strings.Contains(filepath.ToSlash(errs[0].Error()), "broken/broken.go") {
		t.Errorf("error %v does not name broken/broken.go", errs[0])
	}
	st := ix.Stats()
	if st.ParseErrors != 1 {
		t.Errorf("Stats().ParseErrors = %d, want 1", st.ParseErrors)
	}
	if st.Files != 9 {
		t.Errorf("Stats().Files = %d, want 9", st.Files)
	}
	if st.Packages != len(ix.Packages()) || st.Packages != 6 {
		t.Errorf("Stats().Packages = %d, want 6", st.Packages)
	}
	if st.Symbols == 0 || st.Flags != len(ix.Flags()) || st.Envs != len(ix.Envs()) || st.JSONKeys != len(ix.JSONKeys()) {
		t.Errorf("Stats() counters inconsistent: %+v", st)
	}
}

func TestModuleAndPackages(t *testing.T) {
	ix, _ := build(t, defaultOptions())

	if got := ix.ModulePath(); got != "example.com/demo" {
		t.Errorf("ModulePath() = %q, want %q", got, "example.com/demo")
	}
	want := []string{"cli", "extra", "generic", "httpx", "main", "store"}
	if got := ix.Packages(); !equal(got, want) {
		t.Errorf("Packages() = %v, want %v", got, want)
	}

	for _, tc := range []struct {
		name string
		want bool
	}{
		{"httpx", true},
		{"store", true},
		{"main", true},
		{"generic", true},
		{"extra", true},
		{"broken", false},   // never parsed
		{"skipme", false},   // vendor/
		{"testdata", false}, // testdata/
		{"ignored", false},  // _ignored/
		{"hidden", false},   // .hidden/
		{"excluded", false}, // Options.Exclude
		{"nope", false},
	} {
		if got := ix.IsPackage(tc.name); got != tc.want {
			t.Errorf("IsPackage(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}

	if got := ix.PackageDirs("store"); !equal(got, []string{"internal/store"}) {
		t.Errorf("PackageDirs(store) = %v, want [internal/store]", got)
	}
	if got := ix.PackageDirs("main"); !equal(got, []string{"."}) {
		t.Errorf("PackageDirs(main) = %v, want [.]", got)
	}
	if got := ix.PackageDirs("nope"); len(got) != 0 {
		t.Errorf("PackageDirs(nope) = %v, want empty", got)
	}
}

func TestPackageDir(t *testing.T) {
	ix, _ := build(t, defaultOptions())
	for _, tc := range []struct {
		path string
		want string
		ok   bool
	}{
		{"example.com/demo", ".", true},
		{"example.com/demo/httpx", "httpx", true},
		{"example.com/demo/internal/store", "internal/store", true},
		{"example.com/demo/generic", "generic", true},
		{"example.com/demo/extra", "extra", true},
		{"example.com/demo/excluded", "", false},
		{"example.com/demo/vendor/skipme", "", false},
		{"httpx", "", false},
		{"other.com/demo/httpx", "", false},
	} {
		got, ok := ix.PackageDir(tc.path)
		if got != tc.want || ok != tc.ok {
			t.Errorf("PackageDir(%q) = (%q, %v), want (%q, %v)", tc.path, got, ok, tc.want, tc.ok)
		}
	}
}

func TestHasSymbol(t *testing.T) {
	ix, _ := build(t, defaultOptions())
	for _, tc := range []struct {
		q    string
		want bool
	}{
		// pkg.Name
		{"httpx.WriteData", true},
		{"httpx.writeRaw", true},
		{"httpx.ErrClosed", true},
		{"httpx.MaxRetries", true},
		{"httpx.Client", true},
		{"store.WriteData", true},
		{"store.DefaultName", true},
		{"main.helper", true},
		{"main.Version", true},
		{"extra.KeepMe", true},
		// call and pointer decorations
		{"httpx.WriteData()", true},
		{"httpx.WriteData(v, opts)", true},
		{"*httpx.Client", true},
		{"&httpx.Client", true},
		{"  httpx.WriteData  ", true},
		// import path prefixes
		{"example.com/demo/httpx.WriteData", true},
		{"example.com/demo/internal/store.Store.Get", true},
		// pkg.Type.Member
		{"httpx.Client.Do", true},
		{"httpx.Client.Close", true},
		{"httpx.Client.reset", true},
		{"httpx.Client.Timeout", true},
		{"httpx.Client.baseURL", true},
		{"httpx.Client.Logger", true}, // embedded field
		{"httpx.Handler.Serve", true}, // interface method
		{"httpx.Handler.Name", true},
		{"store.Config.Server", true},
		{"store.Config.Meta", true},
		// Type.Member without a package
		{"Client.Do", true},
		{"Client.Timeout", true},
		{"Handler.Serve", true},
		{"Store.Get", true},
		// generics
		{"generic.Stack.Push", true},
		{"generic.Stack[int].Push", true},
		{"generic.Stack[K, V].Pop", true},
		{"Stack[T].Push", true},
		{"generic.Pair.First", true},
		{"generic.Pair.Key", true},
		// bare top level identifiers
		{"WriteData", true},
		{"Client", true},
		{"ErrClosed", true},
		{"Setup", true},
		// test-only symbols are still reachable
		{"httpx.helperOnlyInTest", true},
		{"helperOnlyInTest", true},
		{"httpx.TestWriteData", true},
		// misses
		{"httpx.WriteJSON", false},
		{"httpx.Client.Send", false},
		{"Do", false},      // bare method names are not indexed
		{"Timeout", false}, // bare field names are not indexed
		{"Serve", false},
		{"", false},
		{"a.b.c.d", false},
		{"excluded.ExcludedOnly", false},
		{"ExcludedOnly", false},
		{"extra.SkipMe", false},
		{"skipme.VendorOnly", false},
		{"TestdataOnly", false},
		{"IgnoredOnly", false},
		{"HiddenOnly", false},
		{"broken.Oops", false},
	} {
		if got := ix.HasSymbol(tc.q); got != tc.want {
			t.Errorf("HasSymbol(%q) = %v, want %v", tc.q, got, tc.want)
		}
	}
}

func TestSymbolFile(t *testing.T) {
	ix, _ := build(t, defaultOptions())
	for _, tc := range []struct {
		q    string
		file string
		line int
	}{
		{"httpx.WriteData", "httpx/httpx.go", lineOfText(t, "httpx/httpx.go", "func WriteData(")},
		{"httpx.Client.Do", "httpx/httpx.go", lineOfText(t, "httpx/httpx.go", "func (c *Client) Do(")},
		{"Client.Timeout", "httpx/httpx.go", lineOfText(t, "httpx/httpx.go", "Timeout int")},
		{"generic.Stack.Push", "generic/stack.go", lineOfText(t, "generic/stack.go", "func (s *Stack[T]) Push(")},
		{"store.Config", "internal/store/config.go", lineOfText(t, "internal/store/config.go", "type Config struct")},
		{"httpx.helperOnlyInTest", "httpx/httpx_test.go", lineOfText(t, "httpx/httpx_test.go", "func helperOnlyInTest(")},
	} {
		file, line, ok := ix.SymbolFile(tc.q)
		if !ok {
			t.Errorf("SymbolFile(%q) not found", tc.q)
			continue
		}
		if file != tc.file || line != tc.line {
			t.Errorf("SymbolFile(%q) = (%q, %d), want (%q, %d)", tc.q, file, line, tc.file, tc.line)
		}
	}
	if _, _, ok := ix.SymbolFile("httpx.Nope"); ok {
		t.Error("SymbolFile(httpx.Nope) reported ok")
	}
}

func TestIsType(t *testing.T) {
	ix, _ := build(t, defaultOptions())
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"Client", true},
		{"Logger", true},
		{"Handler", true},
		{"Stack", true},
		{"Pair", true},
		{"Config", true},
		{"Store", true},
		{"level", true},
		{"Stack[T]", true}, // instantiation is stripped
		{"WriteData", false},
		{"Do", false},
		{"Timeout", false},
		{"", false},
	} {
		if got := ix.IsType(tc.name); got != tc.want {
			t.Errorf("IsType(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFlags(t *testing.T) {
	ix, _ := build(t, defaultOptions())
	want := []string{"config", "dry-run", "log-level", "mode", "out", "timeout", "verbose"}
	if got := ix.Flags(); !equal(got, want) {
		t.Errorf("Flags() = %v, want %v", got, want)
	}
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"config", true},
		{"--config", true},
		{"-out", true},
		{"dry-run", true},
		{"log-level", true},
		{"mode", true},
		{"nope", false},
		{"build", false}, // the FlagSet name, not a flag
		{"", false},
	} {
		if got := ix.HasFlag(tc.name); got != tc.want {
			t.Errorf("HasFlag(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEnvs(t *testing.T) {
	ix, _ := build(t, defaultOptions())
	want := []string{"API_KEY", "DOCROT_DEBUG", "DROP_ME", "HOME_DIR", "SET_ME", "SYS_VAR", "TIMEOUT_MS"}
	if got := ix.Envs(); !equal(got, want) {
		t.Errorf("Envs() = %v, want %v", got, want)
	}
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"HOME_DIR", true},
		{"DOCROT_DEBUG", true},
		{"API_KEY", true},
		{"TIMEOUT_MS", true},
		{"SYS_VAR", true},
		{"NOT_A_VAR", false}, // os.ExpandEnv takes a template
		{"lowercase_thing", false},
		{"NOPE", false},
	} {
		if got := ix.HasEnv(tc.name); got != tc.want {
			t.Errorf("HasEnv(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestJSONKeys(t *testing.T) {
	ix, _ := build(t, defaultOptions())
	for _, tc := range []struct {
		key  string
		want bool
	}{
		{"server", true},
		{"server.addr", true},
		{"server.address", true}, // yaml spelling
		{"server.port", true},
		{"server.tls", true},
		{"server.tls.cert_file", true},
		{"server.tls.next", true},
		{"peers", true},
		{"peers.host", true},
		{"labels", true},
		{"version", true},                    // inlined embedded struct
		{"Debug", true},                      // untagged field keeps its Go name
		{"addr", true},                       // ServerConfig as its own root
		{"host", true},                       // PeerConfig as its own root
		{"l2.l3.l4.l5.l6.l7", true},          // exactly at the depth cap
		{"server.tls.next.cert_file", false}, // recursion stops on a cycle
		{"l2.l3.l4.l5.l6.l7.leaf", false},    // beyond the depth cap
		{"leaf", true},                       // but L7 is still a root
		{"Ignored", false},                   // json:"-"
		{"server.Ignored", false},
		{"Server", false}, // the tag wins over the field name
		{"nope", false},
		{"", false},
	} {
		if got := ix.HasJSONKey(tc.key); got != tc.want {
			t.Errorf("HasJSONKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
	keys := ix.JSONKeys()
	if !sort.StringsAreSorted(keys) {
		t.Errorf("JSONKeys() is not sorted: %v", keys)
	}
	if !contains(keys, "server.addr") {
		t.Errorf("JSONKeys() = %v, missing server.addr", keys)
	}
}

func TestExported(t *testing.T) {
	ix, _ := build(t, defaultOptions())

	got := ix.Exported(false)
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].Qualified < got[j].Qualified }) {
		t.Error("Exported() is not sorted by Qualified")
	}
	names := make([]string, 0, len(got))
	for _, e := range got {
		names = append(names, e.Qualified)
		if e.File == "" || e.Line <= 0 {
			t.Errorf("Exported entry %q has no location (%q:%d)", e.Qualified, e.File, e.Line)
		}
		if e.Kind != "gosym" {
			t.Errorf("Exported entry %q has kind %q, want gosym", e.Qualified, e.Kind)
		}
	}
	for _, want := range []string{
		"httpx.WriteData", "httpx.Client", "httpx.Client.Do", "httpx.Client.Close",
		"httpx.Handler", "httpx.Handler.Serve", "httpx.ErrClosed", "httpx.MaxRetries",
		"generic.Stack", "generic.Stack.Push", "generic.Pair.First",
		"cli.Setup", "cli.Load", "extra.KeepMe",
	} {
		if !contains(names, want) {
			t.Errorf("Exported(false) = %v, missing %q", names, want)
		}
	}
	for _, bad := range []string{
		"main.Version", "main.helper", // package main
		"store.WriteData", "store.Store", // internal/
		"httpx.writeRaw", "httpx.Client.reset", // unexported
		"httpx.Client.Timeout", "httpx.Logger.Prefix", // struct fields
		"httpx.TestWriteData", "httpx.helperOnlyInTest", // _test.go
		"cli.envGetter.Get", "extra.SkipMe",
	} {
		if contains(names, bad) {
			t.Errorf("Exported(false) = %v, should not contain %q", names, bad)
		}
	}

	internal := ix.Exported(true)
	if len(internal) <= len(got) {
		t.Fatalf("Exported(true) has %d entries, Exported(false) has %d", len(internal), len(got))
	}
	var innames []string
	for _, e := range internal {
		innames = append(innames, e.Qualified)
	}
	for _, want := range []string{"store.WriteData", "store.Store", "store.Store.Get", "store.Config"} {
		if !contains(innames, want) {
			t.Errorf("Exported(true) missing %q", want)
		}
	}
	if contains(innames, "store.Config.Server") {
		t.Error("Exported(true) must not contain struct fields")
	}
}

func TestSimilar(t *testing.T) {
	ix, _ := build(t, defaultOptions())
	for _, tc := range []struct {
		name string
		q    string
		n    int
		want []string // expected prefix of the result
		none bool
	}{
		{name: "typo in package", q: "httpx.WriteDta", n: 3, want: []string{"httpx.WriteData"}},
		{name: "renamed symbol", q: "httpx.WriteJSON", n: 3, want: []string{"httpx.WriteData"}},
		{name: "wrong package", q: "generic.WriteData", n: 2, want: []string{"httpx.WriteData", "store.WriteData"}},
		{name: "bare", q: "WriteDat", n: 1, want: []string{"httpx.WriteData"}},
		{name: "member form", q: "Client.D", n: 1, want: []string{"httpx.Client.Do"}},
		{name: "call syntax", q: "httpx.WriteDat()", n: 1, want: []string{"httpx.WriteData"}},
		{name: "nothing close", q: "httpx.CompletelyDifferentThing", n: 3, none: true},
		{name: "zero results asked", q: "httpx.WriteDta", n: 0, none: true},
		{name: "empty", q: "", n: 3, none: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ix.Similar(tc.q, tc.n)
			if tc.none {
				if len(got) != 0 {
					t.Fatalf("Similar(%q, %d) = %v, want none", tc.q, tc.n, got)
				}
				return
			}
			if len(got) > tc.n {
				t.Fatalf("Similar(%q, %d) returned %d results: %v", tc.q, tc.n, len(got), got)
			}
			if len(got) < len(tc.want) {
				t.Fatalf("Similar(%q, %d) = %v, want prefix %v", tc.q, tc.n, got, tc.want)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("Similar(%q, %d)[%d] = %q, want %q (full: %v)", tc.q, tc.n, i, got[i], w, got)
				}
			}
		})
	}
	// The query itself is never suggested.
	for _, s := range ix.Similar("httpx.WriteData", 5) {
		if s == "httpx.WriteData" {
			t.Error("Similar suggested the query itself")
		}
	}
}

func TestIncludeTests(t *testing.T) {
	ix, _ := build(t, Options{Exclude: defaultOptions().Exclude, IncludeTests: true})

	if !ix.HasSymbol("httpx.helperOnlyInTest") {
		t.Error("HasSymbol(httpx.helperOnlyInTest) = false with IncludeTests")
	}
	// Test symbols are candidates for suggestions only when tests are included.
	if got := ix.Similar("httpx.helperOnlyInTes", 3); len(got) == 0 || got[0] != "httpx.helperOnlyInTest" {
		t.Errorf("Similar(httpx.helperOnlyInTes) = %v, want httpx.helperOnlyInTest first", got)
	}
	// Exported never reports test declarations.
	for _, e := range ix.Exported(true) {
		if strings.HasSuffix(e.File, "_test.go") {
			t.Errorf("Exported includes the test declaration %q", e.Qualified)
		}
	}

	off, _ := build(t, defaultOptions())
	if got := off.Similar("httpx.helperOnlyInTes", 3); contains(got, "httpx.helperOnlyInTest") {
		t.Errorf("Similar(%q) = %v, test symbols must not be suggested", "httpx.helperOnlyInTes", got)
	}
}

func TestNoGoMod(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix, errs := Build(root, Options{})
	if len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	if ix.ModulePath() != "" {
		t.Errorf("ModulePath() = %q, want empty", ix.ModulePath())
	}
	if !ix.HasSymbol("a.F") {
		t.Error("HasSymbol(a.F) = false")
	}
	if dir, ok := ix.PackageDir("."); !ok || dir != "." {
		t.Errorf("PackageDir(.) = (%q, %v), want (., true)", dir, ok)
	}
}

func TestEmptyRoot(t *testing.T) {
	ix, errs := Build(t.TempDir(), Options{})
	if len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	if got := ix.Stats(); got != (Stats{}) {
		t.Errorf("Stats() = %+v, want zero", got)
	}
	if ix.HasSymbol("a.B") || ix.HasFlag("x") || ix.HasEnv("X") || ix.HasJSONKey("x") {
		t.Error("empty index reported a hit")
	}
	if got := ix.Similar("a.B", 3); len(got) != 0 {
		t.Errorf("Similar on an empty index = %v", got)
	}
}

func TestConcurrentReads(t *testing.T) {
	ix, _ := build(t, defaultOptions())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = ix.HasSymbol("httpx.Client.Do")
				_ = ix.Similar("httpx.WriteDta", 3)
				_ = ix.Exported(false)
				_ = ix.Flags()
				_ = ix.Packages()
			}
		}()
	}
	wg.Wait()
}

func TestNormalizeSymbol(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"httpx.WriteData", "httpx.WriteData"},
		{"  httpx.WriteData  ", "httpx.WriteData"},
		{"httpx.WriteData()", "httpx.WriteData"},
		{"httpx.WriteData(ctx, v)", "httpx.WriteData"},
		{"*httpx.Client", "httpx.Client"},
		{"&httpx.Client", "httpx.Client"},
		{"**httpx.Client", "httpx.Client"},
		{"generic.Stack[int]", "generic.Stack"},
		{"generic.Stack[K, V].Push", "generic.Stack.Push"},
		{"New[map[string]int]()", "New"},
		{"example.com/demo/httpx.WriteData", "httpx.WriteData"},
		{"httpx.", "httpx"},
		{".WriteData", "WriteData"},
		{"", ""},
		{"()", ""},
	} {
		if got := normalizeSymbol(tc.in); got != tc.want {
			t.Errorf("normalizeSymbol(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDamerau(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "acb", 1},    // transposition
		{"write", "writ", 1}, // deletion
		{"writ", "write", 1}, // insertion
		{"write", "wrote", 1},
		{"writedata", "writedta", 1},
		{"writedata", "writejson", 4},
		{"kitten", "sitting", 3},
		{"日本語", "日本", 1},
	} {
		if got := damerau(tc.a, tc.b); got != tc.want {
			t.Errorf("damerau(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := damerau(tc.b, tc.a); got != tc.want {
			t.Errorf("damerau(%q, %q) = %d, want %d (symmetry)", tc.b, tc.a, got, tc.want)
		}
	}
}

func TestExcludeMatching(t *testing.T) {
	for _, tc := range []struct {
		excl []string
		rel  string
		want bool
	}{
		{[]string{"vendor"}, "vendor", true},
		{[]string{"vendor"}, "vendor/x/y.go", true},
		{[]string{"vendor"}, "vendored/x.go", false},
		{[]string{"a/b.go"}, "a/b.go", true},
		{[]string{"a/b.go"}, "a/bb.go", false},
		{[]string{"./docs/"}, "docs/x.go", true},
		{nil, "anything", false},
	} {
		if got := excluded(normalizeExcludes(tc.excl), tc.rel); got != tc.want {
			t.Errorf("excluded(%v, %q) = %v, want %v", tc.excl, tc.rel, got, tc.want)
		}
	}
}

// equal reports whether two string slices are identical.
func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
