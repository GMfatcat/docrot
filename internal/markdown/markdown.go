// Package markdown implements a fast, line-oriented Markdown tokenizer.
//
// It is deliberately not a CommonMark parser. docrot only needs to know
// where headings, fenced code blocks, inline code spans, links, images,
// tables and HTML comments live, which sections they belong to, and which
// lines an author asked docrot to ignore. Everything else (emphasis, block
// quotes, list structure, HTML rendering) is left alone.
//
// The tokenizer is tuned for real-world README files: CJK text and emoji in
// headings, fences nested inside list items, four-or-more backtick fences,
// raw HTML blocks such as <details>, GFM tables and multi-line HTML
// comments all parse without surprises. Positions are byte offsets so that
// they line up with compiler-style "file:line:col" output.
//
// Parse never fails and never panics: malformed input degrades to prose.
package markdown

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
)

// Heading is one ATX (`# x`) or setext (`x` underlined by `===`) heading.
type Heading struct {
	Line  int    // 1-based line of the heading text
	Level int    // 1..6
	Text  string // heading text, trimmed, without the closing '#' run
	Slug  string // GitHub anchor slug, with "-1"/"-2" duplicate suffixes
}

// Fence is one fenced code block.
//
// EndLine is the line of the closing fence marker, or the last line of the
// document when the fence is never closed. Content holds the lines between
// the markers with the opening fence's indentation removed.
type Fence struct {
	StartLine int
	EndLine   int
	Lang      string // first word of the info string, lower-cased
	Info      string // full info string, trimmed
	Content   []string
}

// Span is an inline code span ("code") or a bare path found in prose
// ("bare"). Col is the 1-based byte column of the first character after the
// opening backticks (for "code") or of the token itself (for "bare").
type Span struct {
	Line    int
	Col     int
	Text    string
	Section string // nearest heading text at or above Line ("" if none)
	Kind    string // "code" | "bare"
}

// Link is an inline link, an image, an autolink, a bare URL in prose or a
// reference definition. Col is the 1-based byte column where the construct
// starts. Autolinks and bare URLs carry an empty Text. For a reference
// definition ("[id]: target") Text is the id.
type Link struct {
	Line    int
	Col     int
	Text    string
	Target  string
	Title   string
	IsImage bool
	Section string
}

// Table is one GFM pipe table. Rows counts body rows only (the header and
// the delimiter row are excluded); Cols is the number of header cells.
type Table struct {
	StartLine int
	EndLine   int
	Rows      int
	Cols      int
}

// Comment is an HTML comment, possibly spanning several lines. Text is the
// content between "<!--" and "-->", trimmed; inner lines are joined with
// "\n".
type Comment struct {
	StartLine int
	EndLine   int
	Text      string
}

// Doc is the tokenized form of one Markdown document.
type Doc struct {
	Path     string   // as given by the caller (relative, forward slashes)
	Lines    []string // raw lines, without line endings
	Headings []Heading
	Fences   []Fence
	Spans    []Span // inline code spans, in document order
	Links    []Link
	Images   []Link // same struct, IsImage == true
	RefDefs  []Link // "[id]: target" definitions; Text is the id
	Tables   []Table
	Comments []Comment

	// Ignored reports whether a 1-based line is excluded by a
	// docrot:ignore directive. It is never nil.
	Ignored func(line int) bool

	// RuleIgnored reports whether findings of one rule are excluded on a
	// line by a rule-scoped directive (<!-- docrot:ignore missing-path -->,
	// <!-- docrot:ignore-start unknown-flag,unknown-env -->). Such a
	// directive does not affect other rules and does not set Ignored. It
	// is never nil.
	RuleIgnored func(line int, rule string) bool

	// IgnoreFile is set by a <!-- docrot:ignore-file --> comment.
	IgnoreFile bool

	// Labels are explicit anchor targets that are global to a documentation
	// set rather than local to the page: reStructuredText ".. _label:" and
	// AsciiDoc "[[id]]". The anchors index accepts them from any document.
	Labels []string

	// BarePaths holds prose tokens that look like file paths. Kind is
	// always "bare".
	BarePaths []Span

	// numLines holds, per line, the prose text used for number
	// extraction: fence and front-matter lines are blank and link targets
	// are masked out.
	numLines []string
}

// rng is a half-open byte range [a, b) inside a line.
type rng struct{ a, b int }

func (r rng) contains(i int) bool { return i >= r.a && i < r.b }

