// Package rst tokenizes reStructuredText into the same Doc shape the
// Markdown parser produces, so that every later docrot stage (extraction,
// anchors, staleness, pairs) works on Sphinx documentation unchanged.
//
// Like the Markdown tokenizer it is line-oriented and deliberately partial:
// section titles with underline/overline adornments, directives
// (code-block, literalinclude, include, image, figure, toctree, the
// autodoc and Python domain directives), explicit targets, literal blocks
// introduced by "::", doctest blocks, grid and simple tables, inline
// literals, roles (:func:, :ref:, :doc:, :file:, :option:, :envvar:, …),
// hyperlink references and bare URLs. Everything else is prose.
package rst

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"docrot/internal/markdown"
)

var (
	reDirective  = regexp.MustCompile(`^(\s*)\.\.\s+([A-Za-z][\w:+.-]*)::\s*(.*)$`)
	reCommentAt  = regexp.MustCompile(`^(\s*)\.\.(?:\s+(.*))?$`)
	reTarget     = regexp.MustCompile("^_(`[^`]+`|[^:`]+):\\s*(.*)$")
	reSubstDef   = regexp.MustCompile(`^\|[^|]+\|\s+\S+::`)
	reOptionLine = regexp.MustCompile(`^\s*:[\w-]+:`)
	reLiteral    = regexp.MustCompile("``([^`]+)``")
	reRole       = regexp.MustCompile(":([A-Za-z][\\w:+.-]*):`([^`]+)`")
	reHyperlink  = regexp.MustCompile("`([^`<>]*?)\\s*<([^<>`]+)>`__?")
	reNamedRef   = regexp.MustCompile("`([^`<>]+)`_")
	reWordRef    = regexp.MustCompile(`(?:^|[\s(])([A-Za-z][\w.-]*[A-Za-z0-9])_(?:[\s.,;:)]|$)`)
	reInterp     = regexp.MustCompile("`([^`\\s]+)`")
	reSubstRef   = regexp.MustCompile(`\|[\w.-]+\|`)
	reGridBorder = regexp.MustCompile(`^\s*\+[-=+]+\+\s*$`)
	reSimpleBrd  = regexp.MustCompile(`^\s*=+(?:\s+=+)+\s*$`)
	reSlugJunk   = regexp.MustCompile(`[^a-z0-9]+`)
	reNonAlnum   = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// Looks reports whether content is probably reStructuredText: a title
// adornment under a line of text, or a directive, within the first lines.
func Looks(content []byte) bool {
	lines := markdown.SplitLines(content)
	if len(lines) > 120 {
		lines = lines[:120]
	}
	for i, l := range lines {
		if reDirective.MatchString(l) || reCommentAt.MatchString(l) && strings.HasPrefix(strings.TrimSpace(l), ".. _") {
			return true
		}
		if i > 0 && isAdornLine(l) && strings.TrimSpace(lines[i-1]) != "" && !isAdornLine(lines[i-1]) &&
			len(strings.TrimRight(l, " \t")) >= len(strings.TrimSpace(lines[i-1])) && indentOf(lines[i-1]) == 0 {
			return true
		}
	}
	return false
}

type parser struct {
	doc   *markdown.Doc
	ext   string // the documentation set's source extension (".rst", or ".txt" as Django uses)
	lines []string
	skip  []bool   // structural or literal lines: not prose
	prose []string // masked prose per line, for numbers
	refs  map[string]string
	pend  []pendingRef // named references waiting for a ".. _name: target"
	slugs map[string]int
}

type pendingRef struct {
	idx  int
	name string
}

// Parse tokenizes content. path is stored verbatim in Doc.Path.
func Parse(path string, content []byte) *markdown.Doc {
	lines := markdown.SplitLines(content)
	ext := strings.ToLower(filepathExt(path))
	if ext == "" {
		ext = ".rst"
	}
	p := &parser{
		doc:   &markdown.Doc{Path: path, Lines: lines},
		ext:   ext,
		lines: lines,
		skip:  make([]bool, len(lines)),
		prose: make([]string, len(lines)),
		refs:  map[string]string{},
		slugs: map[string]int{},
	}
	p.blocks()
	p.inline()
	p.resolvePending()
	sortTokens(p.doc)
	p.doc.SetNumberLines(p.prose)
	p.doc.ApplyIgnores(func(markdown.Comment) bool { return true })
	p.doc.AssignSections()
	return p.doc
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func filepathExt(p string) string {
	if i := strings.LastIndexAny(p, "./"); i >= 0 && p[i] == '.' {
		return p[i:]
	}
	return ""
}

// builtinLabels are the targets Sphinx defines itself.
var builtinLabels = map[string]bool{"genindex": true, "modindex": true, "py-modindex": true, "search": true}

func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " \t")) }

