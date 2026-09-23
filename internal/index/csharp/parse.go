package csharp

import (
	"regexp"
	"strings"
)

// decl is one declaration as parseFile sees it.
type decl struct {
	namespace string
	path      string // Type, Type.Member, Outer.Inner.Member
	name      string
	kind      string
	file      string
	line      int
	exported  bool
	span      *csSpan
}

// csSpan is the comment and body geometry of one declaration. Line
// numbers are 1-based; zero means "none".
type csSpan struct {
	docStart int
	docEnd   int
	declLine int
	bodyEnd  int
	doc      []string
	params   []string
}

// scope is an open brace block.
type scope struct {
	kind     string // namespace, type, enum, member, other
	name     string // type name, or the namespace path
	typeKind string // class, struct, interface, record for a type scope
	depth    int
	exported bool
}

const ident = `[A-Za-z_][A-Za-z0-9_]*`

var (
	reNamespaceFile  = regexp.MustCompile(`^namespace\s+(` + ident + `(?:\.` + ident + `)*)\s*;`)
	reNamespaceBlock = regexp.MustCompile(`^namespace\s+(` + ident + `(?:\.` + ident + `)*)\s*(?:\{|$)`)
	modifiers        = `(?:(?:public|internal|private|protected|static|sealed|abstract|partial|readonly|ref|unsafe|new|file|virtual|override|async|extern|volatile|const|required)\s+)*`
	// [modifiers] class|struct|interface|enum|record [class|struct] Name
	reType = regexp.MustCompile(`^` + modifiers + `(class|struct|interface|enum|record(?:\s+(?:class|struct))?)\s+(` + ident + `)`)
	// [modifiers] delegate RET Name(
	reDelegate = regexp.MustCompile(`^` + modifiers + `delegate\s+.+?\s(` + ident + `)\s*(?:<[^>]*>)?\s*\(`)
	// [modifiers] RET Name(   — a method or a constructor
	reMethod = regexp.MustCompile(`^` + modifiers + `(?:[A-Za-z_][A-Za-z0-9_.<>\[\],?() ]*?\s+)?(` + ident + `)\s*(?:<[^>]*>)?\s*\(`)
	// [modifiers] RET Name { get; }  / RET Name => expr  / RET Name;  / RET Name = value;
	reProperty = regexp.MustCompile(`^` + modifiers + `(?:event\s+)?[A-Za-z_][A-Za-z0-9_.<>\[\],?()]*\s+(` + ident + `)\s*(?:\{|=>|;|=[^=>])`)
	// an enum member
	reEnumMember = regexp.MustCompile(`^(` + ident + `)\s*(?:[,=]|$)`)
	reAttribute  = regexp.MustCompile(`^\[`)
)

var keywords = map[string]bool{
	"if": true, "for": true, "foreach": true, "while": true, "switch": true, "return": true, "catch": true, "else": true,
	"try": true, "do": true, "using": true, "lock": true, "new": true, "throw": true, "await": true, "typeof": true,
	"yield": true, "base": true, "this": true, "namespace": true, "class": true, "struct": true, "interface": true,
	"enum": true, "record": true, "delegate": true, "get": true, "set": true, "init": true, "add": true, "remove": true,
	"var": true, "operator": true, "implicit": true, "explicit": true, "where": true, "fixed": true, "checked": true,
	"unchecked": true, "default": true, "case": true, "goto": true, "is": true, "as": true, "in": true, "out": true,
}

