// Package asciidoc tokenizes AsciiDoc into the Doc shape the Markdown
// parser produces: section titles ("== Title"), explicit ids ("[[id]]",
// "[#id]"), listing and literal blocks ("----", "....", "[source,go]"),
// inline code (`code`), link:/xref:/include::/image:: macros, "<<anchor>>"
// cross references, bare URLs and "// docrot:ignore" comments. Auto-generated
// section ids follow Asciidoctor's defaults ("_getting_started").
package asciidoc

import (
	"regexp"
	"sort"
	"strings"

	"docrot/internal/markdown"
)

var (
	reTitle    = regexp.MustCompile(`^(={1,6})\s+(.+?)\s*(?:=+)?\s*$`)
	reAnchorLn = regexp.MustCompile(`^\[\[([^\],]+)(?:,[^\]]*)?\]\]\s*$|^\[#([^\],\]]+)[^\]]*\]\s*$`)
	reSource   = regexp.MustCompile(`^\[(?:source|listing|literal)?\s*,?\s*([A-Za-z0-9_+-]*)`)
	reInline   = regexp.MustCompile("`([^`\n]+)`")
	reMacro    = regexp.MustCompile(`\b(link|xref|image|include|mailto):([^\[\s]+)\[([^\]]*)\]`)
	reBlockMac = regexp.MustCompile(`^(image|include|video|audio)::([^\[\s]+)\[([^\]]*)\]\s*$`)
	reXref     = regexp.MustCompile(`<<([^,>]+)(?:,[^>]*)?>>`)
	reAttrRef  = regexp.MustCompile(`\{[\w-]+\}`)
	reURLMacro = regexp.MustCompile(`(https?://[^\s\[\]]+)\[([^\]]*)\]`)
	reSlugJunk = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

type parser struct {
	doc   *markdown.Doc
	lines []string
	skip  []bool
	prose []string
	slugs map[string]int
}

// Parse tokenizes content. path is stored verbatim in Doc.Path.
func Parse(path string, content []byte) *markdown.Doc {
	lines := markdown.SplitLines(content)
	p := &parser{doc: &markdown.Doc{Path: path, Lines: lines}, lines: lines, skip: make([]bool, len(lines)), prose: make([]string, len(lines)), slugs: map[string]int{}}
	p.blocks()
	p.inline()
	sort.SliceStable(p.doc.Links, func(i, j int) bool {
		if p.doc.Links[i].Line != p.doc.Links[j].Line {
			return p.doc.Links[i].Line < p.doc.Links[j].Line
		}
		return p.doc.Links[i].Col < p.doc.Links[j].Col
	})
	p.doc.SetNumberLines(p.prose)
	p.doc.ApplyIgnores(func(markdown.Comment) bool { return true })
	p.doc.AssignSections()
	return p.doc
}

func (p *parser) blocks() {
	n := len(p.lines)
	pendingLang, havePending := "", false
	var pendingID string
	for i := 0; i < n; i++ {
		line := p.lines[i]
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, "////"):
			// comment block
			end := i
			for j := i + 1; j < n; j++ {
				if strings.HasPrefix(strings.TrimSpace(p.lines[j]), "////") {
					end = j
					break
				}
				end = j
			}
			var parts []string
			for j := i + 1; j < end; j++ {
				parts = append(parts, strings.TrimSpace(p.lines[j]))
			}
			p.doc.Comments = append(p.doc.Comments, markdown.Comment{StartLine: i + 1, EndLine: end + 1, Text: strings.Join(parts, "\n")})
			p.markSkip(i, end)
			i = end
			continue
		case strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "///"):
			p.doc.Comments = append(p.doc.Comments, markdown.Comment{StartLine: i + 1, EndLine: i + 1, Text: strings.TrimSpace(t[2:])})
			p.skip[i] = true
			continue
		case t == "----" || t == "...." || t == "```":
			// delimited listing / literal block (``` is the Markdown-compatible form)
			end := n - 1
			for j := i + 1; j < n; j++ {
				if strings.TrimSpace(p.lines[j]) == t {
					end = j
					break
				}
			}
			lang := ""
			if havePending {
				lang = pendingLang
			}
			var body []string
			for j := i + 1; j < end; j++ {
				body = append(body, p.lines[j])
			}
			p.doc.Fences = append(p.doc.Fences, markdown.Fence{StartLine: i + 1, EndLine: end + 1, Lang: lang, Content: body})
			p.markSkip(i, end)
			havePending = false
			i = end
			continue
		case strings.HasPrefix(t, "|==="):
			end := n - 1
			rows := 0
			for j := i + 1; j < n; j++ {
				if strings.HasPrefix(strings.TrimSpace(p.lines[j]), "|===") {
					end = j
					break
				}
				if strings.HasPrefix(strings.TrimSpace(p.lines[j]), "|") {
					rows++
				}
			}
			p.doc.Tables = append(p.doc.Tables, markdown.Table{StartLine: i + 1, EndLine: end + 1, Rows: rows, Cols: 0})
			p.skip[i], p.skip[end] = true, true
			i = end
			continue
		}
		if m := reAnchorLn.FindStringSubmatch(t); m != nil {
			id := m[1]
			if id == "" {
				id = m[2]
			}
			pendingID = strings.ToLower(strings.TrimSpace(id))
			p.doc.Labels = append(p.doc.Labels, pendingID)
			p.skip[i] = true
			continue
		}
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			if m := reSource.FindStringSubmatch(t); m != nil {
				pendingLang, havePending = strings.ToLower(m[1]), true
			}
			p.skip[i] = true // a block attribute line
			continue
		}
		if m := reBlockMac.FindStringSubmatch(t); m != nil {
			lk := markdown.Link{Line: i + 1, Col: 1, Text: m[1], Target: m[2]}
			if m[1] == "image" || m[1] == "video" || m[1] == "audio" {
				lk.IsImage = true
				p.doc.Images = append(p.doc.Images, lk)
			} else {
				p.doc.Links = append(p.doc.Links, lk)
			}
			p.skip[i] = true
			continue
		}
		if m := reTitle.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, " ") {
			text := strings.TrimSpace(m[2])
			slug := pendingID
			if slug == "" {
				slug = p.slug(text)
			}
			p.doc.Headings = append(p.doc.Headings, markdown.Heading{Line: i + 1, Level: len(m[1]), Text: text, Slug: slug})
			pendingID = ""
			continue
		}
		pendingID = ""
	}
}

