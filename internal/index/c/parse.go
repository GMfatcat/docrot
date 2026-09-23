package c

import (
	"regexp"
	"strconv"
	"strings"
)

// decl is one declaration as parseFile sees it.
type decl struct {
	namespace string // C++ namespace path with "::"
	path      string // name, Type::member
	name      string
	kind      string
	file      string
	line      int
	exported  bool
	span      *cSpan
}

// cSpan is the comment and body geometry of one declaration. Line numbers
// are 1-based; zero means "none".
type cSpan struct {
	docStart int
	docEnd   int
	declLine int
	bodyEnd  int
	doc      []string
	params   []string
}

// scope is an open brace block.
type scope struct {
	kind    string // namespace, type, enum, typedef, function, other
	name    string
	tkind   string // struct, class, union for a type scope
	depth   int
	macro   bool   // a namespace opened by an X_NAMESPACE_BEGIN macro: closed by its END, not by a brace
	access  string // current access label of a type scope
	typedef bool   // "typedef struct … { … } NAME;": the closing line names the type
}

const ident = `[A-Za-z_][A-Za-z0-9_]*`

var (
	reDefine    = regexp.MustCompile(`^#\s*define\s+(` + ident + `)`)
	reNamespace = regexp.MustCompile(`^(?:inline\s+)?namespace\s+(` + ident + `(?:::` + ident + `)*)\s*(?:\{|$)`)
	reNSMacro   = regexp.MustCompile(`^([A-Z][A-Z0-9_]*)_NAMESPACE_(BEGIN|END)\b`)
	// [typedef] struct|class|union|enum [class] NAME [: base] [{]
	reType = regexp.MustCompile(`^(?:typedef\s+)?(struct|class|union|enum(?:\s+(?:class|struct))?)\s+(?:` + ident + `\s+)?(` + ident + `)\s*(?:final\s*)?(?::[^{]*)?(\{|;|$)`)
	// typedef struct { … } NAME;  → the closing line
	reTypedefEnd = regexp.MustCompile(`^\}\s*(` + ident + `)\s*;`)
	// typedef RET (*NAME)(…);  typedef … NAME;
	reTypedefFn  = regexp.MustCompile(`^typedef\s+.*\(\s*\*\s*(` + ident + `)\s*\)`)
	reTypedefOne = regexp.MustCompile(`^typedef\s+.*?\b(` + ident + `)\s*(?:\[[^\]]*\])?\s*;`)
	reUsing      = regexp.MustCompile(`^using\s+(` + ident + `)\s*=`)
	// typedef struct { / typedef enum {   (anonymous, named on the closing line)
	reAnonType = regexp.MustCompile(`^typedef\s+(struct|enum|union)\s*\{`)
	// [qualifiers] RET [Type::]name(   — a prototype, definition or method
	reFunc = regexp.MustCompile(`^(?:(?:static|inline|extern|constexpr|virtual|explicit|friend|[A-Z][A-Z0-9_]*(?:_EXTERN|_INLINE|_API|_EXPORT|_PUBLIC|_DECL))\s+)*(?:"C"\s+)?(?:(?:const|unsigned|signed|struct|enum|union|typename)\s+)*(` + ident + `(?:::` + ident + `)*(?:<[^>]*>)?)\s*[*&\s]+\s*(?:(` + ident + `)::)?(` + ident + `)\s*\(`)
	// name( at column 0: K&R style, the return type on the line above
	reKR = regexp.MustCompile(`^(` + ident + `)\s*\(`)
	// a field inside a type: RET name; / RET name = x; / RET name[N];
	reField  = regexp.MustCompile(`^(?:(?:static|const|mutable|constexpr|inline|unsigned|signed|struct|enum|union)\s+)*` + ident + `(?:::` + ident + `)*(?:<[^>]*>)?\s*[*&\s]+\s*(` + ident + `)\s*(?:\[[^\]]*\])?\s*(?:=[^=]|;|\{)`)
	reAccess = regexp.MustCompile(`^(public|private|protected)\s*:`)
	// an enum value: NAME, NAME = x, MACRO(NAME, …)
	reEnumValue = regexp.MustCompile(`^(?:[A-Z][A-Z0-9_]*\(\s*)?(` + ident + `)\s*(?:[,=)]|$)`)
	reDoxyTag   = regexp.MustCompile(`^[@\\]param(?:\[[^\]]*\])?\s+\w+\s*|^[@\\](?:brief|return|returns|note|warning|sa|see|since|deprecated|throws?)\s*`)
)

var keywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "return": true, "catch": true, "else": true, "do": true,
	"sizeof": true, "defined": true, "typedef": true, "case": true, "goto": true, "new": true, "delete": true,
	"throw": true, "operator": true, "template": true, "using": true, "namespace": true, "class": true, "struct": true,
	"union": true, "enum": true, "static_assert": true, "decltype": true, "alignas": true, "alignof": true,
	"__attribute__": true, "__declspec": true, "assert": true,
}

// withType prefixes a member name with the enclosing type path.
func withType(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "::" + name
}

// inlineValues returns the values of an enum written on one line:
// "enum class Mode { Fast, Slow = 2 };".
func inlineValues(t string) []string {
	open := strings.IndexByte(t, '{')
	close := strings.LastIndexByte(t, '}')
	if open < 0 || close <= open {
		return nil
	}
	var out []string
	for _, seg := range strings.Split(t[open+1:close], ",") {
		if mm := reEnumValue.FindStringSubmatch(strings.TrimSpace(seg)); mm != nil && !keywords[mm[1]] {
			out = append(out, mm[1])
		}
	}
	return out
}

// parseFile scans one file and returns every recognised declaration.
// header marks a header file, whose declarations are public.
func parseFile(lines, masked []string, rel string, header bool) []decl {
	var out []decl
	var scopes []scope
	var pending *scope
	depth, paren := 0, 0
	anonCount := 0

	namespace := func() string {
		var segs []string
		for _, s := range scopes {
			if s.kind == "namespace" {
				segs = append(segs, s.name)
			}
		}
		return strings.Join(segs, "::")
	}
	typeScope := func() *scope {
		if n := len(scopes); n > 0 && depth == scopes[n-1].depth+1 && scopes[n-1].kind == "type" {
			return &scopes[n-1]
		}
		return nil
	}
	enumScope := func() *scope {
		if n := len(scopes); n > 0 && depth == scopes[n-1].depth+1 && scopes[n-1].kind == "enum" {
			return &scopes[n-1]
		}
		return nil
	}
	typePath := func() string {
		var segs []string
		for _, s := range scopes {
			if s.kind == "type" {
				segs = append(segs, s.name)
			}
		}
		return strings.Join(segs, "::")
	}
	// placed reports whether a declaration here belongs to the file, a
	// namespace or a type rather than to a function body.
	placed := func() bool {
		if depth == 0 {
			return true
		}
		n := len(scopes)
		if n == 0 {
			return false
		}
		s := scopes[n-1]
		if s.macro {
			return depth == s.depth
		}
		return depth == s.depth+1 && (s.kind == "namespace" || s.kind == "type" || s.kind == "extern")
	}
	add := func(d decl) {
		d.file = rel
		d.namespace = namespace()
		out = append(out, d)
	}
	memberExported := func(s *scope) bool {
		if s == nil {
			return header
		}
		if s.access == "" {
			return s.tkind != "class"
		}
		return s.access == "public"
	}

	for i, raw := range lines {
		rawT := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		m := masked[i]
		t := strings.TrimSpace(m)

		// preprocessor lines take no part in brace accounting
		if strings.HasPrefix(rawT, "#") {
			if mm := reDefine.FindStringSubmatch(rawT); mm != nil && !strings.HasPrefix(mm[1], "_") {
				add(decl{path: mm[1], name: mm[1], kind: "macro", line: i + 1, exported: header})
			}
			continue
		}
		if mm := reNSMacro.FindStringSubmatch(t); mm != nil {
			if mm[2] == "BEGIN" {
				name := strings.ToLower(strings.SplitN(mm[1], "_", 2)[0])
				scopes = append(scopes, scope{kind: "namespace", name: name, depth: depth, macro: true})
			} else {
				for n := len(scopes); n > 0; n = len(scopes) {
					top := scopes[n-1]
					scopes = scopes[:n-1]
					if top.macro {
						break
					}
				}
			}
			continue
		}
		if t == "" || strings.HasPrefix(rawT, "//") || strings.HasPrefix(rawT, "/*") || strings.HasPrefix(rawT, "*") {
			if !strings.ContainsAny(m, "{}") {
				continue
			}
		}

		if pending == nil && t != "" {
			if ts := typeScope(); ts != nil {
				if am := reAccess.FindStringSubmatch(t); am != nil {
					ts.access = am[1]
				}
			}
			switch {
			case strings.HasPrefix(rawT, "extern \"C\"") && strings.Contains(m, "{"):
				// extern "C" { … }: transparent, everything inside is at file level
				pending = &scope{kind: "extern"}
			case strings.HasPrefix(t, "}"):
				if mm := reTypedefEnd.FindStringSubmatch(t); mm != nil {
					if n := len(scopes); n > 0 && scopes[n-1].typedef && scopes[n-1].depth == depth-1 {
						old := scopes[n-1].name
						if strings.HasPrefix(old, "anon#") {
							// an anonymous struct or enum: the typedef name is the type
							kind := "struct"
							if scopes[n-1].kind == "enum" {
								kind = "enum"
							}
							for j := range out {
								if out[j].path == old && out[j].namespace == namespace() {
									out[j].path, out[j].name, out[j].kind, out[j].exported = mm[1], mm[1], kind, header
								} else if strings.HasPrefix(out[j].path, old+"::") && out[j].namespace == namespace() {
									out[j].path = mm[1] + out[j].path[len(old):]
								}
							}
						} else if mm[1] != old {
							add(decl{path: mm[1], name: mm[1], kind: "typedef", line: i + 1, exported: header})
						}
					}
				}
			case reNamespace.MatchString(t) && placed():
				pending = &scope{kind: "namespace", name: reNamespace.FindStringSubmatch(t)[1]}
			case placed() && reType.MatchString(t):
				mm := reType.FindStringSubmatch(t)
				kind := strings.Fields(mm[1])[0]
				name := mm[2]
				if mm[3] == ";" && !strings.HasPrefix(t, "typedef") {
					break // a forward declaration
				}
				prefix := typePath()
				d := decl{path: name, name: name, kind: kind, line: i + 1, exported: memberExported(typeScope())}
				if prefix != "" {
					d.path = prefix + "::" + name
				}
				d.span = &cSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i)}
				d.span.docStart, d.span.docEnd, d.span.doc = docAbove(lines, i)
				if kind != "enum" && strings.Contains(t, ":") && !strings.Contains(t, "::") {
					d.kind = kind + ":open" // inherits members the index does not see
				}
				if kind == "enum" {
					pending = &scope{kind: "enum", name: d.path, typedef: strings.HasPrefix(t, "typedef")}
					for _, v := range inlineValues(t) {
						add(decl{path: d.path + "::" + v, name: v, kind: "member", line: i + 1, exported: d.exported})
					}
					if strings.Contains(t, "}") {
						pending = nil // the whole enum sits on this line
					}
				} else {
					pending = &scope{kind: "type", name: name, tkind: kind, typedef: strings.HasPrefix(t, "typedef")}
				}
				add(d)
			case placed() && reAnonType.MatchString(t):
				// typedef struct { … } NAME; / typedef enum { … } NAME;: the
				// members are recorded under a placeholder until the closing line
				mm := reAnonType.FindStringSubmatch(t)
				anonCount++
				name := "anon#" + strconv.Itoa(anonCount)
				kind := "type"
				if mm[1] == "enum" {
					kind = "enum"
				}
				pending = &scope{kind: kind, name: name, tkind: mm[1], typedef: true}
				add(decl{path: name, name: name, kind: mm[1], line: i + 1, exported: false})
			case placed() && reTypedefFn.MatchString(t):
				mm := reTypedefFn.FindStringSubmatch(t)
				add(decl{path: withType(typePath(), mm[1]), name: mm[1], kind: "typedef", line: i + 1, exported: header})
			case placed() && reTypedefOne.MatchString(t):
				mm := reTypedefOne.FindStringSubmatch(t)
				if !keywords[mm[1]] {
					add(decl{path: withType(typePath(), mm[1]), name: mm[1], kind: "typedef", line: i + 1, exported: header})
				}
			case placed() && reUsing.MatchString(t):
				mm := reUsing.FindStringSubmatch(t)
				p := mm[1]
				if prefix := typePath(); prefix != "" {
					p = prefix + "::" + mm[1]
				}
				add(decl{path: p, name: mm[1], kind: "alias", line: i + 1, exported: memberExported(typeScope())})
			case enumScope() != nil:
				if mm := reEnumValue.FindStringSubmatch(t); mm != nil && !keywords[mm[1]] {
					es := enumScope()
					add(decl{path: es.name + "::" + mm[1], name: mm[1], kind: "member", line: i + 1, exported: header})
				}
			case placed() && (reFunc.MatchString(t) || reKR.MatchString(t) && depth == 0 && !keywords[reKR.FindStringSubmatch(t)[1]]):
				var owner, name string
				if mm := reFunc.FindStringSubmatch(t); mm != nil {
					owner, name = mm[2], mm[3]
					if keywords[name] || keywords[mm[1]] && mm[1] != "struct" && mm[1] != "enum" && mm[1] != "union" {
						break
					}
				} else {
					name = reKR.FindStringSubmatch(t)[1]
				}
				ts := typeScope()
				if ts != nil && (name == ts.name || strings.HasPrefix(t, "~")) {
					// a constructor or destructor: not a member the documentation names on its own
					if strings.Contains(m, "{") {
						pending = &scope{kind: "function", name: name}
					}
					break
				}
				exported := header || !strings.HasPrefix(t, "static ")
				if ts != nil {
					exported = memberExported(ts)
				}
				p := name
				if owner != "" {
					p = owner + "::" + name
				} else if prefix := typePath(); prefix != "" {
					p = prefix + "::" + name
				}
				d := decl{path: p, name: name, kind: "function", line: i + 1, exported: exported}
				if strings.Contains(p, "::") {
					d.kind = "method"
				}
				d.span = &cSpan{declLine: i + 1, bodyEnd: bodyEndLine(masked, i), params: params(lines, masked, i)}
				d.span.docStart, d.span.docEnd, d.span.doc = docAbove(lines, i)
				add(d)
				if strings.Contains(m, "{") {
					pending = &scope{kind: "function", name: name}
				}
			case typeScope() != nil && reField.MatchString(t):
				mm := reField.FindStringSubmatch(t)
				ts := typeScope()
				if !keywords[mm[1]] && !strings.HasPrefix(t, "return") {
					add(decl{path: typePath() + "::" + mm[1], name: mm[1], kind: "field", line: i + 1, exported: memberExported(ts)})
				}
			}
		} else if pending == nil && depth > 0 && strings.Contains(m, "{") {
			// a block inside a function body
			pending = &scope{kind: "other"}
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
				for n := len(scopes); n > 0 && !scopes[n-1].macro && scopes[n-1].depth >= depth; n = len(scopes) {
					scopes = scopes[:n-1]
				}
			case ';':
				if pending != nil && paren == 0 && pending.kind != "namespace" {
					pending = nil
				}
			}
		}
	}
	return out
}

