package comments

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"docrot/internal/model"
)

// TestMentionsKnowSiblingsMethodsAndNouns: names that live in a sibling
// repository, methods named the short way (`natsx.Request`) and proper
// nouns with an inner capital (gRPC) are not "mentions of nothing".
func TestMentionsKnowSiblingsMethodsAndNouns(t *testing.T) {
	sib := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sib, "errx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sib, "errx", "code.go"), []byte("package errx\n\nconst CodeNotFound Code = \"NOT_FOUND\"\n\nfunc isValidRequestID(s string) bool { return true }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// vendored code is not scanned
	if err := os.MkdirAll(filepath.Join(sib, "vendor", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sib, "vendor", "x", "x.go"), []byte("package x\n\nconst SQLITE_LOCKED = 6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	words := WordsOf([]string{sib})
	for _, w := range []string{"NOT_FOUND", "isValidRequestID", "CodeNotFound"} {
		if !words[w] {
			t.Errorf("WordsOf lacks %q", w)
		}
	}
	if words["SQLITE_LOCKED"] {
		t.Error("WordsOf must skip vendor/")
	}

	src := "package x\n\n// Request maps NOT_FOUND like natsx.Request does, over gRPC, using isValidRequestID;\n// SQLITE_LOCKED and frobnicateThing are unknown.\nfunc Request() {}\n"
	sp := model.SymbolSpan{
		Qualified: "x.Request", Kind: model.KindGoSymbol, File: "x.go",
		DocStart: 3, DocEnd: 4, DeclLine: 5, BodyStart: 5, BodyEnd: 5,
		Doc: []string{
			"Request maps NOT_FOUND like natsx.Request does, over gRPC, using isValidRequestID; Envelope.content_type too;",
			"SQLITE_LOCKED and frobnicateThing are unknown.",
		},
	}
	ix := fakeLookup{syms: map[string]bool{"pkg:natsx": true, "member:Request": true, "Envelope": true, "lit:content_type": true}}
	got := mentions(sp, strings.Split(src, "\n"), ix, Options{MentionSeverity: model.SevWarning, ExternalWords: words})
	var names []string
	for _, f := range got {
		names = append(names, f.Data["mention"].(string))
	}
	if want := "SQLITE_LOCKED,frobnicateThing"; strings.Join(names, ",") != want {
		t.Fatalf("mentions = %v, want %s", names, want)
	}
	if WordsOf(nil) != nil {
		t.Error("WordsOf(nil) must be nil")
	}
}