func (p *parser) markSkip(from, to int) {
	for j := from; j <= to && j < len(p.skip); j++ {
		p.skip[j] = true
	}
}

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
		for _, m := range reInline.FindAllStringSubmatchIndex(line, -1) {
			p.doc.Spans = append(p.doc.Spans, markdown.Span{Line: i + 1, Col: m[2] + 1, Text: strings.TrimSpace(line[m[2]:m[3]]), Kind: "code"})
			wipe(m[0], m[1])
		}
		cur := string(masked)
		for _, m := range reMacro.FindAllStringSubmatchIndex(cur, -1) {
			kind, target, text := cur[m[2]:m[3]], cur[m[4]:m[5]], cur[m[6]:m[7]]
			lk := markdown.Link{Line: i + 1, Col: m[0] + 1, Text: text, Target: target}
			switch kind {
			case "image":
				lk.IsImage = true
				p.doc.Images = append(p.doc.Images, lk)
			case "mailto":
			case "xref":
				// xref:page.adoc#id[text] or xref:id[text]
				if !strings.Contains(target, "#") && !strings.Contains(target, ".") {
					lk.Target = "#" + target
				}
				p.doc.Links = append(p.doc.Links, lk)
			default:
				p.doc.Links = append(p.doc.Links, lk)
			}
			wipe(m[0], m[1])
		}
		cur = string(masked)
		for _, m := range reXref.FindAllStringSubmatchIndex(cur, -1) {
			target := strings.TrimSpace(cur[m[2]:m[3]])
			if !strings.Contains(target, "#") && !strings.Contains(target, ".") {
				target = "#" + target
			}
			p.doc.Links = append(p.doc.Links, markdown.Link{Line: i + 1, Col: m[0] + 1, Target: target})
			wipe(m[0], m[1])
		}
		cur = string(masked)
		for _, m := range reURLMacro.FindAllStringSubmatchIndex(cur, -1) { // https://x[text]
			p.doc.Links = append(p.doc.Links, markdown.Link{Line: i + 1, Col: m[0] + 1, Text: cur[m[4]:m[5]], Target: cur[m[2]:m[3]]})
			wipe(m[0], m[1])
		}
		cur = string(masked)
		for _, r := range markdown.FindBareURLs(cur) {
			p.doc.Links = append(p.doc.Links, markdown.Link{Line: i + 1, Col: r[0] + 1, Target: cur[r[0]:r[1]]})
			wipe(r[0], r[1])
		}
		for _, m := range reAttrRef.FindAllStringIndex(string(masked), -1) {
			wipe(m[0], m[1])
		}
		cur = string(masked)
		p.doc.BarePaths = append(p.doc.BarePaths, markdown.BarePathSpans(cur, i+1)...)
		p.prose[i] = cur
	}
}

// slug builds Asciidoctor's default section id: "_" plus the lower-cased
// title with runs of non-alphanumerics replaced by "_".
func (p *parser) slug(title string) string {
	s := strings.Trim(reSlugJunk.ReplaceAllString(strings.ToLower(title), "_"), "_")
	s = "_" + s
	n := p.slugs[s]
	p.slugs[s] = n + 1
	if n > 0 {
		return s + "_" + string(rune('0'+n))
	}
	return s
}