// adornChars are the characters docutils accepts for title adornments.
const adornChars = "=-`:'\"~^_*+#<>."

// adorn reports the adornment character of a title underline/overline: at
// least three copies of one punctuation character and nothing else.
func adorn(s string) (byte, bool) {
	t := strings.TrimRight(s, " \t")
	if len(t) < 3 || strings.IndexByte(adornChars, t[0]) < 0 {
		return 0, false
	}
	for k := 1; k < len(t); k++ {
		if t[k] != t[0] {
			return 0, false
		}
	}
	return t[0], true
}

// blocks walks the document once for structure: titles, directives,
// comments, explicit targets, literal and doctest blocks, tables.
func (p *parser) blocks() {
	n := len(p.lines)
	styles := map[string]int{}
	for i := 0; i < n; i++ {
		if p.skip[i] {
			continue
		}
		line := p.lines[i]
		if blank(line) {
			continue
		}
		// section title: text underlined (and maybe overlined) by an adornment
		if i+1 < n && indentOf(line) == 0 {
			// docutils accepts an underline shorter than the title (with a
			// warning) as long as it is at least four characters
			if c, ok := adorn(p.lines[i+1]); ok && !isAdornLine(line) && (len(strings.TrimRight(p.lines[i+1], " \t")) >= len(strings.TrimSpace(line)) || len(strings.TrimRight(p.lines[i+1], " \t")) >= 4) {
				over := false
				if i > 0 {
					if oc, ok := adorn(p.lines[i-1]); ok && oc == c {
						over = true
						p.skip[i-1] = true
					}
				}
				key := string(c)
				if over {
					key += "o"
				}
				lvl, seen := styles[key]
				if !seen {
					lvl = len(styles) + 1
					styles[key] = lvl
				}
				text := strings.TrimSpace(line)
				p.doc.Headings = append(p.doc.Headings, markdown.Heading{Line: i + 1, Level: lvl, Text: text, Slug: p.slug(text)})
				p.skip[i+1] = true
				i++
				continue
			}
		}
		if isAdornLine(line) {
			// an overline: the title branch handles it from the next line
			if i+2 < n && !blank(p.lines[i+1]) && !isAdornLine(p.lines[i+1]) {
				if c, ok := adorn(p.lines[i+2]); ok && c == line[indentOf(line)] {
					continue
				}
			}
			// a transition on its own
			p.skip[i] = true
			continue
		}
		// explicit markup: directives, targets, substitutions, comments
		if m := reDirective.FindStringSubmatch(line); m != nil {
			i = p.directive(i, len(m[1]), strings.ToLower(m[2]), strings.TrimSpace(m[3]))
			continue
		}
		if m := reCommentAt.FindStringSubmatch(line); m != nil {
			i = p.explicit(i, len(m[1]), strings.TrimSpace(m[2]))
			continue
		}
		// tables
		if reGridBorder.MatchString(line) {
			i = p.gridTable(i)
			continue
		}
		if reSimpleBrd.MatchString(line) {
			i = p.simpleTable(i)
			continue
		}
		// doctest block
		if strings.HasPrefix(strings.TrimSpace(line), ">>> ") {
			i = p.doctest(i)
			continue
		}
		// paragraph ending in "::" introduces a literal block
		if t := strings.TrimRight(line, " \t"); strings.HasSuffix(t, "::") {
			if strings.TrimSpace(t) == "::" {
				p.skip[i] = true
			} else {
				p.lines[i] = strings.TrimSuffix(t, "::") + " " // keep the prose, drop the marker
				p.doc.Lines[i] = p.lines[i]
			}
			i = p.literalBlock(i, indentOf(line), "")
			continue
		}
	}
}

func isAdornLine(s string) bool { _, ok := adorn(s); return ok }

// sortTokens puts spans and links in document order: directives are
// collected in the block pass, inline markup afterwards.
func sortTokens(d *markdown.Doc) {
	sort.SliceStable(d.Spans, func(i, j int) bool {
		if d.Spans[i].Line != d.Spans[j].Line {
			return d.Spans[i].Line < d.Spans[j].Line
		}
		return d.Spans[i].Col < d.Spans[j].Col
	})
	sort.SliceStable(d.Links, func(i, j int) bool {
		if d.Links[i].Line != d.Links[j].Line {
			return d.Links[i].Line < d.Links[j].Line
		}
		return d.Links[i].Col < d.Links[j].Col
	})
}

