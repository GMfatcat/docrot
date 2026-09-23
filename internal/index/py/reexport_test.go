package py

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestReexportsModulesAndExamples(t *testing.T) {
	root := writeTree(t, map[string]string{
		"fastapi/__init__.py":      "from starlette import status\nfrom .applications import FastAPI as FastAPI\nimport fastapi.responses as responses\nfrom .params import (\n    Depends,\n    Query,\n)\n",
		"fastapi/applications.py":  "class FastAPI:\n    def add_middleware(self, m):\n        pass\n",
		"fastapi/responses.py":     "class JSONResponse:\n    pass\n",
		"fastapi/params.py":        "class Depends:\n    pass\nclass Query:\n    pass\n",
		"docs_src/app/__init__.py": "",
		"docs_src/app/main.py":     "def main():\n    pass\n",
	})
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]bool{
		"fastapi.status":                 true, // imported name
		"fastapi.FastAPI":                true, // re-export via alias
		"fastapi.Depends":                true, // parenthesised multi-line import
		"fastapi.Query":                  true,
		"fastapi.responses":              true, // module
		"fastapi.responses.JSONResponse": true,
		"fastapi.applications.FastAPI":   true,
		"fastapi.Nope":                   false,
		"FastAPI.add_middleware":         true,
	} {
		if got := ix.Has(q); got != want {
			t.Errorf("Has(%q) = %v, want %v", q, got, want)
		}
	}
	if !ix.IsModule("fastapi.responses") || ix.IsModule("fastapi.nope") {
		t.Error("IsModule wrong")
	}
	if !ix.IsExampleModule("app.main") && !ix.IsExampleModule("docs_src.app.main") {
		t.Errorf("docs_src module should be an example module; modules=%v", ix.Modules())
	}
	if ix.IsExampleModule("fastapi.applications") {
		t.Error("library module flagged as example")
	}
}

func TestImportedNames(t *testing.T) {
	got := importedNames("a, b as c, os.path as p, x.y, *  # comment")
	want := []string{"a", "c", "p", "y"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