// Parse tokenizes content as Markdown. path is stored verbatim in Doc.Path.
// Both "\n" and "\r\n" line endings are accepted.
func Parse(path string, content []byte) *Doc {
	p := &parser{doc: &Doc{Path: path, Lines: splitLines(content)}}
	n := len(p.doc.Lines)
	p.text = make([]string, n)
	p.code = make([][]rng, n)
	p.links = make([][]rng, n)
	p.targets = make([][]rng, n)
	p.skip = make([]bool, n)
	p.table = make([]bool, n)
	p.refDef = make([]bool, n)
	p.heading = make([]bool, n)

	p.frontMatter()
	p.scanBlocks()
	p.scanTables()
	p.scanHeadings()
	p.scanRefDefs()
	p.scanLinks()
	p.scanProse()
	p.assignSections()
	p.applyIgnores()
	return p.doc
}

// SectionAt returns the text of the nearest heading at or above line, or ""
// when line precedes every heading.
func (d *Doc) SectionAt(line int) string {
	hs := d.Headings
	i := sort.Search(len(hs), func(i int) bool { return hs[i].Line > line })
	if i == 0 {
		return ""
	}
	return hs[i-1].Text
}

// splitLines splits content into lines, dropping "\n" and "\r\n" endings.
// A trailing newline does not produce a final empty line.
func splitLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	b := bytes.TrimSuffix(content, []byte("\n"))
	b = bytes.TrimSuffix(b, []byte("\r"))
	raw := bytes.Split(b, []byte("\n"))
	out := make([]string, len(raw))
	for i, r := range raw {
		out[i] = string(bytes.TrimSuffix(r, []byte("\r")))
	}
	return out
}

type parser struct {
	doc *Doc

	text    []string // comment-masked source; "" for fence and front-matter lines
	code    [][]rng  // inline code span extents (including backticks)
	links   [][]rng  // whole link/image/URL extents
	targets [][]rng  // link destination extents only
	skip    []bool   // fence marker, fence content or front matter
	table   []bool   // part of a GFM table
	refDef  []bool   // "[id]: target" definition line
	heading []bool   // ATX heading or setext underline

	// inline comment state, carried across lines
	inComment    bool
	commentStart int
	commentParts []string

	pending []pendingRef
}

type pendingRef struct {
	idx   int // index into doc.Links or doc.Images
	image bool
	id    string
}

// frontMatter marks a leading "---" YAML block so that its closing "---" is
// not mistaken for a setext heading.
func (p *parser) frontMatter() {
	lines := p.doc.Lines
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return
	}
	for i := 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "---" || t == "..." {
			for j := 0; j <= i; j++ {
				p.skip[j] = true
			}
			return
		}
	}
}

type openFence struct {
	start  int // 1-based
	char   byte
	length int
	indent int
	info   string
	body   []string
}

// scanBlocks walks the document once, splitting it into fenced code blocks,
// HTML comments and ordinary text. It fills text, code and Spans.
func (p *parser) scanBlocks() {
	var fence *openFence
	for i, line := range p.doc.Lines {
		if p.skip[i] && fence == nil && !p.inComment {
			continue // front matter
		}
		if !p.inComment {
			if fence != nil {
				if fenceCloses(line, fence) {
					p.closeFence(fence, i+1)
					fence = nil
				} else {
					fence.body = append(fence.body, stripIndent(line, fence.indent))
				}
				p.skip[i] = true
				continue
			}
			if f := openFenceAt(line); f != nil {
				f.start = i + 1
				fence = f
				p.skip[i] = true
				continue
			}
		}
		p.scanInline(i, line)
	}
	if fence != nil {
		p.closeFence(fence, len(p.doc.Lines))
	}
	if p.inComment {
		p.doc.Comments = append(p.doc.Comments, Comment{
			StartLine: p.commentStart,
			EndLine:   len(p.doc.Lines),
			Text:      strings.TrimSpace(strings.Join(p.commentParts, "\n")),
		})
		p.inComment = false
	}
}

func (p *parser) closeFence(f *openFence, end int) {
	p.doc.Fences = append(p.doc.Fences, Fence{
		StartLine: f.start,
		EndLine:   end,
		Lang:      firstWord(f.info),
		Info:      f.info,
		Content:   f.body,
	})
}

