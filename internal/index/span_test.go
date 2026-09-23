package index

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"docrot/internal/model"
)

func buildSpanIndex(t *testing.T) *Index {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/demo\n\ngo 1.26\n",
		"demo.go": `package demo

// Greet greets name.
func Greet(name string) string { return "hi " + name }
`,
		"lib.py": `def load(path):
    """Load a file."""
    return path
`,
		"demo.odin": `package demo

// sum adds a and b.
sum :: proc(a, b: int) -> int {
	return a + b
}
`,
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ix, warns, err := Build(root, Options{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, w := range warns {
		t.Fatalf("build warning: %v", w)
	}
	return ix
}

func TestSymbolSpan(t *testing.T) {
	ix := buildSpanIndex(t)

	tests := []struct {
		name      string
		kind      model.Kind
		qualified string
		want      model.SymbolSpan // empty Qualified means "must not resolve"
	}{
		{
			name: "go", kind: model.KindGoSymbol, qualified: "demo.Greet",
			want: model.SymbolSpan{
				Qualified: "demo.Greet", Kind: model.KindGoSymbol, File: "demo.go",
				DocStart: 3, DocEnd: 3, DeclLine: 4, BodyStart: 4, BodyEnd: 4,
				Doc:      []string{"Greet greets name."},
				Params:   []string{"name"},
				Exported: true,
			},
		},
		{
			name: "python", kind: model.KindPySym, qualified: "lib.load",
			want: model.SymbolSpan{
				Qualified: "lib.load", Kind: model.KindPySym, File: "lib.py",
				DocStart: 2, DocEnd: 2, DeclLine: 1, BodyStart: 1, BodyEnd: 3,
				Doc:      []string{"Load a file."},
				Params:   []string{"path"},
				Exported: true,
			},
		},
		{
			name: "odin", kind: model.KindOdinSym, qualified: "demo.sum",
			want: model.SymbolSpan{
				Qualified: "demo.sum", Kind: model.KindOdinSym, File: "demo.odin",
				DocStart: 3, DocEnd: 3, DeclLine: 4, BodyStart: 4, BodyEnd: 6,
				Doc:      []string{"sum adds a and b."},
				Params:   []string{"a", "b"},
				Exported: true,
			},
		},
		{name: "wrong language", kind: model.KindGoSymbol, qualified: "lib.load"},
		{name: "unsupported kind", kind: model.KindPath, qualified: "demo.Greet"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ix.SymbolSpan(tt.kind, tt.qualified)
			if tt.want.Qualified == "" {
				if ok {
					t.Fatalf("SymbolSpan(%q, %q) = %+v, want not found", tt.kind, tt.qualified, got)
				}
				return
			}
			if !ok {
				t.Fatalf("SymbolSpan(%q, %q) not found", tt.kind, tt.qualified)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SymbolSpan(%q, %q)\n got %+v\nwant %+v", tt.kind, tt.qualified, got, tt.want)
			}
		})
	}
}

func TestAllSpans(t *testing.T) {
	ix := buildSpanIndex(t)

	type pair struct {
		kind      model.Kind
		qualified string
	}
	want := []pair{
		{model.KindGoSymbol, "demo.Greet"},
		{model.KindOdinSym, "demo.sum"},
		{model.KindPySym, "lib.load"},
	}

	var got []pair
	for _, sp := range ix.AllSpans(false) {
		got = append(got, pair{sp.Kind, sp.Qualified})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AllSpans() = %v, want %v", got, want)
	}
}
