package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"docrot/internal/model"
)

// TestExternalFlagsAreSkipped: docker's, go test's and git's flags in prose
// are not this program's claims — unless the code defines them.
func TestExternalFlagsAreSkipped(t *testing.T) {
	ix := newFake()
	r := New(ix, Options{})
	for _, name := range []string{"read-only", "platform", "race", "amend"} {
		if res := r.Resolve(ref(model.KindFlag, name, model.High, "README.md")); !res.Skipped {
			t.Errorf("--%s: %+v, want skipped", name, res)
		}
	}
	// a flag the program does define is still checked and found
	if res := r.Resolve(ref(model.KindFlag, "verbose", model.High, "README.md")); !res.OK {
		t.Errorf("--verbose: %+v, want ok", res)
	}
	// an unknown flag that is nobody's known flag is still reported
	if res := r.Resolve(ref(model.KindFlag, "frobnicate", model.High, "README.md")); res.Finding == nil {
		t.Errorf("--frobnicate: %+v, want a finding", res)
	}
	// the code owning a flag of an external name wins over the skip list
	ix.flags = append(ix.flags, "platform")
	if res := New(ix, Options{}).Resolve(ref(model.KindFlag, "platform", model.High, "README.md")); !res.OK {
		t.Errorf("--platform defined by the code: %+v, want ok", res)
	}
}

// TestBareMethodNameResolves: `Addr()` names a method some type has, and
// `httpx.Addr` is the short way of writing httpx.Server.Addr.
func TestBareMethodNameResolves(t *testing.T) {
	ix := newFake()
	ix.similar = map[model.Kind][]string{model.KindGoSymbol: {"httpx.Server.Addr"}}
	r := New(ix, Options{})
	if res := r.Resolve(ref(model.KindGoSymbol, "Addr", model.Medium, "README.md")); !res.OK {
		t.Errorf("Addr(): %+v, want ok (Server.Addr exists)", res)
	}
	if res := r.Resolve(ref(model.KindGoSymbol, "httpx.Addr", model.High, "README.md")); res.Finding == nil || res.Finding.Severity != model.SevInfo || !strings.Contains(res.Finding.Message, "httpx.Server.Addr") {
		t.Errorf("httpx.Addr: %+v, want an info finding naming httpx.Server.Addr", res)
	}
	res := r.Resolve(ref(model.KindGoSymbol, "Frob", model.Medium, "README.md"))
	if res.Finding == nil || res.Finding.Severity != model.SevInfo {
		t.Errorf("Frob(): %+v, want an info finding", res)
	}
}

// TestSiblingPackageOfTheSameName: `httpx.Response` is missing from this
// module's httpx but exported by a sibling module's package httpx — the
// finding becomes info and names the sibling.
func TestSiblingPackageOfTheSameName(t *testing.T) {
	sib := t.TempDir()
	dir := filepath.Join(sib, "httpx")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package httpx\n\n// Response is the envelope.\ntype Response[T any] struct{ Data T }\n\nfunc WriteJSON() {}\n\nconst (\n\tDefaultLimit = 10\n)\n"
	if err := os.WriteFile(filepath.Join(dir, "json.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// a test file must not count
	if err := os.WriteFile(filepath.Join(dir, "json_test.go"), []byte("package httpx\n\nfunc TestOnly() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := New(newFake(), Options{Siblings: []string{sib}})
	for _, name := range []string{"Response", "WriteJSON", "DefaultLimit"} {
		res := r.Resolve(ref(model.KindGoSymbol, "httpx."+name, model.High, "README.md"))
		if res.Finding == nil {
			t.Fatalf("httpx.%s: %+v, want a finding", name, res)
		}
		if res.Finding.Severity != model.SevInfo || !strings.Contains(res.Finding.Message, filepath.Base(sib)) {
			t.Errorf("httpx.%s: severity %s, message %q; want info naming the sibling", name, res.Finding.Severity, res.Finding.Message)
		}
	}
	// a name the sibling does not export either stays an error
	res := r.Resolve(ref(model.KindGoSymbol, "httpx.TestOnly", model.High, "README.md"))
	if res.Finding == nil || res.Finding.Severity != model.SevError {
		t.Errorf("httpx.TestOnly: %+v, want an error", res)
	}
	// no siblings configured: unchanged behaviour
	res = New(newFake(), Options{}).Resolve(ref(model.KindGoSymbol, "httpx.Response", model.High, "README.md"))
	if res.Finding == nil || res.Finding.Severity != model.SevError {
		t.Errorf("without siblings: %+v, want an error", res)
	}
}
