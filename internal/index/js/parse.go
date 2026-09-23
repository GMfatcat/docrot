package js

import (
	"regexp"
	"strings"
)

// decl is one declaration as parseFile sees it, qualified within the file.
type decl struct {
	path     string // fetchAll, Client.fetchAll, Mode.Fast, ns.helper
	name     string
	kind     string
	file     string
	line     int
	exported bool
	private  bool // a #name or `private` member: never exported through its owner
	span     *jsSpan
}

// jsSpan is the comment and body geometry of one declaration. Line
// numbers are 1-based; zero means "none".
type jsSpan struct {
	docStart int
	docEnd   int
	declLine int
	bodyEnd  int
	doc      []string
	params   []string
}

// scope is an open brace block that names the members inside it.
type scope struct {
	kind     string // class, object, enum, namespace, function, other
	name     string
	depth    int
	exported bool
}

var (
	ident = `[A-Za-z_$][A-Za-z0-9_$]*`
	// [export] [default] [declare] [abstract] [async] function|class|... NAME
	reDecl = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:declare\s+)?(?:abstract\s+)?(?:async\s+)?(function\s*\*?|class|interface|type|enum|namespace|module|const|let|var)\s+(` + ident + `)`)
	// export { A, B as C } [from '...'];  export * from '...'
	reExportList = regexp.MustCompile(`^export\s*(?:type\s+)?\{([^}]*)\}(?:\s*from\s*['"]([^'"]+)['"])?`)
	reExportStar = regexp.MustCompile(`^export\s*\*(?:\s*as\s+(` + ident + `))?\s*from\s*['"]([^'"]+)['"]`)
	reExportDef  = regexp.MustCompile(`^export\s+default\s+(` + ident + `)\s*;?\s*$`)
	// module.exports = { A, B: C };  module.exports = NAME;  exports.NAME = / module.exports.NAME =
	reModExports    = regexp.MustCompile(`^module\.exports\s*=\s*(\{[^}]*\}|` + ident + `)`)
	reExportsAssign = regexp.MustCompile(`^(?:module\.)?exports\.(` + ident + `)\s*=`)
	// NAME.prototype.method = ...;  NAME.method = ...
	reProto  = regexp.MustCompile(`^(` + ident + `)\.prototype\.(` + ident + `)\s*=`)
	reAssign = regexp.MustCompile(`^(` + ident + `)\.(` + ident + `)\s*=[^=]`)
	// Object.defineProperties(NAME.prototype, {   /  Object.defineProperty(NAME.prototype, 'name', ...)
	reDefProps = regexp.MustCompile(`^Object\.defineProperties\(\s*(` + ident + `)(?:\.prototype)?\s*,\s*\{\s*$`)
	reDefProp  = regexp.MustCompile(`^Object\.defineProperty\(\s*(` + ident + `)(?:\.prototype)?\s*,\s*['"](` + ident + `)['"]`)
	// a class member: [modifiers] [get|set] [#]name(   or   [modifiers] [#]name[?!]: / =
	reMethod = regexp.MustCompile(`^(?:(?:public|private|protected|static|async|readonly|override|abstract|declare|accessor)\s+)*(?:(?:get|set)\s+)?(#?` + ident + `)\s*(?:<[^>]*>)?\s*\(`)
	reField  = regexp.MustCompile(`^(?:(?:public|private|protected|static|readonly|override|declare|accessor)\s+)*(#?` + ident + `)\s*[?!]?\s*[:=]`)
	// an object-literal member: key: ... / method() { / async method() { / shorthand,
	reObjMember = regexp.MustCompile(`^(?:async\s+)?(?:(?:get|set)\s+)?(` + ident + `)\s*(?:\(|:|,\s*$|$)`)
	// const NAME = {   (an object literal spanning lines)
	reObjOpen = regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(` + ident + `)\s*(?::[^=]+)?=\s*\{\s*$`)
	// an enum member: Name, Name = 1, Name = "x"
	reEnumMember = regexp.MustCompile(`^(` + ident + `)\s*(?:[,=]|$)`)
	reDecorator  = regexp.MustCompile(`^@` + ident)
	// the value of a key is an arrow function: (a, b) => … / a => …
	reArrowValue = regexp.MustCompile(`^(?:\([^)]*\)|` + ident + `)\s*=>`)
)

// keywords that reMethod/reObjMember must never take for a name
var notMember = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "return": true, "catch": true, "else": true, "try": true,
	"do": true, "constructor": true, "function": true, "new": true, "throw": true, "await": true, "typeof": true,
	"yield": true, "super": true, "this": true, "import": true, "export": true, "default": true, "case": true,
}

// parseFile scans one file and returns every recognised declaration and
// the `export * from` targets. masked is maskAll(lines).
func parseFile(lines, masked []string, rel string) ([]decl, []string) {
	var out []decl
	var globs []string
	var scopes []scope
	var pending *scope
	depth, paren := 0, 0
	declared := map[string]bool{} // top-level names of this file, for X.y = assignments
	exportedLater := map[string]bool{}

	modPath := func() string {
		var segs []string
		for _, s := range scopes {
			if s.kind == "namespace" {
				segs = append(segs, s.name)
			}
		}
		return strings.Join(segs, ".")
	}
	top := func() *scope {
		if n := len(scopes); n > 0 && depth == scopes[n-1].depth+1 {
			return &scopes[n-1]
		}
		return nil
	}
	placed := func() bool {
		if depth == 0 {
			return true
		}
		s := top()
		return s != nil && s.kind == "namespace"
	}
	withPath := func(p string) string {
		if mp := modPath(); mp != "" {
			return mp + "." + p
		}
		return p
	}
	add := func(d decl) {
		d.path = withPath(d.path)
		d.file = rel
		out = append(out, d)
	}

	for i, raw := range lines {
		rawT := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		m := masked[i]
		t := strings.TrimSpace(m)
		if t == "" || strings.HasPrefix(rawT, "//") || strings.HasPrefix(rawT, "/*") || strings.HasPrefix(rawT, "*") {
			if t == "" || !strings.ContainsAny(m, "{}") {
				continue
			}
		}

		if pending == nil {
			if s := top(); s != nil && !strings.HasPrefix(t, "}") {
				switch s.kind {
				case "class":
					if mm := reMethod.FindStringSubmatch(t); mm != nil && !notMember[mm[1]] {
						private := strings.HasPrefix(mm[1], "#") || strings.HasPrefix(t, "private ")
						d := decl{path: s.name + "." + strings.TrimPrefix(mm[1], "#"), name: strings.TrimPrefix(mm[1], "#"), kind: "method", line: i + 1, exported: s.exported && !private, private: private}
						d.span = &jsSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i), params: params(lines, masked, i)}
						d.span.docStart, d.span.docEnd, d.span.doc = docAbove(lines, i)
						add(d)
						pending = &scope{kind: "function", name: d.name}
					} else if mm := reField.FindStringSubmatch(t); mm != nil && !notMember[mm[1]] {
						private := strings.HasPrefix(mm[1], "#") || strings.HasPrefix(t, "private ")
						add(decl{path: s.name + "." + strings.TrimPrefix(mm[1], "#"), name: strings.TrimPrefix(mm[1], "#"), kind: "field", line: i + 1, exported: s.exported && !private, private: private})
					}
				case "object":
					if mm := reObjMember.FindStringSubmatch(t); mm != nil && !notMember[mm[1]] {
						d := decl{path: s.name + "." + mm[1], name: mm[1], kind: "member", line: i + 1, exported: s.exported}
						rest := strings.TrimSpace(t[len(mm[0]):])
						if strings.HasSuffix(strings.TrimSpace(mm[0]), "(") || strings.HasPrefix(rest, "function") || strings.HasPrefix(rest, "async") || reArrowValue.MatchString(rest) {
							// method() {…} or key: function () {…}: a body of its own; `key: fn(x)` is a value
							d.span = &jsSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i), params: params(lines, masked, i)}
							d.span.docStart, d.span.docEnd, d.span.doc = docAbove(lines, i)
							if strings.Contains(m, "{") {
								pending = &scope{kind: "function", name: d.name}
							}
						}
						add(d)
					}
				case "enum":
					if mm := reEnumMember.FindStringSubmatch(t); mm != nil {
						add(decl{path: s.name + "." + mm[1], name: mm[1], kind: "member", line: i + 1, exported: s.exported})
					}
				}
			}
		}

		if pending == nil && placed() {
			switch {
			case reExportStar.MatchString(t):
				mm := reExportStar.FindStringSubmatch(rawT)
				if mm != nil {
					if mm[1] != "" {
						add(decl{path: mm[1], name: mm[1], kind: "export", line: i + 1, exported: true})
					} else {
						globs = append(globs, mm[2])
					}
				}
			case reExportList.MatchString(t):
				mm := reExportList.FindStringSubmatch(rawT)
				for _, name := range exportNames(mm[1]) {
					if mm[2] != "" || !declared[name] {
						add(decl{path: name, name: name, kind: "export", line: i + 1, exported: true})
					} else {
						exportedLater[name] = true
					}
				}
			case reExportDef.MatchString(t):
				exportedLater[reExportDef.FindStringSubmatch(t)[1]] = true
			case reModExports.MatchString(t):
				mm := reModExports.FindStringSubmatch(rawT)
				if strings.HasPrefix(mm[1], "{") {
					for _, name := range exportNames(strings.Trim(mm[1], "{}")) {
						if !declared[name] {
							add(decl{path: name, name: name, kind: "export", line: i + 1, exported: true})
						} else {
							exportedLater[name] = true
						}
					}
				} else {
					exportedLater[mm[1]] = true
				}
			case reExportsAssign.MatchString(t):
				mm := reExportsAssign.FindStringSubmatch(t)
				d := decl{path: mm[1], name: mm[1], kind: "export", line: i + 1, exported: true}
				d.span = &jsSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i)}
				d.span.docStart, d.span.docEnd, d.span.doc = docAbove(lines, i)
				add(d)
				if strings.Contains(m, "{") {
					pending = &scope{kind: "function", name: mm[1]}
				}
			case reDefProps.MatchString(t):
				mm := reDefProps.FindStringSubmatch(t)
				pending = &scope{kind: "object", name: mm[1], exported: exportedLater[mm[1]] || declared[mm[1]]}
			case reDefProp.MatchString(rawT):
				mm := reDefProp.FindStringSubmatch(rawT)
				add(decl{path: mm[1] + "." + mm[2], name: mm[2], kind: "member", line: i + 1, exported: exportedLater[mm[1]] || declared[mm[1]]})
			case reProto.MatchString(t):
				mm := reProto.FindStringSubmatch(t)
				d := decl{path: mm[1] + "." + mm[2], name: mm[2], kind: "method", line: i + 1, exported: exportedLater[mm[1]] || declared[mm[1]]}
				d.span = &jsSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i), params: params(lines, masked, i)}
				add(d)
				if strings.Contains(m, "{") {
					pending = &scope{kind: "function", name: mm[2]}
				}
			case reAssign.MatchString(t):
				mm := reAssign.FindStringSubmatch(t)
				if declared[mm[1]] && !notMember[mm[2]] {
					d := decl{path: mm[1] + "." + mm[2], name: mm[2], kind: "method", line: i + 1, exported: true}
					d.span = &jsSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i), params: params(lines, masked, i)}
					add(d)
					if strings.Contains(m, "{") {
						pending = &scope{kind: "function", name: mm[2]}
					}
				}
			case reObjOpen.MatchString(t):
				mm := reObjOpen.FindStringSubmatch(t)
				declared[mm[1]] = true
				d := decl{path: mm[1], name: mm[1], kind: "object", line: i + 1, exported: strings.HasPrefix(t, "export")}
				d.span = &jsSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i)}
				d.span.docStart, d.span.docEnd, d.span.doc = docAbove(lines, i)
				add(d)
				pending = &scope{kind: "object", name: mm[1], exported: d.exported}
			case reDecl.MatchString(t):
				mm := reDecl.FindStringSubmatch(t)
				kind := strings.TrimSpace(strings.TrimSuffix(mm[1], "*"))
				name := mm[2]
				declared[name] = true
				exported := strings.HasPrefix(t, "export") || strings.HasPrefix(t, "declare")
				d := decl{path: name, name: name, kind: kind, line: i + 1, exported: exported}
				switch kind {
				case "interface", "type", "namespace", "module":
					// types carry no body to date; namespaces are scopes
					if kind == "namespace" || kind == "module" {
						pending = &scope{kind: "namespace", name: name, exported: exported}
						if strings.HasPrefix(name, "'") || strings.HasPrefix(name, "\"") {
							pending = &scope{kind: "other"}
						}
					}
				case "enum":
					pending = &scope{kind: "enum", name: name, exported: exported}
					for _, v := range inlineMembers(t) {
						add(decl{path: name + "." + v, name: v, kind: "member", line: i + 1, exported: exported})
					}
				case "class":
					d.span = &jsSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i)}
					d.span.docStart, d.span.docEnd, d.span.doc = docAbove(lines, i)
					if strings.Contains(t, " extends ") {
						d.kind = "class:open" // inherited members are not indexed
					}
					pending = &scope{kind: "class", name: name, exported: exported}
				default:
					d.span = &jsSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i)}
					d.span.docStart, d.span.docEnd, d.span.doc = docAbove(lines, i)
					if kind == "function" || strings.Contains(t, "=>") || strings.Contains(t, "function") {
						d.span.params = params(lines, masked, i)
					}
					if strings.Contains(m, "{") {
						pending = &scope{kind: "function", name: name}
					}
				}
				add(d)
			}
		} else if pending == nil && depth > 0 {
			// inside a function or block: a declaration here is local, but
			// its braces still need a scope so members are not misattached.
			// An object literal built inside a factory (fastify's
			// `const fastify = { listen, inject, … }`) is the instance the
			// documentation describes, so its members are indexed under
			// the object's name.
			if mm := reObjOpen.FindStringSubmatch(t); mm != nil && depth <= 2 {
				declared[mm[1]] = true
				add(decl{path: mm[1], name: mm[1], kind: "object", line: i + 1})
				pending = &scope{kind: "object", name: mm[1]}
			} else if mm := reDefProps.FindStringSubmatch(t); mm != nil && depth <= 2 {
				pending = &scope{kind: "object", name: mm[1], exported: exportedLater[mm[1]] || declared[mm[1]]}
			} else if mm := reAssign.FindStringSubmatch(t); mm != nil && depth <= 2 && declared[mm[1]] && !notMember[mm[2]] {
				add(decl{path: mm[1] + "." + mm[2], name: mm[2], kind: "member", line: i + 1, exported: exportedLater[mm[1]]})
				if strings.Contains(m, "{") {
					pending = &scope{kind: "other"}
				}
			} else if reDecl.MatchString(t) && strings.Contains(m, "{") {
				pending = &scope{kind: "other"}
			}
		}

		// brace accounting; a pending scope opens on the first "{" and is
		// dropped by a ";" outside parentheses (a type alias, an arrow
		// assignment without braces)
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
					// also inside parentheses: Object.defineProperties(X, {
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
	// names exported after their declaration (export { a }, export default a,
	// module.exports = { a })
	for i := range out {
		if exportedLater[out[i].name] && !strings.Contains(out[i].path, ".") {
			out[i].exported = true
		}
		if owner, _, ok := strings.Cut(out[i].path, "."); ok && exportedLater[owner] && !out[i].private {
			out[i].exported = true
		}
	}
	return out, globs
}

// exportNames returns the local names of an export list: "a, b as c,
// type D" → a, c, D.
func exportNames(list string) []string {
	var out []string
	for _, it := range strings.Split(list, ",") {
		it = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(it), "type "))
		if it == "" {
			continue
		}
		if k := strings.Index(it, " as "); k >= 0 {
			it = strings.TrimSpace(it[k+4:])
		}
		if k := strings.IndexByte(it, ':'); k >= 0 {
			it = strings.TrimSpace(it[:k]) // { a: b } in module.exports: the key
		}
		if it == "default" || !isIdent(it) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// inlineMembers returns the members of an enum written on one line:
// "export enum Mode { Fast, Slow = 2 }".
func inlineMembers(t string) []string {
	open := strings.IndexByte(t, '{')
	close := strings.LastIndexByte(t, '}')
	if open < 0 || close <= open {
		return nil
	}
	var out []string
	for _, seg := range strings.Split(t[open+1:close], ",") {
		if mm := reEnumMember.FindStringSubmatch(strings.TrimSpace(seg)); mm != nil {
			out = append(out, mm[1])
		}
	}
	return out
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// docAbove returns the JSDoc block or run of "//" lines directly above
// lines[i] (decorator lines in between are skipped), as 1-based first and
// last line and the text with markers removed.
func docAbove(lines []string, i int) (int, int, []string) {
	j := i - 1
	for j >= 0 && reDecorator.MatchString(strings.TrimSpace(lines[j])) {
		j--
	}
	if j < 0 {
		return 0, 0, nil
	}
	t := strings.TrimSpace(strings.TrimRight(lines[j], "\r"))
	if strings.HasSuffix(t, "*/") {
		last := j
		for j >= 0 && !strings.Contains(lines[j], "/*") {
			j--
		}
		if j < 0 || !strings.Contains(lines[j], "/**") {
			return 0, 0, nil
		}
		var doc []string
		for k := j; k <= last; k++ {
			s := strings.TrimSpace(strings.TrimRight(lines[k], "\r"))
			s = strings.TrimPrefix(s, "/**")
			s = strings.TrimSuffix(s, "*/")
			s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "*"))
			doc = append(doc, s)
		}
		return j + 1, last + 1, trimDoc(doc)
	}
	if strings.HasPrefix(t, "//") {
		last := j
		for j >= 0 && strings.HasPrefix(strings.TrimSpace(lines[j]), "//") {
			j--
		}
		first := j + 1
		var doc []string
		for k := first; k <= last; k++ {
			s := strings.TrimSpace(strings.TrimRight(lines[k], "\r"))
			doc = append(doc, strings.TrimPrefix(strings.TrimPrefix(s, "//"), " "))
		}
		return first + 1, last + 1, trimDoc(doc)
	}
	return 0, 0, nil
}

func trimDoc(doc []string) []string {
	for len(doc) > 0 && strings.TrimSpace(doc[0]) == "" {
		doc = doc[1:]
	}
	for len(doc) > 0 && strings.TrimSpace(doc[len(doc)-1]) == "" {
		doc = doc[:len(doc)-1]
	}
	if len(doc) == 0 {
		return nil
	}
	return doc
}

// bodyEndLine returns the 1-based line closing the declaration's braces;
// a declaration ended by ";" before any "{" ends on that line.
func bodyEndLine(masked []string, start int) int {
	depth, paren := 0, 0
	opened := false
	last := start + 5000
	if last > len(masked) {
		last = len(masked)
	}
	for j := start; j < last; j++ {
		code := masked[j]
		for i := 0; i < len(code); i++ {
			switch code[i] {
			case '(', '[':
				paren++
			case ')', ']':
				if paren > 0 {
					paren--
				}
			case '{':
				depth++
				opened = true
			case '}':
				if depth > 0 {
					depth--
				}
			case ';':
				if !opened && paren == 0 {
					return j + 1
				}
			}
		}
		if opened && depth == 0 {
			return j + 1
		}
		if !opened && j > start && strings.TrimSpace(code) == "" {
			return j // an arrow assignment without braces or semicolon
		}
	}
	return start + 1
}

// params returns the parameter names of the function starting on
// lines[start]: identifiers before ":" or "=", "..." stripped; patterns
// are skipped.
func params(lines, masked []string, start int) []string {
	last := start + 100
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
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			depth--
			if depth == 0 && m[i] == ')' {
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
		s = strings.TrimPrefix(s, "...")
		for _, mod := range []string{"public ", "private ", "protected ", "readonly "} {
			s = strings.TrimPrefix(s, mod)
		}
		if i := strings.IndexAny(s, ":=?"); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
		if isIdent(s) && s != "this" {
			out = append(out, s)
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

// maskAll blanks string literals (including template literals across
// lines) and comments in every line. Each result has the length of its
// input line.
func maskAll(lines []string) []string {
	out := make([]string, len(lines))
	state := byte(0) // 0, '*' for a block comment, '`' for a template literal
	for i, ln := range lines {
		out[i], state = maskLine(strings.TrimRight(ln, "\r"), state)
	}
	return out
}

func maskLine(ln string, state byte) (string, byte) {
	out := []byte(ln)
	for i := 0; i < len(out); i++ {
		c := out[i]
		switch state {
		case '*':
			if c == '*' && i+1 < len(out) && out[i+1] == '/' {
				out[i], out[i+1] = ' ', ' '
				i++
				state = 0
				continue
			}
			out[i] = ' '
			continue
		case '`':
			if c == '\\' {
				out[i] = ' '
				if i+1 < len(out) {
					out[i+1] = ' '
					i++
				}
				continue
			}
			if c == '`' {
				state = 0
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
				return string(out), state
			}
			if i+1 < len(out) && out[i+1] == '*' {
				out[i], out[i+1] = ' ', ' '
				i++
				state = '*'
			}
		case '"', '\'':
			j := i + 1
			for j < len(out) && out[j] != c {
				if out[j] == '\\' {
					j++
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
		case '`':
			state = '`'
		}
	}
	return string(out), state
}

// templateToPlain rewrites a template literal without substitutions into
// a double-quoted string so that the literal scanner records it.
func templateToPlain(ln string) string {
	if !strings.Contains(ln, "`") || strings.Contains(ln, "${") {
		return ln
	}
	return strings.ReplaceAll(ln, "`", "\"")
}
