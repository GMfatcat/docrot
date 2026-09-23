package gosym

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"docrot/internal/model"
)

// spanFixture is one package written to disk by buildSpanIndex. Line numbers
// asserted below are the literal line numbers of this source, starting at
// "package demo" on line 1.
const spanFixture = `package demo

import "errors"

// Greet returns a greeting for name.
// It never fails.
func Greet(name string) string {
	return "hi " + name
}

// Client talks to a server.
type Client struct {
	Timeout int
}

// Do performs a request and reports the status.
func (c *Client) Do(path string, body []byte) (n int, err error) {
	return 0, errors.New("x")
}

// Limits are the tunable caps.
const (
	// MaxRetries caps the number of attempts.
	MaxRetries = 3
	Timeout    = 30
)

/*
Config holds settings.

It is copied by value.
*/
type Config struct{}

// Map applies f to every element of in.
func Map[T, U any](in []T, f func(T) U) []U {
	var out []U
	return out
}

func Bare() {}

func internalOnly() {}
`

const spanInternalFixture = `package util

// Helper does nothing.
func Helper() {}
`

func buildSpanIndex(t *testing.T) *Index {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                "module example.com/demo\n\ngo 1.26\n",
		"demo.go":               spanFixture,
		"internal/util/util.go": spanInternalFixture,
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ix, errs := Build(root, Options{})
	for _, err := range errs {
		t.Fatalf("build: %v", err)
	}
	return ix
}

func TestSpan(t *testing.T) {
	ix := buildSpanIndex(t)

	tests := []struct {
		name   string
		lookup string
		want   model.SymbolSpan
	}{
		{
			name:   "func with line doc",
			lookup: "demo.Greet",
			want: model.SymbolSpan{
				Qualified: "demo.Greet", Kind: model.KindGoSymbol, File: "demo.go",
				DocStart: 5, DocEnd: 6, DeclLine: 7, BodyStart: 7, BodyEnd: 9,
				Doc:      []string{"Greet returns a greeting for name.", "It never fails."},
				Params:   []string{"name"},
				Exported: true,
			},
		},
		{
			name:   "type",
			lookup: "demo.Client",
			want: model.SymbolSpan{
				Qualified: "demo.Client", Kind: model.KindGoSymbol, File: "demo.go",
				DocStart: 11, DocEnd: 11, DeclLine: 12, BodyStart: 12, BodyEnd: 14,
				Doc:      []string{"Client talks to a server."},
				Exported: true,
			},
		},
		{
			name:   "method on pointer receiver with named results",
			lookup: "demo.Client.Do",
			want: model.SymbolSpan{
				Qualified: "demo.Client.Do", Kind: model.KindGoSymbol, File: "demo.go",
				DocStart: 16, DocEnd: 16, DeclLine: 17, BodyStart: 17, BodyEnd: 19,
				Doc:      []string{"Do performs a request and reports the status."},
				Params:   []string{"c", "path", "body", "n", "err"},
				Exported: true,
			},
		},
		{
			name:   "grouped const with its own doc",
			lookup: "demo.MaxRetries",
			want: model.SymbolSpan{
				Qualified: "demo.MaxRetries", Kind: model.KindGoSymbol, File: "demo.go",
				DocStart: 23, DocEnd: 23, DeclLine: 24, BodyStart: 24, BodyEnd: 24,
				Doc:      []string{"MaxRetries caps the number of attempts."},
				Exported: true,
			},
		},
		{
			name:   "grouped const falling back to the group doc",
			lookup: "demo.Timeout",
			want: model.SymbolSpan{
				Qualified: "demo.Timeout", Kind: model.KindGoSymbol, File: "demo.go",
				DocStart: 21, DocEnd: 21, DeclLine: 25, BodyStart: 25, BodyEnd: 25,
				Doc:      []string{"Limits are the tunable caps."},
				Exported: true,
			},
		},
		{
			name:   "type with block doc",
			lookup: "demo.Config",
			want: model.SymbolSpan{
				Qualified: "demo.Config", Kind: model.KindGoSymbol, File: "demo.go",
				DocStart: 28, DocEnd: 32, DeclLine: 33, BodyStart: 33, BodyEnd: 33,
				Doc:      []string{"Config holds settings.", "", "It is copied by value."},
				Exported: true,
			},
		},
		{
			name:   "generic func",
			lookup: "demo.Map",
			want: model.SymbolSpan{
				Qualified: "demo.Map", Kind: model.KindGoSymbol, File: "demo.go",
				DocStart: 35, DocEnd: 35, DeclLine: 36, BodyStart: 36, BodyEnd: 39,
				Doc:      []string{"Map applies f to every element of in."},
				Params:   []string{"in", "f"},
				Exported: true,
			},
		},
		{
			name:   "func without doc",
			lookup: "demo.Bare",
			want: model.SymbolSpan{
				Qualified: "demo.Bare", Kind: model.KindGoSymbol, File: "demo.go",
				DeclLine: 41, BodyStart: 41, BodyEnd: 41,
				Exported: true,
			},
		},
		{
			name:   "unexported func",
			lookup: "demo.internalOnly",
			want: model.SymbolSpan{
				Qualified: "demo.internalOnly", Kind: model.KindGoSymbol, File: "demo.go",
				DeclLine: 43, BodyStart: 43, BodyEnd: 43,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ix.Span(tt.lookup)
			if !ok {
				t.Fatalf("Span(%q) not found", tt.lookup)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Span(%q)\n got %+v\nwant %+v", tt.lookup, got, tt.want)
			}
		})
	}
}