// parseFile scans one file and returns every recognised declaration.
func parseFile(lines, masked []string, rel string) []decl {
	var out []decl
	var scopes []scope
	var pending *scope
	depth, paren := 0, 0
	fileNS := "" // file-scoped namespace

	namespace := func() string {
		ns := fileNS
		for _, s := range scopes {
			if s.kind == "namespace" {
				if ns == "" {
					ns = s.name
				} else {
					ns += "." + s.name
				}
			}
		}
		return ns
	}
	// typePath is the dotted name of the enclosing types.
	typePath := func() (string, bool, bool) {
		var segs []string
		exported := true
		inType := false
		for _, s := range scopes {
			if s.kind == "type" || s.kind == "enum" {
				segs = append(segs, s.name)
				exported = exported && s.exported
				inType = true
			}
		}
		return strings.Join(segs, "."), exported, inType
	}
	top := func() *scope {
		if n := len(scopes); n > 0 && depth == scopes[n-1].depth+1 {
			return &scopes[n-1]
		}
		return nil
	}
	// placed reports whether a declaration here belongs to a namespace or
	// type body rather than to a method body.
	placed := func() bool {
		if depth == 0 {
			return true
		}
		s := top()
		return s != nil && (s.kind == "namespace" || s.kind == "type" || s.kind == "enum")
	}
	add := func(d decl) {
		d.file = rel
		d.namespace = namespace()
		out = append(out, d)
	}

	for i, raw := range lines {
		rawT := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		m := masked[i]
		t := strings.TrimSpace(m)
		if t == "" || strings.HasPrefix(rawT, "//") || strings.HasPrefix(rawT, "#") || reAttribute.MatchString(t) && !strings.Contains(t, "]") {
			if !strings.ContainsAny(m, "{}") {
				continue
			}
		}
		// an attribute in front of the declaration on the same line
		for reAttribute.MatchString(t) {
			end := strings.IndexByte(t, ']')
			if end < 0 {
				break
			}
			t = strings.TrimSpace(t[end+1:])
		}

		if pending == nil && t != "" && !strings.HasPrefix(t, "}") {
			switch {
			case reNamespaceFile.MatchString(t):
				fileNS = reNamespaceFile.FindStringSubmatch(t)[1]
			case reNamespaceBlock.MatchString(t) && placed():
				pending = &scope{kind: "namespace", name: reNamespaceBlock.FindStringSubmatch(t)[1]}
			case placed() && reType.MatchString(t):
				mm := reType.FindStringSubmatch(t)
				kind := strings.Fields(mm[1])[0]
				name := mm[2]
				prefix, outerExported, _ := typePath()
				exported := outerExported && (strings.Contains(t, "public ") || strings.Contains(t, "protected "))
				d := decl{path: name, name: name, kind: kind, line: i + 1, exported: exported}
				if prefix != "" {
					d.path = prefix + "." + name
				}
				d.span = &csSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i)}
				d.span.docStart, d.span.docEnd, d.span.doc = docAbove(lines, i)
				if strings.Contains(t, " : ") || strings.HasSuffix(t, " :") {
					if kind != "enum" {
						d.kind = kind + ":open" // inherits members the index does not see
					}
				}
				add(d)
				if kind == "enum" {
					pending = &scope{kind: "enum", name: name, exported: exported}
					for _, v := range inlineMembers(t) {
						add(decl{path: d.path + "." + v, name: v, kind: "member", line: i + 1, exported: exported})
					}
				} else {
					pending = &scope{kind: "type", name: name, typeKind: kind, exported: exported}
				}
			case placed() && reDelegate.MatchString(t):
				mm := reDelegate.FindStringSubmatch(t)
				prefix, outerExported, _ := typePath()
				d := decl{path: mm[1], name: mm[1], kind: "delegate", line: i + 1, exported: outerExported && strings.Contains(t, "public ")}
				if prefix != "" {
					d.path = prefix + "." + mm[1]
				}
				add(d)
			default:
				s := top()
				if s == nil {
					break
				}
				switch s.kind {
				case "enum":
					if mm := reEnumMember.FindStringSubmatch(t); mm != nil {
						prefix, _, _ := typePath()
						add(decl{path: prefix + "." + mm[1], name: mm[1], kind: "member", line: i + 1, exported: s.exported})
					}
				case "type":
					prefix, outerExported, _ := typePath()
					memberExported := outerExported && (strings.Contains(t, "public ") || strings.Contains(t, "protected ") || isInterface(scopes))
					if mm := reMethod.FindStringSubmatch(t); mm != nil && !keywords[mm[1]] {
						name := mm[1]
						if name == s.name {
							// a constructor: not a member the documentation names on its own
							if strings.Contains(m, "{") {
								pending = &scope{kind: "member", name: name}
							}
							break
						}
						d := decl{path: prefix + "." + name, name: name, kind: "method", line: i + 1, exported: memberExported}
						d.span = &csSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i), params: params(lines, masked, i)}
						d.span.docStart, d.span.docEnd, d.span.doc = docAbove(lines, i)
						add(d)
						if strings.Contains(m, "{") {
							pending = &scope{kind: "member", name: name}
						}
					} else if mm := reProperty.FindStringSubmatch(t); mm != nil && !keywords[mm[1]] {
						name := mm[1]
						kind := "property"
						if strings.Contains(t, "event ") {
							kind = "event"
						} else if strings.HasSuffix(t, ";") || strings.Contains(t, " = ") && !strings.Contains(t, "=>") && !strings.Contains(t, "{") {
							kind = "field"
						}
						d := decl{path: prefix + "." + name, name: name, kind: kind, line: i + 1, exported: memberExported}
						d.span = &csSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i)}
						d.span.docStart, d.span.docEnd, d.span.doc = docAbove(lines, i)
						add(d)
						if strings.Contains(m, "{") {
							pending = &scope{kind: "member", name: name}
						}
					}
				}
			}
		}

		// brace accounting; a pending scope opens on the first "{"; a
		// declaration that ends with ";" before any "{" opens nothing
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
				if pending != nil && paren == 0 && depth == pending.depth {
					pending = nil
				}
			}
		}
		if pending != nil && !strings.Contains(m, "{") && strings.HasSuffix(t, ";") {
			pending = nil
		}
	}
	return out
}

// isInterface reports whether the innermost type scope is an interface,
// whose members are public without saying so.
func isInterface(scopes []scope) bool {
	for i := len(scopes) - 1; i >= 0; i-- {
		if scopes[i].kind == "type" {
			return scopes[i].typeKind == "interface"
		}
	}
	return false
}