// openFenceAt reports whether line opens a fenced code block. Up to six
// leading spaces are allowed so that fences nested in list items are
// recognised; the indentation is stripped from the block's content.
func openFenceAt(line string) *openFence {
	ind := 0
	for ind < len(line) && line[ind] == ' ' && ind < 6 {
		ind++
	}
	if ind >= len(line) {
		return nil
	}
	ch := line[ind]
	if ch != '`' && ch != '~' {
		return nil
	}
	n := 0
	for ind+n < len(line) && line[ind+n] == ch {
		n++
	}
	if n < 3 {
		return nil
	}
	info := strings.TrimSpace(line[ind+n:])
	if ch == '`' && strings.Contains(info, "`") {
		return nil // an inline code span, not a fence
	}
	return &openFence{char: ch, length: n, indent: ind, info: info}
}

// fenceCloses reports whether line is a closing marker for f: the same
// character, at least as long, and nothing but whitespace after it.
func fenceCloses(line string, f *openFence) bool {
	ind := 0
	for ind < len(line) && line[ind] == ' ' && ind < 6 {
		ind++
	}
	n := 0
	for ind+n < len(line) && line[ind+n] == f.char {
		n++
	}
	if n < f.length {
		return false
	}
	return strings.TrimSpace(line[ind+n:]) == ""
}

func stripIndent(line string, n int) string {
	i := 0
	for i < n && i < len(line) && line[i] == ' ' {
		i++
	}
	return line[i:]
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t,;"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// scanInline masks HTML comments out of a line and records the inline code
// spans it contains. Backtick runs are consumed before "<!--" is looked for,
// so a comment quoted inside a code span stays a code span.
func (p *parser) scanInline(i int, line string) {
	out := []byte(line)
	pos := 0

	if p.inComment {
		idx := strings.Index(line, "-->")
		if idx < 0 {
			p.commentParts = append(p.commentParts, line)
			blank(out, 0, len(out))
			p.text[i] = string(out)
			return
		}
		p.commentParts = append(p.commentParts, line[:idx])
		p.doc.Comments = append(p.doc.Comments, Comment{
			StartLine: p.commentStart,
			EndLine:   i + 1,
			Text:      strings.TrimSpace(strings.Join(p.commentParts, "\n")),
		})
		p.inComment = false
		p.commentParts = nil
		pos = idx + 3
		blank(out, 0, pos)
	}

	for pos < len(line) {
		switch {
		case line[pos] == '`':
			cs, ce, end, ok := findCodeSpan(line, pos)
			if !ok {
				for pos < len(line) && line[pos] == '`' {
					pos++
				}
				continue
			}
			p.doc.Spans = append(p.doc.Spans, Span{
				Line: i + 1,
				Col:  cs + 1,
				Text: trimSpanContent(line[cs:ce]),
				Kind: "code",
			})
			p.code[i] = append(p.code[i], rng{pos, end})
			pos = end
		case strings.HasPrefix(line[pos:], "<!--"):
			rest := line[pos+4:]
			if idx := strings.Index(rest, "-->"); idx >= 0 {
				end := pos + 4 + idx + 3
				p.doc.Comments = append(p.doc.Comments, Comment{
					StartLine: i + 1,
					EndLine:   i + 1,
					Text:      strings.TrimSpace(rest[:idx]),
				})
				blank(out, pos, end)
				pos = end
				continue
			}
			p.inComment = true
			p.commentStart = i + 1
			p.commentParts = []string{rest}
			blank(out, pos, len(out))
			pos = len(line)
		default:
			pos++
		}
	}
	p.text[i] = string(out)
}

func blank(b []byte, start, end int) {
	for i := start; i < end && i < len(b); i++ {
		b[i] = ' '
	}
}

// findCodeSpan matches a backtick run starting at i against a run of the
// same length later on the line. It returns the content bounds and the
// offset just past the closing run.
func findCodeSpan(s string, i int) (contentStart, contentEnd, end int, ok bool) {
	n := 0
	for i+n < len(s) && s[i+n] == '`' {
		n++
	}
	j := i + n
	for j < len(s) {
		if s[j] != '`' {
			j++
			continue
		}
		k := j
		for k < len(s) && s[k] == '`' {
			k++
		}
		if k-j == n {
			return i + n, j, k, true
		}
		j = k
	}
	return 0, 0, 0, false
}

// trimSpanContent applies the CommonMark rule that one leading and one
// trailing space are stripped when the content has both and is not blank.
func trimSpanContent(c string) string {
	if len(c) >= 2 && (c[0] == ' ' || c[0] == '\t') &&
		(c[len(c)-1] == ' ' || c[len(c)-1] == '\t') && strings.TrimSpace(c) != "" {
		return c[1 : len(c)-1]
	}
	return c
}

var delimCellRe = regexp.MustCompile(`^:?-+:?$`)

// scanTables finds GFM pipe tables: a header row containing '|' followed by
// a delimiter row with the same number of cells.
func (p *parser) scanTables() {
	n := len(p.doc.Lines)
	for i := 0; i < n-1; i++ {
		if p.skip[i] || p.skip[i+1] {
			continue
		}
		head := p.text[i]
		if !strings.Contains(head, "|") || strings.TrimSpace(head) == "" {
			continue
		}
		dcells, ok := delimiterCells(p.text[i+1])
		if !ok {
			continue
		}
		hcells := splitRow(head)
		if len(hcells) == 0 || len(hcells) != len(dcells) {
			continue
		}
		j := i + 2
		for j < n && !p.skip[j] && strings.Contains(p.text[j], "|") &&
			strings.TrimSpace(p.text[j]) != "" {
			j++
		}
		p.doc.Tables = append(p.doc.Tables, Table{
			StartLine: i + 1,
			EndLine:   j,
			Rows:      j - i - 2,
			Cols:      len(hcells),
		})
		for k := i; k < j; k++ {
			p.table[k] = true
		}
		i = j - 1
	}
}

func delimiterCells(line string) ([]string, bool) {
	if !strings.Contains(line, "-") {
		return nil, false
	}
	cells := splitRow(line)
	if len(cells) == 0 {
		return nil, false
	}
	for _, c := range cells {
		if !delimCellRe.MatchString(c) {
			return nil, false
		}
	}
	return cells, true
}

// splitRow splits a table row on unescaped '|', dropping the optional
// leading and trailing pipe.
// SplitRow splits a GFM table row into trimmed cells ("\|" escapes kept
// as "|"); nil for a row with no content.
func SplitRow(line string) []string { return splitRow(line) }

func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.ReplaceAll(s, `\|`, "\x00")
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, "|")
	out := make([]string, len(parts))
	for i, c := range parts {
		out[i] = strings.TrimSpace(strings.ReplaceAll(c, "\x00", "|"))
	}
	return out
}