// body returns the last index (inclusive) of the indented block that
// follows line i, whose own indent is w: every following line that is blank
// or indented deeper than w. Trailing blank lines are not included.
func (p *parser) body(i, w int) int {
	last := i
	for j := i + 1; j < len(p.lines); j++ {
		l := p.lines[j]
		if blank(l) {
			continue
		}
		if indentOf(l) <= w {
			break
		}
		last = j
	}
	return last
}

func (p *parser) markSkip(from, to int) {
	for j := from; j <= to && j < len(p.skip); j++ {
		p.skip[j] = true
	}
}

// dedent returns lines[from..to] with the common leading indentation
// removed, leading option lines (":linenos:") and the blank after them
// dropped.
func (p *parser) dedent(from, to int) []string {
	var raw []string
	for j := from; j <= to; j++ {
		raw = append(raw, p.lines[j])
	}
	// drop option lines at the top
	for len(raw) > 0 && (reOptionLine.MatchString(raw[0]) || blank(raw[0])) {
		if blank(raw[0]) && (len(raw) == 1 || !reOptionLine.MatchString(raw[1])) {
			// the blank that separates options from content, or a leading blank
			raw = raw[1:]
			break
		}
		raw = raw[1:]
	}
	min := -1
	for _, l := range raw {
		if blank(l) {
			continue
		}
		if w := indentOf(l); min < 0 || w < min {
			min = w
		}
	}
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		if len(l) >= min && min > 0 {
			out = append(out, l[min:])
		} else {
			out = append(out, strings.TrimLeft(l, " \t"))
		}
	}
	return out
}

// symbolDirectives document or declare a Python/C object by name: the
// name is a claim about the code.
var symbolDirectives = map[string]bool{
	"function": true, "class": true, "method": true, "exception": true, "attribute": true, "data": true,
	"module": true, "currentmodule": true, "decorator": true, "property": true,
	"autofunction": true, "autoclass": true, "automethod": true, "autoexception": true,
	"autoattribute": true, "autodata": true, "automodule": true, "autodecorator": true, "autoproperty": true,
}

// opaqueDirectives have bodies that are not prose.
var opaqueDirectives = map[string]bool{"raw": true, "math": true, "index": true, "contents": true, "csv-table": true, "list-table": false}

func (p *parser) directive(i, w int, name, arg string) int {
	end := p.body(i, w)
	name = strings.TrimPrefix(name, "py:")
	switch {
	case name == "code-block" || name == "code" || name == "sourcecode" || name == "parsed-literal":
		lang := ""
		if f := strings.Fields(arg); len(f) > 0 {
			lang = strings.ToLower(f[0])
		}
		p.markSkip(i, end)
		if end > i {
			p.doc.Fences = append(p.doc.Fences, markdown.Fence{StartLine: i + 1, EndLine: end + 1, Lang: lang, Info: arg, Content: p.dedent(i+1, end)})
		}
		return end
	case name == "literalinclude" || name == "include" || name == "download":
		p.markSkip(i, end)
		p.addLink(markdown.Link{Line: i + 1, Col: 1, Text: name, Target: arg})
		return end
	case name == "image" || name == "figure":
		p.skip[i] = true
		p.doc.Images = append(p.doc.Images, markdown.Link{Line: i + 1, Col: 1, Text: name, Target: arg, IsImage: true})
		return i // a figure's caption is prose
	case name == "toctree":
		p.markSkip(i, end)
		glob := false
		for j := i + 1; j <= end; j++ {
			t := strings.TrimSpace(p.lines[j])
			if t == "" {
				continue
			}
			if strings.HasPrefix(t, ":") {
				if strings.HasPrefix(t, ":glob:") {
					glob = true
				}
				continue
			}
			target := t
			if k := strings.LastIndex(t, "<"); k >= 0 && strings.HasSuffix(t, ">") {
				target = strings.TrimSpace(t[k+1 : len(t)-1])
			}
			if glob && strings.ContainsAny(target, "*?") || strings.Contains(target, "://") {
				if strings.Contains(target, "://") {
					p.addLink(markdown.Link{Line: j + 1, Col: 1, Target: target})
				}
				continue
			}
			p.addLink(markdown.Link{Line: j + 1, Col: 1, Text: "toctree", Target: p.docTarget(target)})
		}
		return end
	case symbolDirectives[name]:
		p.skip[i] = true
		if arg != "" {
			col := strings.Index(p.lines[i], arg) + 1
			p.doc.Spans = append(p.doc.Spans, markdown.Span{Line: i + 1, Col: col, Text: arg, Kind: "code"})
		}
		return i // options and the description are prose
	case opaqueDirectives[name]:
		p.markSkip(i, end)
		return end
	}
	p.skip[i] = true // an admonition, versionadded, note…: its body is prose
	return i
}