// inlineMembers returns the members of an enum written on one line:
// "public enum Mode { Fast, Slow = 2 }".
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

// docAbove returns the run of "///" XML doc lines directly above lines[i]
// (attribute lines in between are skipped): 1-based first and last line
// and the text with markers and XML tags removed.
func docAbove(lines []string, i int) (int, int, []string) {
	j := i - 1
	for j >= 0 && (strings.HasPrefix(strings.TrimSpace(lines[j]), "[") || strings.TrimSpace(lines[j]) == "") {
		if strings.TrimSpace(lines[j]) == "" {
			return 0, 0, nil
		}
		j--
	}
	if j < 0 || !strings.HasPrefix(strings.TrimSpace(lines[j]), "///") {
		return 0, 0, nil
	}
	last := j
	for j >= 0 && strings.HasPrefix(strings.TrimSpace(lines[j]), "///") {
		j--
	}
	first := j + 1
	var doc []string
	for k := first; k <= last; k++ {
		s := strings.TrimSpace(strings.TrimRight(lines[k], "\r"))
		s = strings.TrimPrefix(strings.TrimPrefix(s, "///"), " ")
		s = reXMLTag.ReplaceAllString(s, "")
		if strings.TrimSpace(s) != "" {
			doc = append(doc, strings.TrimSpace(s))
		}
	}
	if len(doc) == 0 {
		return 0, 0, nil
	}
	return first + 1, last + 1, doc
}

var reXMLTag = regexp.MustCompile(`</?[a-z]+(?:\s[^>]*)?/?>`)

// bodyEndLine returns the 1-based line closing the declaration's braces;
// a declaration ended by ";" before any "{" ends on that line.
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

// params returns the parameter names of the method declared on
// lines[start]: the last identifier of each comma-separated segment before
// any "=" default.
func params(lines, masked []string, start int) []string {
	last := start + 60
	if last > len(lines) {
		last = len(lines)
	}
	var msk strings.Builder
	for j := start; j < last; j++ {
		if j > start {
			msk.WriteByte('\n')
		}
		msk.WriteString(masked[j])
	}
	m := msk.String()
	open := strings.IndexByte(m, '(')
	if open < 0 {
		return nil
	}
	depth, closed := 0, -1
	for i := open; i < len(m) && closed < 0; i++ {
		switch m[i] {
		case '(', '[', '<':
			depth++
		case ')', ']', '>':
			depth--
			if depth == 0 && m[i] == ')' {
				closed = i
			}
		}
	}
	if closed < 0 {
		return nil
	}
	var out []string
	segStart := open + 1
	depth = 0
	emit := func(seg string) {
		s := strings.TrimSpace(strings.ReplaceAll(seg, "\n", " "))
		if i := strings.IndexByte(s, '='); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
		f := strings.Fields(s)
		if len(f) == 0 {
			return
		}
		name := f[len(f)-1]
		if isIdent(name) && !keywords[name] {
			out = append(out, name)
		}
	}
	for i := open + 1; i < closed; i++ {
		switch m[i] {
		case '(', '[', '<':
			depth++
		case ')', ']', '>':
			depth--
		case ',':
			if depth == 0 {
				emit(m[segStart:i])
				segStart = i + 1
			}
		}
	}
	emit(m[segStart:closed])
	return out
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

// maskAll blanks string literals (verbatim and raw strings across lines
// included), char literals and comments in every line. Each result has
// the length of its input line.
func maskAll(lines []string) []string {
	out := make([]string, len(lines))
	state := byte(0) // 0, '*' block comment, '@' verbatim string, '"' raw string
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
		case '@':
			if c == '"' {
				if i+1 < len(out) && out[i+1] == '"' {
					out[i], out[i+1] = ' ', ' '
					i++
					continue
				}
				state = 0
				continue
			}
			out[i] = ' '
			continue
		case '"': // a """ raw string literal
			if strings.HasPrefix(string(out[i:]), `"""`) {
				out[i], out[i+1], out[i+2] = ' ', ' ', ' '
				i += 2
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
		case '"':
			if strings.HasPrefix(string(out[i:]), `"""`) {
				out[i], out[i+1], out[i+2] = ' ', ' ', ' '
				i += 2
				state = '"'
				continue
			}
			verbatim := i > 0 && (out[i-1] == '@' || i > 1 && out[i-1] == '$' && out[i-2] == '@' || i > 1 && out[i-1] == '@' && out[i-2] == '$')
			if verbatim {
				state = '@'
				continue
			}
			j := i + 1
			for j < len(out) && out[j] != '"' {
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
		case '\'':
			if i+2 < len(out) && out[i+2] == '\'' {
				out[i+1] = ' '
				i += 2
			} else if i+3 < len(out) && out[i+1] == '\\' && out[i+3] == '\'' {
				out[i+1], out[i+2] = ' ', ' '
				i += 3
			}
		}
	}
	return string(out), state
}