var (
	setextRe = regexp.MustCompile(`^ {0,3}(=+|-+)[ \t]*$`)
	listRe   = regexp.MustCompile(`^ {0,3}(?:[-*+]|\d{1,9}[.)])(?:[ \t]|$)`)
)

// scanHeadings collects ATX and setext headings and assigns GitHub slugs
// with duplicate suffixes in document order.
func (p *parser) scanHeadings() {
	for i := range p.doc.Lines {
		if p.skip[i] {
			continue
		}
		if lvl, txt, ok := atxHeading(p.text[i]); ok {
			p.heading[i] = true
			p.doc.Headings = append(p.doc.Headings, Heading{Line: i + 1, Level: lvl, Text: txt})
			continue
		}
		if i == 0 || p.table[i] || !setextRe.MatchString(p.text[i]) {
			continue
		}
		prev := p.text[i-1]
		if p.skip[i-1] || p.heading[i-1] || p.refDefLine(i-1) ||
			setextRe.MatchString(prev) ||
			strings.TrimSpace(prev) == "" || listRe.MatchString(prev) ||
			strings.HasPrefix(strings.TrimSpace(prev), ">") ||
			strings.HasPrefix(strings.TrimSpace(prev), "#") {
			continue
		}
		level := 2
		if strings.Contains(p.text[i], "=") {
			level = 1
		}
		p.heading[i] = true
		p.heading[i-1] = true
		p.doc.Headings = append(p.doc.Headings, Heading{
			Line:  i,
			Level: level,
			Text:  strings.TrimSpace(prev),
		})
	}
	sort.SliceStable(p.doc.Headings, func(a, b int) bool {
		return p.doc.Headings[a].Line < p.doc.Headings[b].Line
	})
	seen := map[string]int{}
	for i := range p.doc.Headings {
		base := Slug(p.doc.Headings[i].Text)
		s := base
		if k, dup := seen[base]; dup {
			s = base + "-" + itoa(k)
		}
		seen[base]++
		p.doc.Headings[i].Slug = s
	}
}

