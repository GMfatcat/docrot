package odin

import (
	"reflect"
	"testing"

	"docrot/internal/model"
)

// spanOdinFixture is the file written by buildSpanIndex. Asserted line
// numbers are the literal line numbers of this source, "package demo" being
// line 1.
const spanOdinFixture = `package demo

// Sum adds two numbers.
// It never overflows.
sum :: proc(a, b: int) -> int {
	return a + b
}

// hidden is internal.
@(private)
hidden :: proc(x: f32) {
}

// Point is a 2D point.
Point :: struct {
	x: f32,
	y: f32,
}

// external comes from C.
external :: proc(handle: rawptr, $T: typeid) -> bool ---

Color :: enum {
	Red,
	Green,
}
`

func buildSpanIndex(t *testing.T) *Index {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "demo.odin", spanOdinFixture)
	ix, err := Build(root, nil)
	if err != nil {
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
			name:   "proc with doc comment",
			lookup: "demo.sum",
			want: model.SymbolSpan{
				Qualified: "demo.sum", Kind: model.KindOdinSym, File: "demo.odin",
				DocStart: 3, DocEnd: 4, DeclLine: 5, BodyStart: 5, BodyEnd: 7,
				Doc:      []string{"Sum adds two numbers.", "It never overflows."},
				Params:   []string{"a", "b"},
				Exported: true,
			},
		},
		{
			name:   "private proc",
			lookup: "demo.hidden",
			want: model.SymbolSpan{
				Qualified: "demo.hidden", Kind: model.KindOdinSym, File: "demo.odin",
				DocStart: 9, DocEnd: 9, DeclLine: 11, BodyStart: 11, BodyEnd: 12,
				Doc:    []string{"hidden is internal."},
				Params: []string{"x"},
			},
		},
		{
			name:   "struct",
			lookup: "demo.Point",
			want: model.SymbolSpan{
				Qualified: "demo.Point", Kind: model.KindOdinSym, File: "demo.odin",
				DocStart: 14, DocEnd: 14, DeclLine: 15, BodyStart: 15, BodyEnd: 18,
				Doc:      []string{"Point is a 2D point."},
				Exported: true,
			},
		},
		{
			name:   "proc without a body",
			lookup: "demo.external",
			want: model.SymbolSpan{
				Qualified: "demo.external", Kind: model.KindOdinSym, File: "demo.odin",
				DocStart: 20, DocEnd: 20, DeclLine: 21, BodyStart: 21, BodyEnd: 21,
				Doc:      []string{"external comes from C."},
				Params:   []string{"handle", "T"},
				Exported: true,
			},
		},
		{
			name:   "enum without doc",
			lookup: "demo.Color",
			want: model.SymbolSpan{
				Qualified: "demo.Color", Kind: model.KindOdinSym, File: "demo.odin",
				DeclLine: 23, BodyStart: 23, BodyEnd: 26,
				Exported: true,
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
		{"demo.sum", "demo.sum"},
		{"sum", "demo.sum"},
		{"sum()", "demo.sum"},
		{"Point", "demo.Point"},
		{"demo.missing", ""},
		{"missing", ""},
		{"", ""},
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

	want := []string{"demo.Color", "demo.Point", "demo.external", "demo.hidden", "demo.sum"}

	var got []string
	for _, sp := range ix.AllSpans() {
		if sp.Kind != model.KindOdinSym || sp.File == "" || sp.DeclLine == 0 || sp.BodyEnd == 0 {
			t.Errorf("AllSpans returned incomplete span %+v", sp)
		}
		got = append(got, sp.Qualified)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AllSpans() = %v, want %v", got, want)
	}
}