// docTarget turns a :doc: / toctree target into a path: the source
// extension of this documentation set is added when the target has none.
// A leading "/" (relative to the Sphinx source root, wherever that is) is
// kept; the extractor marks such paths as rooted and the resolver climbs
// the document's ancestors for them.
func (p *parser) docTarget(t string) string {
	t = strings.TrimSpace(t)
	low := strings.ToLower(t)
	for _, ext := range []string{".rst", ".rest", ".txt", ".md", ".html"} {
		if strings.HasSuffix(low, ext) {
			return t
		}
	}
	return t + p.ext // "releases/1.2" is a document, not a file with a ".2" extension
}

// explicit handles ".. " lines that are not directives: targets,
// substitution definitions, docrot directives and comments.
func (p *parser) explicit(i, w int, text string) int {
	end := p.body(i, w)
	if m := reTarget.FindStringSubmatch(text); m != nil {
		label := strings.Trim(m[1], "`")
		target := strings.TrimSpace(m[2])
		p.skip[i] = true
		if target == "" {
			p.doc.Labels = append(p.doc.Labels, strings.ToLower(label))
			return i
		}
		if target == "_" || strings.HasSuffix(target, "_") && !strings.Contains(target, "/") && !strings.Contains(target, ":") {
			return i // an indirect target: alias of another reference
		}
		p.refs[strings.ToLower(label)] = target
		p.doc.RefDefs = append(p.doc.RefDefs, markdown.Link{Line: i + 1, Col: 1, Text: label, Target: target})
		return i
	}
	if strings.HasPrefix(text, "__:") || reSubstDef.MatchString(text) {
		p.markSkip(i, end)
		return end
	}
	if strings.HasPrefix(text, "docrot:") {
		p.skip[i] = true
		p.doc.Comments = append(p.doc.Comments, markdown.Comment{StartLine: i + 1, EndLine: i + 1, Text: text})
		return i
	}
	// a comment block: this line and everything indented under it
	p.markSkip(i, end)
	var parts []string
	for j := i; j <= end; j++ {
		parts = append(parts, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(p.lines[j]), "..")))
	}
	p.doc.Comments = append(p.doc.Comments, markdown.Comment{StartLine: i + 1, EndLine: end + 1, Text: strings.TrimSpace(strings.Join(parts, "\n"))})
	return end
}

// literalBlock records the indented block after a "::" paragraph as a
// plain fence.
func (p *parser) literalBlock(i, w int, lang string) int {
	end := p.body(i, w)
	if end == i {
		return i
	}
	first := i + 1
	for first <= end && blank(p.lines[first]) {
		first++
	}
	p.markSkip(first, end)
	p.doc.Fences = append(p.doc.Fences, markdown.Fence{StartLine: first, EndLine: end + 1, Lang: lang, Content: p.dedent(first, end)})
	return end
}

// doctest records a ">>>" block as a pycon fence.
func (p *parser) doctest(i int) int {
	end := i
	for j := i + 1; j < len(p.lines) && !blank(p.lines[j]); j++ {
		end = j
	}
	p.markSkip(i, end)
	p.doc.Fences = append(p.doc.Fences, markdown.Fence{StartLine: i, EndLine: end + 1, Lang: "pycon", Content: p.dedent(i, end)})
	return end
}