// atxHeading parses "### text ###".
func atxHeading(line string) (level int, text string, ok bool) {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	if i > 3 || i >= len(line) || line[i] != '#' {
		return 0, "", false
	}
	j := i
	for j < len(line) && line[j] == '#' {
		j++
	}
	level = j - i
	if level > 6 {
		return 0, "", false
	}
	if j < len(line) && line[j] != ' ' && line[j] != '\t' {
		return 0, "", false
	}
	t := strings.TrimSpace(line[j:])
	if e := len(t); e > 0 {
		k := e
		for k > 0 && t[k-1] == '#' {
			k--
		}
		if k < e && (k == 0 || t[k-1] == ' ' || t[k-1] == '\t') {
			t = strings.TrimRight(t[:k], " \t")
		}
	}
	return level, t, true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

var refDefRe = regexp.MustCompile(`^ {0,3}\[([^\]]+)\]:[ \t]*(?:<([^>]*)>|(\S+))(?:[ \t]+(?:"([^"]*)"|'([^']*)'|\(([^)]*)\)))?[ \t]*$`)

func (p *parser) refDefLine(i int) bool {
	return !p.skip[i] && refDefRe.MatchString(p.text[i])
}

// scanRefDefs collects "[id]: target" definitions so that reference links
// can be resolved afterwards.
func (p *parser) scanRefDefs() {
	for i := range p.doc.Lines {
		if p.skip[i] || p.heading[i] || p.table[i] {
			continue
		}
		m := refDefRe.FindStringSubmatchIndex(p.text[i])
		if m == nil {
			continue
		}
		line := p.text[i]
		id := line[m[2]:m[3]]
		tr := rng{m[4], m[5]} // <angle> destination
		if tr.a < 0 {
			tr = rng{m[6], m[7]} // plain destination
		}
		title := ""
		for _, g := range [][2]int{{m[8], m[9]}, {m[10], m[11]}, {m[12], m[13]}} {
			if g[0] >= 0 {
				title = line[g[0]:g[1]]
			}
		}
		p.refDef[i] = true
		p.doc.RefDefs = append(p.doc.RefDefs, Link{
			Line: i + 1, Col: m[0] + 1, Text: id,
			Target: strings.Trim(line[tr.a:tr.b], "<>"), Title: title,
		})
		p.targets[i] = append(p.targets[i], tr)
		p.links[i] = append(p.links[i], rng{m[0], m[1]})
	}
}

// scanLinks records inline links, images, reference links, autolinks and
// bare URLs. Constructs starting inside an inline code span are skipped.
func (p *parser) scanLinks() {
	refs := map[string]string{}
	for _, r := range p.doc.RefDefs {
		refs[strings.ToLower(strings.TrimSpace(r.Text))] = r.Target
	}
	for i := range p.doc.Lines {
		if p.skip[i] || p.refDef[i] {
			continue
		}
		p.scanLinkLine(i, p.text[i])
	}
	p.resolvePending(refs)
}

func (p *parser) scanLinkLine(i int, line string) {
	pos := 0
	for pos < len(line) {
		if r, in := inRanges(p.code[i], pos); in {
			pos = r.b
			continue
		}
		c := line[pos]
		switch {
		case c == '[' || (c == '!' && pos+1 < len(line) && line[pos+1] == '['):
			if lk, end, tgt, id, ok := parseLink(line, pos); ok {
				lk.Line = i + 1
				lk.Col = pos + 1
				p.links[i] = append(p.links[i], rng{pos, end})
				if tgt.b > tgt.a {
					p.targets[i] = append(p.targets[i], tgt)
				}
				idx := p.appendLink(lk)
				if id != "" {
					p.pending = append(p.pending, pendingRef{idx: idx, image: lk.IsImage, id: id})
				}
				pos = end
				continue
			}
		case c == '<':
			if url, end, ok := parseAutolink(line, pos); ok {
				p.links[i] = append(p.links[i], rng{pos, end})
				p.targets[i] = append(p.targets[i], rng{pos + 1, end - 1})
				p.appendLink(Link{Line: i + 1, Col: pos + 1, Target: url})
				pos = end
				continue
			}
		case c == 'h':
			if url, end, ok := parseBareURL(line, pos); ok {
				p.links[i] = append(p.links[i], rng{pos, end})
				p.targets[i] = append(p.targets[i], rng{pos, end})
				p.appendLink(Link{Line: i + 1, Col: pos + 1, Target: url})
				pos = end
				continue
			}
		}
		pos++
	}
}

// appendLink stores lk in Links or Images and returns its index there.
func (p *parser) appendLink(lk Link) int {
	if lk.IsImage {
		p.doc.Images = append(p.doc.Images, lk)
		return len(p.doc.Images) - 1
	}
	p.doc.Links = append(p.doc.Links, lk)
	return len(p.doc.Links) - 1
}

// resolvePending fills in reference-style link targets and drops the ones
// whose label was never defined.
func (p *parser) resolvePending(refs map[string]string) {
	dropL := map[int]bool{}
	dropI := map[int]bool{}
	for _, pr := range p.pending {
		target, ok := refs[strings.ToLower(strings.TrimSpace(pr.id))]
		if !ok {
			if pr.image {
				dropI[pr.idx] = true
			} else {
				dropL[pr.idx] = true
			}
			continue
		}
		if pr.image {
			p.doc.Images[pr.idx].Target = target
		} else {
			p.doc.Links[pr.idx].Target = target
		}
	}
	p.doc.Links = filterLinks(p.doc.Links, dropL)
	p.doc.Images = filterLinks(p.doc.Images, dropI)
}

func filterLinks(in []Link, drop map[int]bool) []Link {
	if len(drop) == 0 {
		return in
	}
	out := in[:0]
	for i, l := range in {
		if !drop[i] {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func inRanges(rs []rng, i int) (rng, bool) {
	for _, r := range rs {
		if r.contains(i) {
			return r, true
		}
	}
	return rng{}, false
}

// parseLink parses "[text](dest \"title\")", "[text](<dest>)",
// "![alt](dest)", "[text][id]" and "[text][]" starting at i. When the link
// is reference-style the returned id is non-empty and Target is still "".
func parseLink(s string, i int) (lk Link, end int, target rng, id string, ok bool) {
	if s[i] == '!' {
		lk.IsImage = true
		i++
	}
	if i >= len(s) || s[i] != '[' {
		return lk, 0, target, "", false
	}
	endBr := matchBracket(s, i, '[', ']')
	if endBr < 0 {
		return lk, 0, target, "", false
	}
	lk.Text = s[i+1 : endBr]
	j := endBr + 1
	if j >= len(s) {
		return lk, 0, target, "", false
	}
	switch s[j] {
	case '(':
		dest, title, tr, e, good := parseInlineDest(s, j)
		if !good {
			return lk, 0, target, "", false
		}
		lk.Target, lk.Title = dest, title
		return lk, e, tr, "", true
	case '[':
		k := matchBracket(s, j, '[', ']')
		if k < 0 {
			return lk, 0, target, "", false
		}
		label := s[j+1 : k]
		if strings.TrimSpace(label) == "" {
			label = lk.Text
		}
		if strings.TrimSpace(label) == "" {
			return lk, 0, target, "", false
		}
		return lk, k + 1, rng{}, label, true
	}
	return lk, 0, target, "", false
}

// matchBracket returns the index of the closer matching the opener at i,
// honouring nesting and backslash escapes, or -1.
func matchBracket(s string, i int, open, close byte) int {
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// parseInlineDest parses "(dest)", "(dest \"title\")" or "(<dest>)"
// beginning at the '(' at index i. Parentheses inside the destination are
// allowed when balanced, so URLs like .../Foo_(bar) survive.
func parseInlineDest(s string, i int) (dest, title string, target rng, end int, ok bool) {
	j := i + 1
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	if j < len(s) && s[j] == '<' {
		k := strings.IndexByte(s[j:], '>')
		if k < 0 {
			return "", "", target, 0, false
		}
		dest = s[j+1 : j+k]
		target = rng{j + 1, j + k}
		j += k + 1
	} else {
		st := j
		depth := 0
		for j < len(s) {
			c := s[j]
			if c == '\\' {
				j += 2
				if j > len(s) {
					j = len(s) // a trailing backslash must not run past the line
				}
				continue
			}
			if c == ' ' || c == '\t' {
				break
			}
			if c == '(' {
				depth++
			} else if c == ')' {
				if depth == 0 {
					break
				}
				depth--
			}
			j++
		}
		dest = s[st:j]
		target = rng{st, j}
	}
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	if j < len(s) && (s[j] == '"' || s[j] == '\'' || s[j] == '(') {
		closer := s[j]
		if closer == '(' {
			closer = ')'
		}
		k := strings.IndexByte(s[j+1:], closer)
		if k < 0 {
			return "", "", target, 0, false
		}
		title = s[j+1 : j+1+k]
		j += k + 2
	}
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	if j >= len(s) || s[j] != ')' {
		return "", "", target, 0, false
	}
	return dest, title, target, j + 1, true
}

var autolinkRe = regexp.MustCompile(`^<([a-zA-Z][a-zA-Z0-9+.\-]*:[^<>\s]+)>`)

func parseAutolink(s string, i int) (url string, end int, ok bool) {
	m := autolinkRe.FindStringSubmatchIndex(s[i:])
	if m == nil {
		return "", 0, false
	}
	return s[i+m[2] : i+m[3]], i + m[1], true
}

// parseBareURL matches a naked http(s) URL in prose and trims the sentence
// punctuation that usually follows it, keeping balanced parentheses.
func parseBareURL(s string, i int) (url string, end int, ok bool) {
	rest := s[i:]
	var scheme int
	switch {
	case strings.HasPrefix(rest, "https://"):
		scheme = len("https://")
	case strings.HasPrefix(rest, "http://"):
		scheme = len("http://")
	default:
		return "", 0, false
	}
	j := i
	for j < len(s) && !isSpaceByte(s[j]) && s[j] != '<' && s[j] != '>' && s[j] != '"' && s[j] != '`' {
		j++
	}
	for j > i {
		c := s[j-1]
		if strings.IndexByte(".,;:!?'*_", c) >= 0 || c == ']' {
			j--
			continue
		}
		if c == ')' && strings.Count(s[i:j], "(") < strings.Count(s[i:j], ")") {
			j--
			continue
		}
		break
	}
	if j <= i+scheme {
		return "", 0, false
	}
	return s[i:j], j, true
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

var barePathRe = regexp.MustCompile(`(?:^|[\s(])((?:\.{0,2}/)?[\w.@-]+(?:/[\w.@*-]+)+(?:\.[A-Za-z0-9]+)?)`)

// scanProse builds the per-line text used for bare-path and number
// extraction and collects the bare paths.
func (p *parser) scanProse() {
	p.doc.numLines = make([]string, len(p.doc.Lines))
	for i := range p.doc.Lines {
		if p.skip[i] {
			continue
		}
		nums := []byte(p.text[i])
		for _, r := range p.targets[i] {
			blank(nums, r.a, r.b)
		}
		p.doc.numLines[i] = string(nums)

		bare := []byte(p.text[i])
		for _, r := range p.code[i] {
			blank(bare, r.a, r.b)
		}
		for _, r := range p.links[i] {
			blank(bare, r.a, r.b)
		}
		p.collectBarePaths(i, string(bare))
	}
}

func (p *parser) collectBarePaths(i int, line string) {
	p.doc.BarePaths = append(p.doc.BarePaths, BarePathSpans(line, i+1)...)
}

// BarePathSpans returns the prose tokens of one line (1-based lineNo, with
// code spans and links already blanked out) that look like file paths.
func BarePathSpans(line string, lineNo int) []Span {
	var out []Span
	for _, m := range barePathRe.FindAllStringSubmatchIndex(line, -1) {
		a := m[2]
		tok := strings.TrimRight(line[a:m[3]], ".-@")
		if !plausiblePath(tok) {
			continue
		}
		out = append(out, Span{Line: lineNo, Col: a + 1, Text: tok, Kind: "bare"})
	}
	return out
}

// FindBareURLs returns the [start, end) byte ranges of the bare http(s)
// URLs in line, trailing punctuation excluded.
func FindBareURLs(line string) [][2]int {
	var out [][2]int
	for i := 0; i < len(line); i++ {
		if line[i] != 'h' {
			continue
		}
		if _, end, ok := parseBareURL(line, i); ok {
			out = append(out, [2]int{i, end})
			i = end - 1
		}
	}
	return out
}

// SetNumberLines sets the per-line prose text that Numbers scans (link
// targets and code blanked out). Parsers of other formats call it.
func (d *Doc) SetNumberLines(lines []string) { d.numLines = lines }

// SplitLines splits content into lines the way Parse does: "\n" or
// "\r\n" endings, no trailing empty line.
func SplitLines(content []byte) []string { return splitLines(content) }

// plausiblePath keeps the bare-path heuristic conservative: one or more
// slashes, no scheme, no "//", not a trailing slash, and at least one
// letter so that "1/2" is not mistaken for a path.
func plausiblePath(tok string) bool {
	if !strings.Contains(tok, "/") || strings.Contains(tok, "//") {
		return false
	}
	if strings.HasPrefix(tok, "http") || strings.HasSuffix(tok, "/") {
		return false
	}
	for _, r := range tok {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func (p *parser) assignSections() { p.doc.AssignSections() }

// AssignSections fills the Section of every span, bare path, link, image
// and reference definition from the headings. Parsers of other formats call
// it once their tokens are in place.
func (d *Doc) AssignSections() {
	for i := range d.Spans {
		d.Spans[i].Section = d.SectionAt(d.Spans[i].Line)
	}
	for i := range d.BarePaths {
		d.BarePaths[i].Section = d.SectionAt(d.BarePaths[i].Line)
	}
	for i := range d.Links {
		d.Links[i].Section = d.SectionAt(d.Links[i].Line)
	}
	for i := range d.Images {
		d.Images[i].Section = d.SectionAt(d.Images[i].Line)
	}
	for i := range d.RefDefs {
		d.RefDefs[i].Section = d.SectionAt(d.RefDefs[i].Line)
	}
}

// Ignore directives, written inside HTML comments.
const (
	directiveIgnore      = "docrot:ignore"
	directiveIgnoreStart = "docrot:ignore-start"
	directiveIgnoreEnd   = "docrot:ignore-end"
	directiveIgnoreFile  = "docrot:ignore-file"
)

// applyIgnores turns docrot:ignore comments into the Ignored predicate.
// The tokenizer still records every token; callers decide what to drop.
func (p *parser) applyIgnores() { p.doc.ApplyIgnores(p.commentAlone) }

// ApplyIgnores derives Ignored and RuleIgnored from the docrot:ignore
// directives among d.Comments. alone reports whether a comment is the only
// content on its line(s), which selects the "ignore the next non-blank
// line" behaviour; parsers whose comments always stand alone pass a
// function that returns true. Both predicates are set even when there is
// no directive at all.
func (d *Doc) ApplyIgnores(alone func(Comment) bool) {
	ignored := map[int]bool{}
	scoped := map[int]map[string]bool{} // line → rules ignored there
	n := len(d.Lines)
	inRange, rangeStart := false, 0
	var rangeRules []string

	mark := func(from, to int, rules []string) {
		if len(rules) == 0 {
			markRange(ignored, from, to)
			return
		}
		for l := from; l <= to; l++ {
			m := scoped[l]
			if m == nil {
				m = map[string]bool{}
				scoped[l] = m
			}
			for _, r := range rules {
				m[r] = true
			}
		}
	}
	for _, c := range d.Comments {
		word, rules := splitDirective(c.Text)
		switch word {
		case directiveIgnoreFile:
			d.IgnoreFile = true
		case directiveIgnoreStart:
			if !inRange {
				inRange, rangeStart, rangeRules = true, c.StartLine, rules
			}
		case directiveIgnoreEnd:
			if inRange {
				mark(rangeStart, c.EndLine, rangeRules)
				inRange = false
			}
		case directiveIgnore:
			if alone(c) {
				for l := c.EndLine + 1; l <= n; l++ {
					if strings.TrimSpace(d.Lines[l-1]) != "" {
						// the next line may open a fenced block: then the whole block is meant
						mark(l, fenceEnd(d.Lines, l), rules)
						break
					}
				}
			} else {
				mark(c.StartLine, c.EndLine, rules)
			}
		}
	}
	if inRange {
		mark(rangeStart, n, rangeRules)
	}
	d.Ignored = func(line int) bool { return ignored[line] }
	d.RuleIgnored = func(line int, rule string) bool { return scoped[line][rule] }
}

// splitDirective splits a comment such as "docrot:ignore missing-path,
// unknown-flag" into the directive word and the rule names that scope
// it (none for the plain form). Rules are separated by commas or spaces.
func splitDirective(text string) (word string, rules []string) {
	f := strings.FieldsFunc(strings.TrimSpace(text), func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == ',' })
	if len(f) == 0 {
		return "", nil
	}
	return f[0], f[1:]
}

// commentAlone reports whether the comment is the only content on its
// line(s), which selects the "ignore the next non-blank line" behaviour.
func (p *parser) commentAlone(c Comment) bool {
	for l := c.StartLine; l <= c.EndLine && l <= len(p.text); l++ {
		if strings.TrimSpace(p.text[l-1]) != "" {
			return false
		}
	}
	return true
}

func markRange(m map[int]bool, from, to int) {
	for l := from; l <= to; l++ {
		m[l] = true
	}
}

// fenceEnd returns the 1-based line that closes the fenced code block
// opened on line l (``` or ~~~, any longer run of the same character), the
// last line when the fence is never closed, or l itself when line l does
// not open a fence.
func fenceEnd(lines []string, l int) int {
	if l < 1 || l > len(lines) {
		return l
	}
	t := strings.TrimSpace(lines[l-1])
	var marker string
	switch {
	case strings.HasPrefix(t, "```"):
		marker = t[:3+len(t[3:])-len(strings.TrimLeft(t[3:], "`"))]
	case strings.HasPrefix(t, "~~~"):
		marker = t[:3+len(t[3:])-len(strings.TrimLeft(t[3:], "~"))]
	default:
		return l
	}
	for e := l + 1; e <= len(lines); e++ {
		s := strings.TrimSpace(lines[e-1])
		if strings.HasPrefix(s, marker) && strings.Trim(s, marker[:1]) == "" {
			return e
		}
	}
	return len(lines)
}
