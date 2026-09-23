package lang_test

import (
	"os"
	"path/filepath"
	"testing"

	"docrot/internal/index/lang"
	"docrot/internal/index/odin"
	"docrot/internal/index/py"
	"docrot/internal/model"
)

// TestImplementations builds the Odin and Python indexes over a tiny tree
// and exercises them only through the lang.Index interface, the way the
// composite index does.
func TestImplementations(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("demo/demo.odin", "package demo\n\n// sum adds.\nsum :: proc(a, b: int) -> int {\n\treturn a + b\n}\n")
	write("lib/__init__.py", "")
	write("lib/core.py", "def load(path):\n    \"\"\"Load.\"\"\"\n    return path\n")

	o, err := odin.Build(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := py.Build(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		kind      model.Kind
		ix        lang.Index
		namespace string
		symbol    string
	}{
		{model.KindOdinSym, o, "demo", "demo.sum"},
		{model.KindPySym, p, "lib.core", "lib.core.load"},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			if c.ix.Empty() {
				t.Fatal("Empty() = true")
			}
			if st := c.ix.Counts(); st.Files < 1 || st.Symbols != 1 || st.Namespaces < 1 {
				t.Errorf("Counts() = %+v", st)
			}
			if !c.ix.IsNamespace(c.namespace) || c.ix.IsNamespace(c.symbol) {
				t.Errorf("IsNamespace: %q should be one, %q not", c.namespace, c.symbol)
			}
			if !c.ix.Has(c.symbol) || !c.ix.Has(c.symbol+"()") {
				t.Errorf("Has(%q) = false", c.symbol)
			}
			if syms := c.ix.Symbols(); len(syms) != 1 || syms[0] != c.symbol {
				t.Errorf("Symbols() = %v", syms)
			}
			if sp, ok := c.ix.Span(c.symbol); !ok || sp.Kind != c.kind || sp.Qualified != c.symbol {
				t.Errorf("Span(%q) = %+v, %v", c.symbol, sp, ok)
			}
			if all := c.ix.AllSpans(); len(all) != 1 {
				t.Errorf("AllSpans() = %d spans", len(all))
			}
			if c.ix.IsExample(c.namespace) {
				t.Errorf("IsExample(%q) = true", c.namespace)
			}
			_ = c.ix.Opaque(c.symbol)
			if c.ix.Literals() == nil || c.ix.Defaults() == nil {
				t.Error("Literals() or Defaults() is nil")
			}
			_ = c.ix.Routes()
			if f, line, ok := c.ix.File(c.symbol); !ok || f == "" || line == 0 {
				t.Errorf("File(%q) = %q, %d, %v", c.symbol, f, line, ok)
			}
		})
	}
}

// TestLangTable checks that every language row is complete and unique: the
// composite index, the extractor and the CLI all trust these fields.
func TestLangTable(t *testing.T) {
	ids := map[string]bool{}
	kinds := map[model.Kind]bool{}
	exts := map[string]bool{}
	for _, l := range model.Langs {
		if l.ID == "" || l.Name == "" || l.Kind == "" || len(l.Exts) == 0 {
			t.Errorf("incomplete row %+v", l)
		}
		if l.Sep != "." && l.Sep != "::" {
			t.Errorf("%s: separator %q", l.ID, l.Sep)
		}
		if ids[l.ID] || kinds[l.Kind] {
			t.Errorf("%s: duplicate id or kind", l.ID)
		}
		ids[l.ID], kinds[l.Kind] = true, true
		for _, e := range l.Exts {
			if exts[e] {
				t.Errorf("%s: extension %s claimed twice", l.ID, e)
			}
			exts[e] = true
		}
		if got, ok := model.LangOf(l.Kind); !ok || got.ID != l.ID {
			t.Errorf("LangOf(%s) = %+v, %v", l.Kind, got, ok)
		}
		if got, ok := model.LangByID(l.ID); !ok || got.Kind != l.Kind {
			t.Errorf("LangByID(%s) = %+v, %v", l.ID, got, ok)
		}
		if !l.Kind.IsSymbol() {
			t.Errorf("%s.IsSymbol() = false", l.Kind)
		}
	}
	if !model.KindGoSymbol.IsSymbol() || model.KindPath.IsSymbol() {
		t.Error("IsSymbol: Go yes, path no")
	}
}