// docAbove returns the comment block directly above lines[i] — a run of
// "///" or "//" lines, or a "/** … */" / "/* … */" block — as 1-based first
// and last line and the text with markers removed.
func docAbove(lines []string, i int) (int, int, []string) {
	j := i - 1
	for j >= 0 && (strings.HasPrefix(strings.TrimSpace(lines[j]), "template") || strings.HasPrefix(strings.TrimSpace(lines[j]), "[[")) {
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
		if j < 0 {
			return 0, 0, nil
		}
		var doc []string
		for k := j; k <= last; k++ {
			s := strings.TrimSpace(strings.TrimRight(lines[k], "\r"))
			s = strings.TrimPrefix(strings.TrimPrefix(s, "/**"), "/*")
			s = strings.TrimSuffix(s, "*/")
			s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "*"))
			s = reDoxyTag.ReplaceAllString(s, "")
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
			s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(s, "///"), "//"), " ")
			s = reDoxyTag.ReplaceAllString(s, "")
			doc = append(doc, s)
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
	depth := 0
	opened := false
	last := start + 5000
	if last > len(masked) {
		last = len(masked)
	}
	for j := start; j < last; j++ {
		code := masked[j]
		if strings.HasPrefix(strings.TrimSpace(code), "#") {
			continue
		}
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

// params returns the parameter names of the function declared on
// lines[start]: the last identifier of each comma-separated segment before
// any "=" default, "void" and unnamed types skipped.
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
		if i := strings.IndexByte(s, '['); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
		s = strings.TrimRight(s, " *&")
		f := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(s, "*", " "), "&", " "))
		if len(f) < 2 {
			return // "void", "int", an unnamed parameter
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

// maskAll blanks string literals, char literals and comments (block
// comments across lines included) in every line. Each result has the
// length of its input line.
func maskAll(lines []string) []string {
	out := make([]string, len(lines))
	inBlock := false
	for i, ln := range lines {
		out[i], inBlock = maskLine(strings.TrimRight(ln, "\r"), inBlock)
	}
	return out
}

func maskLine(ln string, inBlock bool) (string, bool) {
	out := []byte(ln)
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inBlock {
			if c == '*' && i+1 < len(out) && out[i+1] == '/' {
				out[i], out[i+1] = ' ', ' '
				i++
				inBlock = false
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
				out[i], out[i+1] = ' ', ' '
				i++
				inBlock = true
			}
		case '"':
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
			j := i + 1
			for j < len(out) && out[j] != '\'' {
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
		}
	}
	return string(out), inBlock
}