// gridTable measures a "+---+" table; its cell lines stay prose.
func (p *parser) gridTable(i int) int {
	cols := strings.Count(p.lines[i], "+") - 1
	rows := 0
	end := i
	for j := i + 1; j < len(p.lines); j++ {
		t := strings.TrimSpace(p.lines[j])
		if !strings.HasPrefix(t, "+") && !strings.HasPrefix(t, "|") {
			break
		}
		end = j
		if strings.HasPrefix(t, "+-") {
			rows++
		}
		if strings.HasPrefix(t, "+=") {
			rows = 0 // rows above the header separator are the header
		}
	}
	p.skip[i] = true
	for j := i + 1; j <= end; j++ {
		if strings.HasPrefix(strings.TrimSpace(p.lines[j]), "+") {
			p.skip[j] = true
		}
	}
	p.doc.Tables = append(p.doc.Tables, markdown.Table{StartLine: i + 1, EndLine: end + 1, Rows: rows, Cols: cols})
	return end
}

// simpleTable measures a "====  ====" table; its lines stay prose.
func (p *parser) simpleTable(i int) int {
	cols := len(strings.Fields(p.lines[i]))
	var borders []int
	for j := i; j < len(p.lines); j++ {
		if reSimpleBrd.MatchString(p.lines[j]) {
			borders = append(borders, j)
			p.skip[j] = true
			continue
		}
		if blank(p.lines[j]) && len(borders) >= 2 {
			break
		}
	}
	if len(borders) < 2 {
		return i
	}
	last := borders[len(borders)-1]
	rows := 0
	for j := borders[len(borders)-2] + 1; j < last; j++ {
		if !blank(p.lines[j]) {
			rows++
		}
	}
	p.doc.Tables = append(p.doc.Tables, markdown.Table{StartLine: i + 1, EndLine: last + 1, Rows: rows, Cols: cols})
	return last
}

func (p *parser) addLink(lk markdown.Link) int {
	p.doc.Links = append(p.doc.Links, lk)
	return len(p.doc.Links) - 1
}

// symbolRoles are the Sphinx roles whose target names a code object.
var symbolRoles = map[string]bool{
	"func": true, "function": true, "class": true, "meth": true, "method": true, "mod": true, "module": true,
	"data": true, "attr": true, "attribute": true, "exc": true, "exception": true, "obj": true, "const": true,
	"any": true, "deco": true, "decorator": true, "prop": true, "property": true, "type": true,
	"c:func": true, "c:type": true, "c:data": true, "c:macro": true, "c:member": true, "c:var": true,
	"cpp:func": true, "cpp:class": true, "js:func": true, "js:class": true,
}

