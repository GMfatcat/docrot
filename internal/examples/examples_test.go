package examples

import (
	"strings"
	"testing"

	"docrot/internal/markdown"
	"docrot/internal/model"
)

func docOf(blocks ...string) *markdown.Doc {
	var b strings.Builder
	b.WriteString("# T\n\n")
	for _, blk := range blocks {
		b.WriteString(blk)
		b.WriteString("\n\n")
	}
	return markdown.Parse("README.md", []byte(b.String()))
}

func fence(info string, lines ...string) string {
	return "```" + info + "\n" + strings.Join(lines, "\n") + "\n```"
}

func TestGoShapesParse(t *testing.T) {
	good := []string{
		fence("go", "package main", "", "import \"fmt\"", "", "func main() { fmt.Println(1) }"),
		fence("go", "func (s *Server) Start() error"),                          // signature only
		fence("go", "type Config struct {", "\tAddr string", "}"),              // decl without package
		fence("go", "import \"example.com/x\"", "", "s := x.New()", "s.Run()"), // statements after an import
		fence("go", "x.Do(ctx)"),                                          // one expression
		fence("go", "Addr: \":8080\",", "Timeout: 5 * time.Second,"),      // literal elements
		fence("go", "Name string", "Age  int `json:\"age\"`"),             // struct fields
		fence("go", "Start() error", "Stop(ctx context.Context) error"),   // interface methods
		fence("go", "//go:generate stringer -type=Kind", "type Kind int"), // directive
		fence("golang", "return fmt.Errorf(\"x: %w\", err)"),              // return outside a func body
		fence("go", "if err != nil {", "\treturn err", "}"),
		fence("go", "func main() {", "\t// ...", "}"),        // elided
		fence("go", "cfg := Config{", "\tAddr: \"…\",", "}"), // elided (unicode)
		fence("go", "{{.Name}} is templated"),                // template
		fence("go ignore", "this is not go"),                 // skip word
		fence("go,no-check", "this is not go"),               // skip word, comma
		fence("go {pseudo}", "this is not go"),               // skip word, braces
		fence("go", ""),                                      // empty
		fence("go", "import (", "\t\"fmt\"", "\t\"os\"", ")", "", "fmt.Println(os.Args)"),
		fence("go", "case CodeUnknown:", "\treturn 500", "case CodeAborted:", "\treturn 409"),           // switch cases
		fence("go", "var cfg Config", "if err != nil {", "\treturn err", "}", "", "func h(w W) {", "}"), // statements, then a declaration
		fence("go", "module example.com/x", "", "go 1.22", "", "require example.com/y v1.0.0"),          // go.mod content
	}
	for i, blk := range good {
		if got := Go(docOf(blk), Options{}); len(got) != 0 {
			t.Errorf("block %d should parse, got %s\n%s", i, got[0].Message, blk)
		}
	}
}

func TestGoFindings(t *testing.T) {
	doc := docOf(
		fence("go", "func main() {", "\tfmt.Println(\"hi\"", "}"),    // missing paren, line 2 of the block
		fence("go", "package main", "", "func main() {", "\tx := 1"), // unclosed: reported on the last line
		fence("go", "$ go run ./cmd/app"),                            // a shell line in a go fence
		fence("go", "type T struct {", "\tA int", "\tB", "}"),        // fine: B is an embedded field
	)
	got := Go(doc, Options{})
	if len(got) != 3 {
		for _, f := range got {
			t.Logf("%s: %s", f.Loc, f.Message)
		}
		t.Fatalf("got %d findings, want 3", len(got))
	}
	// block 1 starts at line 3 (title, blank, fence); its second line is doc line 5
	if f := got[0]; f.Loc.Line != 5 || f.Loc.Col == 0 || !strings.Contains(f.Message, "does not parse") || f.Severity != model.SevWarning || f.Rule != model.RuleExampleSyntax {
		t.Errorf("first finding = %+v", f)
	}
	// block 2 starts at line 9; its last line (4th) is doc line 13, and an
	// unclosed block is info: as often an excerpt as a mistake
	if f := got[1]; f.Loc.Line != 13 || f.Severity != model.SevInfo || !strings.Contains(f.Message, "ends before its braces close") {
		t.Errorf("unclosed block = %d %s %q, want line 13, info", f.Loc.Line, f.Severity, f.Message)
	}
	if f := got[2]; !strings.Contains(f.Message, "does not parse") || f.Severity != model.SevWarning {
		t.Errorf("shell line finding = %+v", f)
	}
	if got[0].Fingerprint == got[1].Fingerprint || got[0].Fingerprint == "" {
		t.Error("fingerprints should differ per block")
	}
	// severity override
	if g := Go(doc, Options{Severity: model.SevInfo}); g[0].Severity != model.SevInfo {
		t.Errorf("severity override ignored: %s", g[0].Severity)
	}
}

func TestGoRespectsIgnore(t *testing.T) {
	md := "# T\n\n<!-- docrot:ignore -->\n```go\nfunc main() {\n```\n"
	if got := Go(markdown.Parse("README.md", []byte(md)), Options{}); len(got) != 0 {
		t.Errorf("ignored fence reported: %+v", got)
	}
	md = "<!-- docrot:ignore-file -->\n```go\nfunc main() {\n```\n"
	if got := Go(markdown.Parse("README.md", []byte(md)), Options{}); len(got) != 0 {
		t.Errorf("ignore-file fence reported: %+v", got)
	}
}
