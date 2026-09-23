package rust

import (
	"regexp"
	"strings"
)

// decl is one declaration as parseFile sees it: qualified within the file.
type decl struct {
	mods     string // inline module path inside the file ("" or "a::b")
	path     string // Item, Type::method
	name     string
	kind     string
	src      string // for a `use` item: the path it was imported from
	file     string
	line     int
	exported bool
	span     *rustSpan
}

// useItem is one name bound by a `pub use` statement and its source path.
type useItem struct{ name, src string }

// rustSpan is the comment and body geometry of one declaration. Line
// numbers are 1-based; zero means "none".
type rustSpan struct {
	docStart int
	docEnd   int
	declLine int
	bodyEnd  int
	doc      []string
	params   []string
}

// scope is an open brace block that names the items inside it.
type scope struct {
	kind     string // mod, impl, trait, enum, fn, other
	name     string
	depth    int  // brace depth at which the block opened
	trait    bool // impl Trait for Type: methods are as public as the trait
	exported bool // an enum's variants are as public as the enum
}

// glob is a `pub use target::*;` re-export: everything of target is also a
// name of the module (mods, relative to the file's module) that wrote it.
type glob struct {
	mods   string
	target string
}

var (
	// visibility and qualifiers in front of an item keyword
	reQualifiers = regexp.MustCompile(`^(?:pub(?:\([^)]*\))?\s+)?(?:default\s+)?(?:async\s+)?(?:unsafe\s+)?(?:extern\s+(?:"[^"]*"\s+)?)?`)
	reItem       = regexp.MustCompile(`^(?:const\s+)?(fn|struct|enum|union|trait|type|const|static|mod|macro_rules!)\s+(?:mut\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
	// pub use a::b::{C, D as E}; pub use a::b::C;
	reUse = regexp.MustCompile(`^pub(?:\([^)]*\))?\s+use\s+(.*)$`)
	// an enum variant: Name, Name(..), Name { .. }, Name = 1
	reVariant = regexp.MustCompile(`^([A-Z][A-Za-z0-9_]*)\s*(?:[,({=]|$)`)
)

// parseFile scans one file and returns every recognised declaration.
// masked is maskAll(lines): the same lines with strings, chars and comments
// blanked, so that braces and keywords inside them do not count.
func parseFile(lines, masked []string, rel string) ([]decl, []glob) {
	var out []decl
	var globs []glob
	var scopes []scope
	var pending *scope
	depth, paren := 0, 0
	var doc []string
	docStart, docEnd := 0, 0
	inAttr := 0  // bracket depth of an attribute spanning lines
	useBuf := "" // a `pub use` list spanning lines, until its ";"
	useLine := 0 // 0-based line the list started on

	resetDoc := func() { doc, docStart, docEnd = nil, 0, 0 }
	modPath := func() string {
		var segs []string
		for _, s := range scopes {
			if s.kind == "mod" {
				segs = append(segs, s.name)
			}
		}
		return strings.Join(segs, "::")
	}
	// owner is the impl or trait an item at this depth belongs to.
	owner := func() *scope {
		if n := len(scopes); n > 0 && depth == scopes[n-1].depth+1 && (scopes[n-1].kind == "impl" || scopes[n-1].kind == "trait") {
			return &scopes[n-1]
		}
		return nil
	}
	// placed reports whether an item at the current depth is a real
	// declaration (top level, or directly inside a mod/impl/trait block)
	// rather than a local inside a function body.
	placed := func() bool {
		if depth == 0 {
			return true
		}
		n := len(scopes)
		return n > 0 && depth == scopes[n-1].depth+1 && scopes[n-1].kind != "fn" && scopes[n-1].kind != "other"
	}

	for i, raw := range lines {
		rawT := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		m := masked[i]
		t := strings.TrimSpace(m)

		if useBuf != "" {
			useBuf += " " + rawT
			if strings.Contains(m, ";") {
				names, gl := useNames(useBuf)
				for _, u := range names {
					out = append(out, decl{mods: modPath(), path: u.name, name: u.name, kind: "use", src: u.src, file: rel, line: useLine + 1, exported: true})
				}
				for _, g := range gl {
					globs = append(globs, glob{mods: modPath(), target: g})
				}
				useBuf = ""
			}
			continue
		}
		switch {
		case inAttr > 0:
			inAttr += strings.Count(m, "[") - strings.Count(m, "]")
			continue
		case strings.HasPrefix(rawT, "///") && !strings.HasPrefix(rawT, "////"):
			if docStart == 0 {
				docStart = i + 1
			}
			docEnd = i + 1
			doc = append(doc, strings.TrimPrefix(strings.TrimPrefix(rawT, "///"), " "))
			continue
		case strings.HasPrefix(rawT, "//"):
			continue
		case strings.HasPrefix(rawT, "#[") || strings.HasPrefix(rawT, "#!["):
			inAttr = strings.Count(m, "[") - strings.Count(m, "]")
			continue
		case t == "":
			continue
		}

		if reUse.MatchString(t) && placed() {
			if !strings.Contains(m, ";") {
				useBuf, useLine = rawT, i
				continue
			}
			names, gl := useNames(rawT)
			for _, u := range names {
				out = append(out, decl{mods: modPath(), path: u.name, name: u.name, kind: "use", src: u.src, file: rel, line: i + 1, exported: true})
			}
			for _, g := range gl {
				globs = append(globs, glob{mods: modPath(), target: g})
			}
			resetDoc()
			continue
		}
		if n := len(scopes); n > 0 && scopes[n-1].kind == "enum" && depth == scopes[n-1].depth+1 && pending == nil {
			if vm := reVariant.FindStringSubmatch(t); vm != nil {
				e := scopes[n-1]
				out = append(out, decl{mods: modPath(), path: e.name + "::" + vm[1], name: vm[1], kind: "variant", file: rel, line: i + 1, exported: e.exported})
			}
		}
		if pending == nil {
			if d := declOf(t, rawT, i, lines, masked, rel); d != nil {
				if placed() {
					if o := owner(); o != nil && d.kind == "fn" {
						d.path = o.name + "::" + d.name
						d.exported = d.exported || o.trait || o.kind == "trait"
					}
					d.mods = modPath()
					d.span.doc = trimDoc(doc)
					if len(d.span.doc) > 0 {
						d.span.docStart, d.span.docEnd = docStart, docEnd
					}
					if d.kind == "mod" {
						pending = &scope{kind: "mod", name: d.name}
					} else {
						out = append(out, *d)
						switch d.kind {
						case "trait":
							pending = &scope{kind: "trait", name: d.name}
						case "enum":
							pending = &scope{kind: "enum", name: d.name, exported: d.exported}
							for _, v := range inlineVariants(t) {
								out = append(out, decl{mods: modPath(), path: d.name + "::" + v, name: v, kind: "variant", file: rel, line: i + 1, exported: d.exported})
							}
						case "fn", "struct", "union":
							pending = &scope{kind: "fn", name: d.name} // a body no item may live in
						}
					}
				} else if d.kind == "fn" || d.kind == "mod" {
					pending = &scope{kind: "fn", name: d.name}
				}
			} else if typ, hasTrait, ok := implHeader(t); ok && placed() {
				pending = &scope{kind: "impl", name: typ, trait: hasTrait}
			}
		}
		resetDoc()

		// brace accounting; a pending scope opens on the first "{" and is
		// dropped by a ";" outside parentheses ("fn f(&self);" in a trait)
		for j := 0; j < len(m); j++ {
			switch m[j] {
			case '(', '[':
				paren++
			case ')', ']':
				if paren > 0 {
					paren--
				}
			case '{':
				if pending != nil {
					pending.depth = depth
					scopes = append(scopes, *pending)
					pending = nil
				}
				depth++
			case '}':
				if depth > 0 {
					depth--
				}
				for n := len(scopes); n > 0 && scopes[n-1].depth >= depth; n = len(scopes) {
					scopes = scopes[:n-1]
				}
			case ';':
				if pending != nil && paren == 0 {
					pending = nil
				}
			}
		}
	}
	return out, globs
}

// declOf recognises an item declaration on a masked line.
func declOf(t, rawT string, i int, lines, masked []string, rel string) *decl {
	rest := t[len(reQualifiers.FindString(t)):]
	m := reItem.FindStringSubmatch(rest)
	if m == nil {
		return nil
	}
	kind, name := m[1], m[2]
	if kind == "macro_rules!" {
		kind = "macro"
	}
	if kind == "mod" && strings.HasSuffix(t, ";") {
		return nil // "mod x;": the module file is indexed on its own
	}
	d := &decl{path: name, name: name, kind: kind, file: rel, line: i + 1, exported: strings.HasPrefix(rawT, "pub ") || kind == "macro"}
	d.span = &rustSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i)}
	if kind == "fn" {
		d.span.params = fnParams(lines, masked, i)
	}
	return d
}

// inlineVariants returns the variants of an enum whose whole body sits on
// the declaration line: "pub enum Mode { Fast, Slow(u8), Custom { n: u8 } }".
func inlineVariants(t string) []string {
	open := strings.IndexByte(t, '{')
	if open < 0 || !strings.HasSuffix(strings.TrimSpace(t), "}") {
		return nil
	}
	body := t[open+1 : strings.LastIndexByte(t, '}')]
	var out []string
	depth, start := 0, 0
	emit := func(seg string) {
		if m := reVariant.FindStringSubmatch(strings.TrimSpace(seg)); m != nil {
			out = append(out, m[1])
		}
	}
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			depth--
		case ',':
			if depth == 0 {
				emit(body[start:i])
				start = i + 1
			}
		}
	}
	emit(body[start:])
	return out
}

// useNames returns the names a `pub use` statement binds (the alias when
// present, otherwise the last path segment) with their source paths, and
// the paths it re-exports wholesale (`pub use path::*`). Nested trees
// (`self::{path::{Path, RawPathParams}, state::State}`) are walked; `self`
// and `_` bind nothing.
func useNames(stmt string) (names []useItem, globs []string) {
	m := reUse.FindStringSubmatch(strings.TrimSpace(stmt))
	if m == nil {
		return nil, nil
	}
	spec := strings.TrimSpace(m[1])
	if i := strings.Index(spec, ";"); i >= 0 {
		spec = spec[:i]
	}
	if i := strings.Index(spec, "//"); i >= 0 {
		spec = spec[:i]
	}
	walkUseTree("", spec, &names, &globs)
	return names, globs
}

// walkUseTree adds the items of one use-tree node under prefix.
func walkUseTree(prefix, spec string, names *[]useItem, globs *[]string) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return
	}
	if i := strings.IndexByte(spec, '{'); i >= 0 {
		inner := spec[i+1:]
		if j := strings.LastIndexByte(inner, '}'); j >= 0 {
			inner = inner[:j]
		}
		sub := prefix + strings.TrimSpace(spec[:i])
		for _, item := range splitTopLevel(inner) {
			walkUseTree(sub, item, names, globs)
		}
		return
	}
	if spec == "*" || strings.HasSuffix(spec, "::*") {
		if target := strings.TrimSuffix(prefix+strings.TrimSuffix(spec, "*"), "::"); target != "" {
			*globs = append(*globs, target)
		}
		return
	}
	src, name := prefix+spec, spec
	if k := strings.Index(spec, " as "); k >= 0 {
		src, name = prefix+strings.TrimSpace(spec[:k]), strings.TrimSpace(spec[k+4:])
	} else if k := strings.LastIndex(spec, "::"); k >= 0 {
		name = spec[k+2:]
	}
	if name == "self" || name == "_" || !isIdent(name) {
		return
	}
	*names = append(*names, useItem{name: name, src: src})
}

// splitTopLevel splits a use-tree list on the commas outside braces.
func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// implHeader reads "impl<...> [Trait for] Type<...>" and returns the type
// name (the last path segment) and whether a trait is implemented.
// Generic lists may contain "->" and nested angle brackets.
func implHeader(t string) (typ string, hasTrait, ok bool) {
	rest := strings.TrimPrefix(t, "unsafe ")
	if !strings.HasPrefix(rest, "impl") {
		return "", false, false
	}
	rest = rest[len("impl"):]
	if rest == "" || !(rest[0] == '<' || rest[0] == ' ' || rest[0] == '\t') {
		return "", false, false
	}
	rest = strings.TrimSpace(skipAngles(rest))
	// split on " for " at angle depth 0
	depth := 0
	for i := 0; i+5 <= len(rest); i++ {
		switch rest[i] {
		case '<':
			depth++
		case '>':
			if i > 0 && rest[i-1] == '-' {
				continue
			}
			depth--
		}
		if depth == 0 && strings.HasPrefix(rest[i:], " for ") {
			hasTrait = true
			rest = rest[i+5:]
			break
		}
	}
	rest = strings.TrimSpace(rest)
	rest = strings.TrimLeft(rest, "&")
	if strings.HasPrefix(rest, "'") { // &'a Type
		if i := strings.IndexAny(rest, " \t"); i >= 0 {
			rest = strings.TrimSpace(rest[i:])
		}
	}
	rest = strings.TrimPrefix(rest, "mut ")
	rest = strings.TrimPrefix(rest, "dyn ")
	end := len(rest)
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if !(c == '_' || c == ':' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			end = i
			break
		}
	}
	path := strings.TrimSuffix(rest[:end], "::")
	if i := strings.LastIndex(path, "::"); i >= 0 {
		path = path[i+2:]
	}
	if !isIdent(path) {
		return "", false, false
	}
	return path, hasTrait, true
}

// skipAngles removes a leading balanced <...> generic list; "->" inside
// it does not close a bracket.
func skipAngles(s string) string {
	if !strings.HasPrefix(s, "<") {
		return s
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			if i > 0 && s[i-1] == '-' {
				continue
			}
			depth--
			if depth == 0 {
				return s[i+1:]
			}
		}
	}
	return ""
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

func trimDoc(doc []string) []string {
	out := append([]string(nil), doc...)
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// bodyEndLine returns the 1-based line closing the declaration's braces. A
// declaration ended by ";" before any "{" (a trait method signature, a
// type alias, a constant) ends on that line.
func bodyEndLine(masked []string, start int) int {
	depth := 0
	opened := false
	last := start + 5000
	if last > len(masked) {
		last = len(masked)
	}
	for j := start; j < last; j++ {
		code := masked[j]
		for i := 0; i < len(code); i++ {
			switch code[i] {
			case '{':
				depth++
				opened = true
			case '}':
				if depth > 0 {
					depth--
				}
			case ';':
				if !opened {
					return j + 1
				}
			}
		}
		if opened && depth == 0 {
			return j + 1
		}
	}
	return start + 1
}

// fnParams returns the parameter names of the fn declared on lines[start]:
// "self" for any receiver form, the identifier before ":" otherwise.
func fnParams(lines, masked []string, start int) []string {
	last := start + 200
	if last > len(lines) {
		last = len(lines)
	}
	var raw, msk strings.Builder
	for j := start; j < last; j++ {
		if j > start {
			raw.WriteByte('\n')
			msk.WriteByte('\n')
		}
		raw.WriteString(strings.TrimRight(lines[j], "\r"))
		msk.WriteString(masked[j])
	}
	r, m := raw.String(), msk.String()
	open := strings.IndexByte(m, '(')
	if open < 0 {
		return nil
	}
	depth, closed := 0, -1
	for i := open; i < len(m) && closed < 0; i++ {
		switch m[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				closed = i
			}
		}
	}
	if closed < 0 || closed > len(r) {
		return nil
	}
	var out []string
	segStart := open + 1
	depth = 0
	emit := func(seg string) {
		s := strings.TrimSpace(strings.ReplaceAll(seg, "\n", " "))
		if s == "" {
			return
		}
		if strings.Contains(s, "self") && !strings.Contains(s, ":") || strings.HasPrefix(s, "self:") || strings.HasPrefix(s, "mut self") {
			out = append(out, "self")
			return
		}
		if i := strings.IndexByte(s, ':'); i > 0 {
			name := strings.TrimSpace(s[:i])
			name = strings.TrimPrefix(name, "mut ")
			name = strings.TrimPrefix(name, "ref ")
			if isIdent(name) {
				out = append(out, name)
			}
		}
	}
	for i := open + 1; i < closed; i++ {
		switch m[i] {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			depth--
		case ',':
			if depth == 0 {
				emit(r[segStart:i])
				segStart = i + 1
			}
		}
	}
	emit(r[segStart:closed])
	return out
}

// maskAll blanks string literals, char literals and comments in every line
// (block comments across lines included). Each result has the length of
// its input line; lifetimes ('a) are left alone.
func maskAll(lines []string) []string {
	out := make([]string, len(lines))
	inBlock := 0
	for i, ln := range lines {
		out[i], inBlock = maskLine(strings.TrimRight(ln, "\r"), inBlock)
	}
	return out
}

func maskLine(ln string, inBlock int) (string, int) {
	out := []byte(ln)
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inBlock > 0 {
			if c == '/' && i+1 < len(out) && out[i+1] == '*' {
				inBlock++
				out[i], out[i+1] = ' ', ' '
				i++
				continue
			}
			if c == '*' && i+1 < len(out) && out[i+1] == '/' {
				inBlock--
				out[i], out[i+1] = ' ', ' '
				i++
				continue
			}
			out[i] = ' '
			continue
		}
		switch c {
		case '/':
			if i+1 < len(out) && out[i+1] == '/' {
				for j := i; j < len(out); j++ {
					out[j] = ' '
				}
				return string(out), inBlock
			}
			if i+1 < len(out) && out[i+1] == '*' {
				inBlock++
				out[i], out[i+1] = ' ', ' '
				i++
			}
		case '"':
			// a raw string r"…" / r#"…"# or a plain one; both end at the
			// next unescaped quote on this line for our purposes
			hashes := 0
			for k := i - 1; k >= 0 && out[k] == '#'; k-- {
				hashes++
			}
			raw := i-hashes-1 >= 0 && (out[i-hashes-1] == 'r' || out[i-hashes-1] == 'b')
			j := i + 1
			for j < len(out) {
				if !raw && out[j] == '\\' {
					j += 2
					continue
				}
				if out[j] == '"' {
					if !raw || hashes == 0 || strings.HasPrefix(string(out[j+1:]), strings.Repeat("#", hashes)) {
						break
					}
				}
				j++
			}
			for k := i + 1; k < j && k < len(out); k++ {
				out[k] = ' '
			}
			if j < len(out) {
				i = j
			} else {
				i = len(out)
			}
		case '\'':
			// a char literal 'x' or '\n'; otherwise a lifetime, left alone
			if i+2 < len(out) && out[i+2] == '\'' {
				out[i+1] = ' '
				i += 2
			} else if i+3 < len(out) && out[i+1] == '\\' && out[i+3] == '\'' {
				out[i+1], out[i+2] = ' ', ' '
				i += 3
			}
		}
	}
	return string(out), inBlock
}

// stripLifetimes removes 'a-style lifetimes so that the literal scanner
// does not mistake them for the start of a quoted string.
func stripLifetimes(ln string) string {
	if !strings.Contains(ln, "'") {
		return ln
	}
	b := []byte(ln)
	for i := 0; i+1 < len(b); i++ {
		if b[i] != '\'' {
			continue
		}
		if i+2 < len(b) && b[i+2] == '\'' || i+3 < len(b) && b[i+1] == '\\' && b[i+3] == '\'' {
			continue // a char literal
		}
		j := i + 1
		for j < len(b) && (b[j] == '_' || b[j] >= 'a' && b[j] <= 'z' || b[j] >= 'A' && b[j] <= 'Z' || b[j] >= '0' && b[j] <= '9') {
			j++
		}
		if j > i+1 && (j >= len(b) || b[j] != '\'') {
			for k := i; k < j; k++ {
				b[k] = ' '
			}
		}
	}
	return string(b)
}
