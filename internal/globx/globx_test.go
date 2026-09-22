package globx

import (
	"errors"
	"path"
	"testing"
)

func TestMatchPath(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{"literal equal", "README.md", "README.md", true},
		{"literal differs", "README.md", "readme.md", false},
		{"literal nested miss", "README.md", "docs/README.md", false},
		{"star in segment", "docs/*.md", "docs/a.md", true},
		{"star does not cross slash", "docs/*.md", "docs/sub/a.md", false},
		{"star at start", "*.md", "a.md", true},
		{"strict star not nested", "*.md", "docs/a.md", false},
		{"question mark", "a?.go", "ab.go", true},
		{"question mark one char only", "a?.go", "abc.go", false},
		{"question mark no slash", "a?b", "a/b", false},
		{"char class", "src/[ab].go", "src/b.go", true},
		{"char class miss", "src/[ab].go", "src/c.go", false},
		{"doublestar deep", "docs/**/*.md", "docs/a/b/c.md", true},
		{"doublestar zero segments", "docs/**/*.md", "docs/c.md", true},
		{"doublestar at root", "**/x", "x", true},
		{"doublestar at root nested", "**/x", "a/b/x", true},
		{"doublestar all md", "**/*.md", "docs/guide/a.md", true},
		{"doublestar all md root", "**/*.md", "a.md", true},
		{"trailing doublestar", "a/**", "a/b/c.txt", true},
		{"trailing doublestar matches dir itself", "a/**", "a", true},
		{"trailing doublestar other dir", "a/**", "b/c.txt", false},
		{"bare doublestar", "**", "any/thing.go", true},
		{"middle doublestar", "a/**/b", "a/b", true},
		{"middle doublestar deep", "a/**/b", "a/x/y/b", true},
		{"middle doublestar miss", "a/**/b", "a/x/y/c", false},
		{"repeated doublestar", "a/**/**/b", "a/x/b", true},
		{"vendor exclude", "vendor/**", "vendor/x/y.go", true},
		{"testdata exclude", "**/testdata/**", "internal/x/testdata/a.json", true},
		{"testdata exclude dir itself", "**/testdata/**", "testdata", true},
		{"leading slash ignored", "/docs/a.md", "docs/a.md", true},
		{"trailing slash stripped", "docs/", "docs", true},
		{"backslash path normalised", "docs/a.md", "docs\\a.md", true},
		{"dot slash prefix stripped", "./docs/a.md", "./docs/a.md", true},
		{"embedded doublestar degrades", "a**b", "axxb", true},
		{"embedded doublestar no cross", "a**b", "a/xb", false},
		{"longer path than pattern", "a/b", "a/b/c", false},
		{"shorter path than pattern", "a/b/c", "a/b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchPath(tt.pattern, tt.path); got != tt.want {
				t.Errorf("MatchPath(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
			p, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tt.pattern, err)
			}
			if got := p.MatchPath(tt.path); got != tt.want {
				t.Errorf("Compile(%q).MatchPath(%q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestMatchConvenience(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{"base name in dir", "*.md", "docs/a.md", true},
		{"base name deep", "*.test", "a/b/c/x.test", true},
		{"base name literal", "llms.txt", "docs/llms.txt", true},
		{"base name literal root", "llms.txt", "llms.txt", true},
		{"base name mismatch", "*.md", "docs/a.txt", false},
		{"pattern with slash stays strict", "docs/*.md", "other/docs/a.md", false},
		{"pattern with slash root ok", "docs/*.md", "docs/a.md", true},
		{"doublestar pattern has slash", "**/*.md", "docs/a.md", true},
		{"question base name", "?.md", "docs/a.md", true},
		{"question base name too long", "?.md", "docs/ab.md", false},
		{"node_modules not base match", "node_modules/**", "x/node_modules/a.js", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Match(tt.pattern, tt.path); got != tt.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestMatchAny(t *testing.T) {
	pats := []string{"vendor/**", "node_modules/**", "**/testdata/**", "*.test"}
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"vendor", "vendor/github.com/x/y.go", true},
		{"testdata deep", "internal/a/testdata/b.json", true},
		{"base name rule", "internal/a/x.test", true},
		{"normal file", "internal/a/x.go", false},
		{"empty list", "anything", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := pats
			if tt.name == "empty list" {
				list = nil
			}
			if got := MatchAny(list, tt.path); got != tt.want {
				t.Errorf("MatchAny(%v, %q) = %v, want %v", list, tt.path, got, tt.want)
			}
		})
	}
}

func TestCompileErrors(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"ok", "docs/**/*.md", false},
		{"ok class", "[a-z]*.go", false},
		{"unterminated class", "[a-z.go", true},
		{"unterminated class in segment", "docs/[ab", true},
		{"empty pattern", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Compile(tt.pattern)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Compile(%q) = %v, want error", tt.pattern, p)
				}
				if !errors.Is(err, path.ErrBadPattern) {
					t.Errorf("error %v does not wrap ErrBadPattern", err)
				}
				if MatchPath(tt.pattern, "anything") || Match(tt.pattern, "anything") {
					t.Errorf("bad pattern %q should never match", tt.pattern)
				}
				return
			}
			if err != nil {
				t.Fatalf("Compile(%q): %v", tt.pattern, err)
			}
			if p.String() != tt.pattern {
				t.Errorf("String() = %q, want %q", p.String(), tt.pattern)
			}
		})
	}
}

func TestCompileAllAndMatchAnyCompiled(t *testing.T) {
	ps, err := CompileAll([]string{"vendor/**", "*.md"})
	if err != nil {
		t.Fatalf("CompileAll: %v", err)
	}
	if len(ps) != 2 {
		t.Fatalf("len = %d, want 2", len(ps))
	}
	if !MatchAnyCompiled(ps, "docs/a.md") {
		t.Error("expected docs/a.md to match *.md")
	}
	if MatchAnyCompiled(ps, "internal/a.go") {
		t.Error("did not expect internal/a.go to match")
	}
	if _, err := CompileAll([]string{"ok", "[bad"}); err == nil {
		t.Error("CompileAll: want error for bad pattern")
	}
	if ps, err := CompileAll(nil); ps != nil || err != nil {
		t.Errorf("CompileAll(nil) = %v, %v", ps, err)
	}
}

func TestMustCompilePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustCompile: want panic")
		}
	}()
	MustCompile("[bad")
}

func TestClean(t *testing.T) {
	tests := []struct{ in, want string }{
		{"docs/a.md", "docs/a.md"},
		{"./docs/a.md", "docs/a.md"},
		{"././docs", "docs"},
		{"/docs/a.md", "docs/a.md"},
		{"docs\\a.md", "docs/a.md"},
		{"docs//a.md", "docs/a.md"},
		{"docs/", "docs"},
		{".", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := Clean(tt.in); got != tt.want {
				t.Errorf("Clean(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHasMeta(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"docs/a.md", false},
		{"docs/*.md", true},
		{"a?.go", true},
		{"[ab].go", true},
		{"**", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := HasMeta(tt.in); got != tt.want {
				t.Errorf("HasMeta(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestNilPattern(t *testing.T) {
	var p *Pattern
	if p.Match("a") || p.MatchPath("a") {
		t.Error("nil pattern must not match")
	}
}
