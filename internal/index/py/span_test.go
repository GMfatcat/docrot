package py

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"docrot/internal/model"
)

// spanFixture is the module written by buildSpanIndex. Asserted line numbers
// are the literal line numbers of this source, the module docstring being
// line 1.
const spanFixture = `"""Module docstring."""

import functools


def load(path, *, retries=3):
    """Load a file.

    Returns its contents.
    """
    with open(path) as fh:
        return fh.read()


class Store:
    """A place for things."""

    def put(self, key, value=None):
        '''Store a value.'''
        self._data[key] = value

    async def fetch(self, key):
        return self._data[key]

    @property
    def size(self):
        """Number of items."""
        return len(self._data)

    def _hidden(self):
        return 1


def combine(
    first,
    second=2,
    *args,
    **kwargs,
):
    """Combine everything."""
    return first


def plain(a, b):
    return a + b
`

const spanExampleFixture = `def helper(x):
    """Only an example."""
    return x
`

func buildSpanIndex(t *testing.T) *Index {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"sample.py":          spanFixture,
		"tests/test_util.py": spanExampleFixture,
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
			name:   "module level def with multi-line docstring",
			lookup: "sample.load",
			want: model.SymbolSpan{
				Qualified: "sample.load", Kind: model.KindPySym, File: "sample.py",
				DocStart: 7, DocEnd: 10, DeclLine: 6, BodyStart: 6, BodyEnd: 12,
				Doc:      []string{"Load a file.", "", "Returns its contents."},
				Params:   []string{"path", "retries"},
				Exported: true,
			},
		},
		{
			name:   "class with single-line docstring",
			lookup: "sample.Store",
			want: model.SymbolSpan{
				Qualified: "sample.Store", Kind: model.KindPySym, File: "sample.py",
				DocStart: 16, DocEnd: 16, DeclLine: 15, BodyStart: 15, BodyEnd: 31,
				Doc:      []string{"A place for things."},
				Exported: true,
			},
		},
		{
			name:   "method with single-quoted docstring",
			lookup: "sample.Store.put",
			want: model.SymbolSpan{
				Qualified: "sample.Store.put", Kind: model.KindPySym, File: "sample.py",
				DocStart: 19, DocEnd: 19, DeclLine: 18, BodyStart: 18, BodyEnd: 20,
				Doc:      []string{"Store a value."},
				Params:   []string{"self", "key", "value"},
				Exported: true,
			},
		},
		{
			name:   "async def without docstring",
			lookup: "sample.Store.fetch",
			want: model.SymbolSpan{
				Qualified: "sample.Store.fetch", Kind: model.KindPySym, File: "sample.py",
				DeclLine: 22, BodyStart: 22, BodyEnd: 23,
				Params:   []string{"self", "key"},
				Exported: true,
			},
		},
		{
			name:   "decorated def",
			lookup: "sample.Store.size",
			want: model.SymbolSpan{
				Qualified: "sample.Store.size", Kind: model.KindPySym, File: "sample.py",
				DocStart: 27, DocEnd: 27, DeclLine: 26, BodyStart: 26, BodyEnd: 28,
				Doc:      []string{"Number of items."},
				Params:   []string{"self"},
				Exported: true,
			},
		},
		{
			name:   "unexported method",
			lookup: "sample.Store._hidden",
			want: model.SymbolSpan{
				Qualified: "sample.Store._hidden", Kind: model.KindPySym, File: "sample.py",
				DeclLine: 30, BodyStart: 30, BodyEnd: 31,
				Params: []string{"self"},
			},
		},
		{
			name:   "multi-line signature with varargs",
			lookup: "sample.combine",
			want: model.SymbolSpan{
				Qualified: "sample.combine", Kind: model.KindPySym, File: "sample.py",
				DocStart: 40, DocEnd: 40, DeclLine: 34, BodyStart: 34, BodyEnd: 41,
				Doc:      []string{"Combine everything."},
				Params:   []string{"first", "second", "args", "kwargs"},
				Exported: true,
			},
		},
		{
			name:   "def without docstring",
			lookup: "sample.plain",
			want: model.SymbolSpan{
				Qualified: "sample.plain", Kind: model.KindPySym, File: "sample.py",
				DeclLine: 44, BodyStart: 44, BodyEnd: 45,
				Params:   []string{"a", "b"},
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
		{"sample.load", "sample.load"},
		{"load()", "sample.load"},
		{"load", "sample.load"},
		{"Store.put", "sample.Store.put"},
		{"sample.Store.put", "sample.Store.put"},
		{"sample", ""},    // a module has no span
		{"functools", ""}, // an imported name has no span
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

	want := []string{
		"sample.Store", "sample.Store.fetch", "sample.Store.put", "sample.Store.size",
		"sample.combine", "sample.load", "sample.plain",
	}

	var got []string
	for _, sp := range ix.AllSpans() {
		if !sp.Exported {
			t.Errorf("AllSpans returned unexported %q", sp.Qualified)
		}
		if sp.Kind != model.KindPySym || sp.File == "" || sp.DeclLine == 0 || sp.BodyEnd == 0 {
			t.Errorf("AllSpans returned incomplete span %+v", sp)
		}
		got = append(got, sp.Qualified)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AllSpans() = %v, want %v", got, want)
	}
}