// inline scans the prose lines for literals, roles, references and URLs,
// masking each so that bare-path and number extraction do not see them.
func (p *parser) inline() {
	for i, line := range p.lines {
		if p.skip[i] {
			continue
		}
		masked := []byte(line)
		wipe := func(a, b int) {
			for k := a; k < b && k < len(masked); k++ {
				masked[k] = ' '
			}
		}
		// ``literal``
		for _, m := range reLiteral.FindAllStringSubmatchIndex(line, -1) {
			p.doc.Spans = append(p.doc.Spans, markdown.Span{Line: i + 1, Col: m[2] + 1, Text: strings.TrimSpace(line[m[2]:m[3]]), Kind: "code"})
			wipe(m[0], m[1])
		}
		// :role:`text` and :role:`title <target>`
		cur := string(masked)
		for _, m := range reRole.FindAllStringSubmatchIndex(cur, -1) {
			role := strings.ToLower(cur[m[2]:m[3]])
			body := strings.TrimSpace(cur[m[4]:m[5]])
			col := m[4] + 1
			target := body
			if k := strings.LastIndex(body, "<"); k >= 0 && strings.HasSuffix(body, ">") {
				target = strings.TrimSpace(body[k+1 : len(body)-1])
				col += k + 1
			}
			target = strings.TrimLeft(target, "~!")
			role = strings.TrimPrefix(role, "py:")
			switch {
			case symbolRoles[role], role == "option", role == "envvar", role == "file", role == "command", role == "program", role == "samp", role == "code", role == "literal":
				if target != "" {
					p.doc.Spans = append(p.doc.Spans, markdown.Span{Line: i + 1, Col: col, Text: target, Kind: "code"})
				}
			case role == "doc":
				p.addLink(markdown.Link{Line: i + 1, Col: m[0] + 1, Text: body, Target: p.docTarget(target)})
			case role == "ref", role == "numref":
				label := strings.ToLower(target)
				switch {
				case builtinLabels[label]:
				case strings.Contains(label, ":") && !strings.Contains(label, "://"):
					// autosectionlabel: "docname:Section Title"
					docname, title, _ := strings.Cut(target, ":")
					p.addLink(markdown.Link{Line: i + 1, Col: m[0] + 1, Text: ":ref:", Target: "/" + strings.TrimPrefix(p.docTarget(docname), "/") + "#" + slugOf(title)})
				default:
					// Text ":ref:" tells the extractor this is a label that may live
					// in another project's inventory (intersphinx): medium, not high
					p.addLink(markdown.Link{Line: i + 1, Col: m[0] + 1, Text: ":ref:", Target: "#" + label})
				}
			case role == "download":
				p.addLink(markdown.Link{Line: i + 1, Col: m[0] + 1, Text: body, Target: target})
			}
			wipe(m[0], m[1])
		}
		// `text <target>`_ and `text <target>`__
		cur = string(masked)
		for _, m := range reHyperlink.FindAllStringSubmatchIndex(cur, -1) {
			p.addLink(markdown.Link{Line: i + 1, Col: m[0] + 1, Text: strings.TrimSpace(cur[m[2]:m[3]]), Target: strings.TrimSpace(cur[m[4]:m[5]])})
			wipe(m[0], m[1])
		}
		// `name`_ and name_ : resolved against ".. _name: target"
		cur = string(masked)
		for _, m := range reNamedRef.FindAllStringSubmatchIndex(cur, -1) {
			idx := p.addLink(markdown.Link{Line: i + 1, Col: m[0] + 1, Text: cur[m[2]:m[3]]})
			p.pend = append(p.pend, pendingRef{idx: idx, name: strings.ToLower(strings.TrimSpace(cur[m[2]:m[3]]))})
			wipe(m[0], m[1])
		}
		cur = string(masked)
		for _, m := range reWordRef.FindAllStringSubmatchIndex(cur, -1) {
			name := cur[m[2]:m[3]]
			idx := p.addLink(markdown.Link{Line: i + 1, Col: m[2] + 1, Text: name})
			p.pend = append(p.pend, pendingRef{idx: idx, name: strings.ToLower(name)})
			wipe(m[2], m[3]+1)
		}
		// `identifier` with the default role: usually a code object
		cur = string(masked)
		for _, m := range reInterp.FindAllStringSubmatchIndex(cur, -1) {
			p.doc.Spans = append(p.doc.Spans, markdown.Span{Line: i + 1, Col: m[2] + 1, Text: cur[m[2]:m[3]], Kind: "code"})
			wipe(m[0], m[1])
		}
		// |substitution|
		for _, m := range reSubstRef.FindAllStringIndex(string(masked), -1) {
			wipe(m[0], m[1])
		}
		// bare URLs
		cur = string(masked)
		for _, r := range markdown.FindBareURLs(cur) {
			p.addLink(markdown.Link{Line: i + 1, Col: r[0] + 1, Target: cur[r[0]:r[1]]})
			wipe(r[0], r[1])
		}
		cur = string(masked)
		p.doc.BarePaths = append(p.doc.BarePaths, markdown.BarePathSpans(cur, i+1)...)
		p.prose[i] = cur
	}
}

// resolvePending fills in named references from the explicit targets and
// drops the ones that were never defined (they are Sphinx roles or prose).
func (p *parser) resolvePending() {
	drop := map[int]bool{}
	for _, pr := range p.pend {
		t, ok := p.refs[pr.name]
		if !ok || t == "" {
			drop[pr.idx] = true
			continue
		}
		p.doc.Links[pr.idx].Target = t
	}
	if len(drop) == 0 {
		return
	}
	kept := p.doc.Links[:0]
	for i, l := range p.doc.Links {
		if !drop[i] {
			kept = append(kept, l)
		}
	}
	p.doc.Links = kept
}

// slugOf builds the section id docutils would: lower-case, runs of
// non-alphanumerics become "-", leading/trailing hyphens and a leading
// run of digits are dropped.
func slugOf(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	if isASCII(s) {
		s = reSlugJunk.ReplaceAllString(s, "-")
	} else {
		s = reNonAlnum.ReplaceAllString(s, "-")
	}
	s = strings.Trim(s, "-")
	for len(s) > 0 && (s[0] >= '0' && s[0] <= '9' || s[0] == '-') {
		s = s[1:]
	}
	if s == "" {
		s = "section"
	}
	return s
}

// slug is slugOf with "-1", "-2" suffixes for duplicate titles.
func (p *parser) slug(title string) string {
	s := slugOf(title)
	n := p.slugs[s]
	p.slugs[s] = n + 1
	if n > 0 {
		return s + "-" + itoa(n)
	}
	return s
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
