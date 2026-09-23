package odin

import (
	"regexp"
	"sort"
	"strings"

	"docrot/internal/model"
)

// odinSpan is the comment and body geometry of one proc or type
// declaration, captured while the file is scanned. Line numbers are 1-based;
// zero means "none".
type odinSpan struct {
	docStart int
	docEnd   int
	declLine int
	bodyEnd  int
	doc      []string
	params   []string
	exported bool
}

var (
	// reTypeDecl recognises the right hand side of a type declaration.
	reTypeDecl = regexp.MustCompile(`^(distinct\s+)?(struct|enum|union)\b`)
	// reAttrLine recognises a line that is nothing but an attribute.
	reAttrLine = regexp.MustCompile(`^@\([^)]*\)$`)
)

// declSpan describes the declaration starting on lines[start] (0-based).
// isProc selects a procedure, whose parameter list is recorded.
func declSpan(lines []string, start int, isProc bool) *odinSpan {
	sp := &odinSpan{
		declLine: start + 1,
		bodyEnd:  bodyEndLine(lines, start),
		exported: !strings.Contains(attrsOf(lines, start), "@(private"),
	}
	sp.docStart, sp.docEnd, sp.doc = docComment(lines, start)
	if isProc {
		sp.params = procParams(lines, start)
	}
	return sp
}

// attrsOf collects the attribute annotations that apply to a declaration:
// those written in front of it on its own line, and those on the contiguous
// attribute-only lines above it.
func attrsOf(lines []string, start int) string {
	var b strings.Builder
	cur := trimLine(lines[start])
	for {
		m := reAttrPrefix.FindStringSubmatch(cur)
		if m == nil {
			break
		}
		b.WriteString(m[0])
		cur = strings.TrimSpace(cur[len(m[0]):])
	}
	for j := start - 1; j >= 0; j-- {
		t := trimLine(lines[j])
		if !reAttrLine.MatchString(t) {
			break
		}
		b.WriteString(t)
	}
	return b.String()
}

// docComment returns the 1-based first and last line of the run of "//"
// comment lines directly above a declaration — attribute-only lines in
// between are skipped — and their text with the marker and one leading space
// removed. (0, 0, nil) when there is no such comment.
func docComment(lines []string, start int) (int, int, []string) {
	j := start - 1
	for j >= 0 && reAttrLine.MatchString(trimLine(lines[j])) {
		j--
	}
	first, last := -1, -1
	for ; j >= 0; j-- {
		t := trimLine(lines[j])
		if !strings.HasPrefix(t, "//") {
			break
		}
		if last < 0 {
			last = j
		}
		first = j
	}
	if first < 0 {
		return 0, 0, nil
	}
	var doc []string
	for k := first; k <= last; k++ {
		t := strings.TrimRight(trimLine(lines[k])[2:], " \t")
		doc = append(doc, strings.TrimPrefix(t, " "))
	}
	for len(doc) > 0 && strings.TrimSpace(doc[0]) == "" {
		doc = doc[1:]
	}
	for len(doc) > 0 && strings.TrimSpace(doc[len(doc)-1]) == "" {
		doc = doc[:len(doc)-1]
	}
	if len(doc) == 0 {
		return 0, 0, nil
	}
	return first + 1, last + 1, doc
}

// bodyEndLine returns the 1-based line closing the declaration's braces. A
// declaration without a body (a "---" procedure, an alias) ends on its own
// line.
func bodyEndLine(lines []string, start int) int {
	depth := 0
	opened := false
	last := start + 5000
	if last > len(lines) {
		last = len(lines)
	}
	for j := start; j < last; j++ {
		code := maskLine(lines[j])
		if !opened {
			if strings.Contains(code, "---") {
				return j + 1
			}
			if j > start {
				t := strings.TrimSpace(code)
				if t == "" || reDoubleColon.MatchString(t) {
					return start + 1
				}
			}
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
			}
		}
		if opened && depth == 0 {
			return j + 1
		}
	}
	return start + 1
}

