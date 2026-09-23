package py

import (
	"sort"
	"strings"

	"docrot/internal/model"
)

// pySpan is the docstring and body geometry of one def or class, captured
// while the file is scanned. Line numbers are 1-based; zero means "none".
type pySpan struct {
	docStart int
	docEnd   int
	declLine int
	bodyEnd  int
	doc      []string
	params   []string
}

// declSpan describes the declaration that starts on lines[start] (0-based),
// whose "def"/"class" keyword is at the given indentation. isDef selects a
// def, whose parameter list is recorded; a class's base list is not.
func declSpan(lines []string, start, indent int, isDef bool) *pySpan {
	sigEnd, paramText := signatureEnd(lines, start)
	sp := &pySpan{
		declLine: start + 1,
		bodyEnd:  bodyEndLine(lines, sigEnd, indent),
	}
	if isDef {
		sp.params = paramNames(paramText)
	}
	sp.docStart, sp.docEnd, sp.doc = docstring(lines, sigEnd+1)
	return sp
}

// signatureEnd returns the 0-based line holding the ":" that ends the
// signature (the declaration line itself when none is found) together with
// the raw text of the first top-level parenthesised list.
func signatureEnd(lines []string, start int) (int, string) {
	last := start + 200
	if last > len(lines) {
		last = len(lines)
	}
	var raw strings.Builder
	for j := start; j < last; j++ {
		if j > start {
			raw.WriteByte('\n')
		}
		raw.WriteString(strings.TrimRight(lines[j], "\r"))
	}
	r := raw.String()
	m := maskBlock(r)

	depth, open, closed, colon := 0, -1, -1, -1
	for i := 0; i < len(m) && colon < 0; i++ {
		switch m[i] {
		case '(':
			if depth == 0 && open < 0 {
				open = i
			}
			depth++
		case '[', '{':
			depth++
		case ')':
			depth--
			if depth == 0 && open >= 0 && closed < 0 {
				closed = i
			}
		case ']', '}':
			depth--
		case ':':
			if depth == 0 {
				colon = i
			}
		}
	}
	if colon < 0 {
		return start, ""
	}
	end := start + strings.Count(m[:colon], "\n")
	if open >= 0 && closed > open && closed < colon {
		return end, r[open+1 : closed]
	}
	return end, ""
}

// maskBlock masks string literals across several lines, including
// triple-quoted strings that span lines (FastAPI writes parameter docs as
// Annotated[..., Doc("""...""")] inside the signature). Newlines are kept
// so line arithmetic on the result stays valid.
func maskBlock(text string) string {
	out := []byte(text)
	var quote string // "", `"`, `'`, `"""`, `'''`
	for i := 0; i < len(out); i++ {
		c := out[i]
		if quote != "" {
			if c == '\n' {
				if len(quote) == 1 {
					quote = "" // a single-quoted string cannot cross a line
				}
				continue
			}
			if c == '\\' && i+1 < len(out) {
				out[i] = ' '
				if out[i+1] != '\n' {
					out[i+1] = ' '
				}
				i++
				continue
			}
			if strings.HasPrefix(string(out[i:]), quote) {
				for k := 0; k < len(quote); k++ {
					out[i+k] = ' '
				}
				i += len(quote) - 1
				quote = ""
				continue
			}
			out[i] = ' '
			continue
		}
		switch {
		case c == '#':
			for i < len(out) && out[i] != '\n' {
				out[i] = ' '
				i++
			}
		case strings.HasPrefix(string(out[i:]), `"""`) || strings.HasPrefix(string(out[i:]), `'''`):
			quote = string(out[i : i+3])
			out[i], out[i+1], out[i+2] = ' ', ' ', ' '
			i += 2
		case c == '"' || c == '\'':
			quote = string(c)
			out[i] = ' '
		}
	}
	return string(out)
}

// maskLine replaces the contents of string literals, and any trailing
// comment, with spaces so that brackets, commas and colons inside them are
// invisible. The result has exactly the length of the input.
func maskLine(ln string) string {
	out := []byte(ln)
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
		case '\'', '"':
			quote = c
			out[i] = ' '
		case '#':
			for j := i; j < len(out); j++ {
				out[j] = ' '
			}
			return string(out)
		}
	}
	return string(out)
}

// paramNames splits a parameter list on its top-level commas and returns the
// declared names, "self" and "cls" included. Annotations and defaults are
// dropped, "*args"/"**kwargs" become "args"/"kwargs", and the bare "*" and
// "/" markers are skipped.
func paramNames(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	m := strings.Join(mapLines(strings.Split(text, "\n"), maskLine), "\n")
	var out []string
	add := func(seg string) {
		if n := paramName(seg); n != "" {
			out = append(out, n)
		}
	}
	depth, start := 0, 0
	for i := 0; i < len(m) && i < len(text); i++ {
		switch m[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				add(text[start:i])
				start = i + 1
			}
		}
	}
	add(text[start:])
	return out
}