func TestSpanLookupForms(t *testing.T) {
	ix := buildSpanIndex(t)

	tests := []struct {
		lookup string
		want   string // qualified name, "" when the lookup must fail
	}{
		{"demo.Greet", "demo.Greet"},
		{"Greet()", "demo.Greet"},
		{"Greet", "demo.Greet"},
		{"Client.Do", "demo.Client.Do"},
		{"*demo.Client.Do", "demo.Client.Do"},
		{"&Client.Do", "demo.Client.Do"},
		{"Map[int, string]", "demo.Map"},
		{"example.com/demo.Greet", "demo.Greet"},
		{"demo.Missing", ""},
		{"", ""},
		{"a.b.c.d", ""},
	}

	for _, tt := range tests {
		t.Run(tt.lookup, func(t *testing.T) {
			got, ok := ix.Span(tt.lookup)
			if tt.want == "" {
				if ok {
					t.Fatalf("Span(%q) = %q, want not found", tt.lookup, got.Qualified)
				}
				return
			}
			if !ok {
				t.Fatalf("Span(%q) not found", tt.lookup)
			}
			if got.Qualified != tt.want {
				t.Errorf("Span(%q) = %q, want %q", tt.lookup, got.Qualified, tt.want)
			}
		})
	}
}

func TestAllSpans(t *testing.T) {
	ix := buildSpanIndex(t)

	tests := []struct {
		name            string
		includeInternal bool
		want            []string
	}{
		{
			name: "public only",
			want: []string{"demo.Bare", "demo.Client", "demo.Client.Do", "demo.Config", "demo.Greet", "demo.Map"},
		},
		{
			name:            "with internal",
			includeInternal: true,
			want: []string{
				"demo.Bare", "demo.Client", "demo.Client.Do", "demo.Config", "demo.Greet",
				"demo.Map", "util.Helper",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := ix.AllSpans(tt.includeInternal)
			var got []string
			for _, sp := range spans {
				if !sp.Exported {
					t.Errorf("AllSpans returned unexported %q", sp.Qualified)
				}
				if sp.Kind != model.KindGoSymbol || sp.File == "" || sp.DeclLine == 0 {
					t.Errorf("AllSpans returned incomplete span %+v", sp)
				}
				got = append(got, sp.Qualified)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AllSpans(%v) = %v, want %v", tt.includeInternal, got, tt.want)
			}
		})
	}
}

func TestSpanResultIsACopy(t *testing.T) {
	ix := buildSpanIndex(t)
	first, ok := ix.Span("demo.Greet")
	if !ok {
		t.Fatal("Span(demo.Greet) not found")
	}
	first.Doc[0] = "mutated"
	first.Params[0] = "mutated"
	second, _ := ix.Span("demo.Greet")
	if second.Doc[0] == "mutated" || second.Params[0] == "mutated" {
		t.Errorf("Span shares its slices with the index: %+v", second)
	}
}