// procParams returns the identifiers declared in the parameter list of the
// procedure starting on lines[start]. Names sharing one type ("a, b: int")
// are all reported; "$T: typeid" and "using ctx: Context" are reduced to
// their identifier.
func procParams(lines []string, start int) []string {
	last := start + 200
	if last > len(lines) {
		last = len(lines)
	}
	var raw, masked strings.Builder
	for j := start; j < last; j++ {
		if j > start {
			raw.WriteByte('\n')
			masked.WriteByte('\n')
		}
		ln := strings.TrimRight(lines[j], "\r")
		raw.WriteString(ln)
		masked.WriteString(maskLine(ln))
	}
	r, m := raw.String(), masked.String()

	p := strings.Index(m, "proc")
	if p < 0 {
		return nil
	}
	open := strings.IndexByte(m[p:], '(')
	if open < 0 {
		return nil
	}
	open += p
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
	if closed < 0 {
		return nil
	}
	return paramNames(r[open+1 : closed])
}

// paramNames splits a parameter list on its top-level commas. A segment
// without a ":" is a name sharing the type of a later segment (or, at the
// end of the list, an unnamed type, which is dropped).
func paramNames(text string) []string {
	m := strings.Join(mapLines(strings.Split(text, "\n"), maskLine), "\n")
	var segs []string
	depth, start := 0, 0
	for i := 0; i < len(m) && i < len(text); i++ {
		switch m[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				segs = append(segs, text[start:i])
				start = i + 1
			}
		}
	}
	segs = append(segs, text[start:])

	var out, pending []string
	for _, seg := range segs {
		s := strings.TrimSpace(strings.ReplaceAll(seg, "\n", " "))
		if s == "" {
			continue
		}
		i := strings.IndexByte(s, ':')
		if i < 0 {
			pending = append(pending, s)
			continue
		}
		for _, n := range append(pending, s[:i]) {
			if id := identOf(n); id != "" {
				out = append(out, id)
			}
		}
		pending = nil
	}
	return out
}

// identOf reduces one parameter name to its identifier: "$T" and
// "using ctx" both yield their last word without the "$".
func identOf(s string) string {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) == 0 {
		return ""
	}
	name := strings.TrimPrefix(f[len(f)-1], "$")
	for i, r := range name {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')
		if !ok {
			return ""
		}
	}
	return name
}

// maskLine replaces the contents of string and rune literals, and any "//"
// comment, with spaces. The result has exactly the length of the input.
func maskLine(ln string) string {
	out := []byte(strings.TrimRight(ln, "\r"))
	var quote byte
	for i := 0; i < len(out); i++ {
		c := out[i]
		if quote != 0 {
			if c == '\\' {
				out[i] = ' '
				if i+1 < len(out) {
					out[i+1] = ' '
					i++
				}
				continue
			}
			if c == quote {
				quote = 0
			}
			out[i] = ' '
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
			out[i] = ' '
		case '/':
			if i+1 < len(out) && out[i+1] == '/' {
				for j := i; j < len(out); j++ {
					out[j] = ' '
				}
				return string(out)
			}
		}
	}
	return string(out)
}

// mapLines applies f to every element of in.
func mapLines(in []string, f func(string) string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = f(s)
	}
	return out
}

// trimLine strips a trailing carriage return and surrounding blanks.
func trimLine(s string) string { return strings.TrimSpace(strings.TrimRight(s, "\r")) }

// symbolSpan converts a recognised declaration into the shared model type.
func (s symbol) symbolSpan() model.SymbolSpan {
	return model.SymbolSpan{
		Qualified: s.pkg + "." + s.name,
		Kind:      model.KindOdinSym,
		File:      s.file,
		DocStart:  s.span.docStart,
		DocEnd:    s.span.docEnd,
		DeclLine:  s.span.declLine,
		BodyStart: s.span.declLine,
		BodyEnd:   s.span.bodyEnd,
		Doc:       append([]string(nil), s.span.doc...),
		Params:    append([]string(nil), s.span.params...),
		Exported:  s.span.exported,
	}
}

// Span returns the declaration span of a procedure or type: the "//" comment
// above it, its declaration line and the body its braces enclose. qualified
// is resolved as Has resolves it ("pkg.name" or a bare "name", with a
// trailing call stripped); a bare name that matches several declarations
// returns the first procedure or type found.
func (ix *Index) Span(qualified string) (model.SymbolSpan, bool) {
	for _, s := range ix.lookup(qualified) {
		if s.span != nil {
			return s.symbolSpan(), true
		}
	}
	return model.SymbolSpan{}, false
}

// AllSpans returns one span per procedure and per struct, enum or union
// type, private ones included, sorted by qualified name.
func (ix *Index) AllSpans() []model.SymbolSpan {
	var out []model.SymbolSpan
	for _, s := range ix.all {
		if s.span == nil {
			continue
		}
		out = append(out, s.symbolSpan())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified < out[j].Qualified })
	return out
}