// paramName reduces one parameter to its identifier, "" when there is none.
func paramName(seg string) string {
	s := strings.TrimSpace(strings.ReplaceAll(seg, "\n", " "))
	s = strings.TrimSpace(strings.TrimLeft(s, "*"))
	if i := strings.IndexAny(s, ":="); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "" || !isIdent(s) {
		return ""
	}
	return s
}

// mapLines applies f to every element of in.
func mapLines(in []string, f func(string) string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = f(s)
	}
	return out
}

// docstring finds the triple-quoted string that opens the body at or after
// the 0-based line from. It returns the 1-based first and last line of the
// literal and its text with the quote markers removed; (0, 0, nil) when the
// declaration has no docstring.
func docstring(lines []string, from int) (int, int, []string) {
	for j := from; j < len(lines); j++ {
		t := strings.TrimSpace(strings.TrimRight(lines[j], "\r"))
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		rest, quote, ok := cutQuote(t)
		if !ok {
			return 0, 0, nil
		}
		if i := strings.Index(rest, quote); i >= 0 {
			return j + 1, j + 1, docLines([]string{rest[:i]})
		}
		body := []string{rest}
		for k := j + 1; k < len(lines); k++ {
			ln := strings.TrimRight(lines[k], "\r")
			if i := strings.Index(ln, quote); i >= 0 {
				body = append(body, ln[:i])
				return j + 1, k + 1, docLines(body)
			}
			body = append(body, ln)
		}
		return j + 1, len(lines), docLines(body)
	}
	return 0, 0, nil
}

// cutQuote strips an optional string prefix (r, u, f, b or a pair of them)
// and an opening triple quote from a line, returning the rest of the line
// and the quote that closes the literal.
func cutQuote(t string) (rest, quote string, ok bool) {
	s := t
	for len(s) > 0 && len(t)-len(s) < 2 && strings.IndexByte("rRuUfFbB", s[0]) >= 0 {
		s = s[1:]
	}
	for _, q := range []string{`"""`, `'''`} {
		if strings.HasPrefix(s, q) {
			return s[len(q):], q, true
		}
	}
	return "", "", false
}

// docLines trims each docstring line and drops blank lines at either end.
func docLines(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, strings.TrimSpace(s))
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// bodyEndLine returns the 1-based last non-blank line indented deeper than
// the declaration, or the signature's own last line when the body is empty
// or written on the signature line.
func bodyEndLine(lines []string, sigEnd, indent int) int {
	end := sigEnd
	for j := sigEnd + 1; j < len(lines); j++ {
		ln := strings.TrimRight(lines[j], "\r")
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if len(ln)-len(strings.TrimLeft(ln, " \t")) <= indent {
			break
		}
		end = j
	}
	return end + 1
}

// symbolSpan converts a recognised declaration into the shared model type.
func (s symbol) symbolSpan() model.SymbolSpan {
	qualified := s.qualified
	if s.module != "" {
		qualified = s.module + "." + s.qualified
	}
	return model.SymbolSpan{
		Qualified: qualified,
		Kind:      model.KindPySym,
		File:      s.file,
		DocStart:  s.span.docStart,
		DocEnd:    s.span.docEnd,
		DeclLine:  s.span.declLine,
		BodyStart: s.span.declLine,
		BodyEnd:   s.span.bodyEnd,
		Doc:       append([]string(nil), s.span.doc...),
		Params:    append([]string(nil), s.span.params...),
		Exported:  !strings.HasPrefix(s.name, "_"),
	}
}

// Span returns the declaration span of a def, async def or class: its
// docstring, the signature line and the indented body. qualified is resolved
// exactly as Has resolves it ("module.name", "Class.method",
// "module.Class.method" or a bare name, with a trailing call stripped); when
// several declarations match, the first that is a def or class wins.
func (ix *Index) Span(qualified string) (model.SymbolSpan, bool) {
	for _, s := range ix.resolve(qualified) {
		if s.span != nil {
			return s.symbolSpan(), true
		}
	}
	return model.SymbolSpan{}, false
}

// AllSpans returns one span per exported module-level def or class and per
// exported method of a class, excluding modules that live under a tests,
// docs or examples tree. The result is sorted by qualified name.
func (ix *Index) AllSpans() []model.SymbolSpan {
	var out []model.SymbolSpan
	for _, s := range ix.all {
		if s.span == nil || ix.example[s.module] {
			continue
		}
		sp := s.symbolSpan()
		if !sp.Exported {
			continue
		}
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified < out[j].Qualified })
	return out
}
