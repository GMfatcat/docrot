package extract

import (
	"regexp"
	"strings"

	"docrot/internal/markdown"
	"docrot/internal/model"
)

// Documented default values: "`--port` defaults to `8080`", "`log.level`
// (default: `info`)", and tables with a Default column.

var (
	reDefaultTo   = regexp.MustCompile("(?i)\\bdefaults?\\s+(?:to|is|of)\\s+(`[^`]+`|\"[^\"]+\"|[^\\s`,;)]+)")
	reDefaultPar  = regexp.MustCompile("(?i)\\(\\s*default\\s*[:=]?\\s*(`[^`]+`|\"[^\"]+\"|[^`)]+?)\\s*\\)")
	reDefaultColn = regexp.MustCompile("(?i)\\bdefault\\s*[:=]\\s*(`[^`]+`|\"[^\"]+\"|[^\\s`,;)]+)")
	reLiteralish  = regexp.MustCompile(`^(?:[-+]?\d[\w.:/-]*|true|false|yes|no|on|off|none|nil|null|empty|[/.:~][^\s]*)$`)
)

// defaultValue finds the default a line states, or "" when there is none
// or it does not look like a literal.
func defaultValue(line string) string {
	var v string
	for _, re := range []*regexp.Regexp{reDefaultPar, reDefaultTo, reDefaultColn} {
		if m := re.FindStringSubmatch(line); m != nil {
			v = strings.TrimSpace(m[1])
			break
		}
	}
	if v == "" {
		return ""
	}
	quoted := strings.HasPrefix(v, "`") || strings.HasPrefix(v, "\"")
	v = strings.Trim(v, "`\"")
	v = strings.TrimRight(v, ".")
	if v == "" {
		return ""
	}
	if !quoted && !reLiteralish.MatchString(strings.ToLower(v)) {
		return "" // "defaults to including": prose, not a value
	}
	return v
}

// defaultSubject returns the kind and name of the one flag / config key /
// environment variable a line's code spans name; ok is false when there
// is none or more than one.
func (x *extractor) defaultSubject(spans []string) (kind, name string, ok bool) {
	for _, s := range spans {
		r := x.classifyWhole(s)
		if r == nil {
			continue
		}
		var k string
		switch r.Kind {
		case model.KindFlag:
			k = "flag"
		case model.KindEnv:
			k = "env"
		case model.KindConfigKey:
			k = "key"
		default:
			continue
		}
		if ok {
			return "", "", false // two subjects: ambiguous
		}
		kind, name, ok = k, r.Norm, true
	}
	return kind, name, ok
}

// defaultsPass emits KindDefault references for prose lines and table rows
// that pair one flag/key/env with a stated default.
func (x *extractor) defaultsPass() {
	inFence := make([]bool, len(x.doc.Lines)+1)
	for _, f := range x.doc.Fences {
		for i := f.StartLine; i <= f.EndLine && i < len(inFence); i++ {
			inFence[i] = true
		}
	}
	spansAt := map[int][]string{}
	for _, sp := range x.doc.Spans {
		spansAt[sp.Line] = append(spansAt[sp.Line], sp.Text)
	}
	inTable := map[int]bool{}
	for _, t := range x.doc.Tables {
		x.tableDefaults(t, spansAt)
		for l := t.StartLine; l <= t.EndLine; l++ {
			inTable[l] = true
		}
	}
	for i, line := range x.doc.Lines {
		ln := i + 1
		if inFence[ln] || inTable[ln] || x.ignored(ln) || len(spansAt[ln]) == 0 {
			continue
		}
		v := defaultValue(line)
		if v == "" {
			continue
		}
		kind, name, ok := x.defaultSubject(spansAt[ln])
		if !ok {
			continue
		}
		x.emit(model.Reference{Kind: model.KindDefault, Text: name + " default " + v, Norm: kind + ":" + name + "|" + v, Confidence: model.Medium}, ln, 0, x.doc.SectionAt(ln), "")
	}
}

// tableDefaults handles "| Flag | Default | … |" tables: the cell under a
// header that says "default" is the value for the flag/key/env named in
// the same row.
func (x *extractor) tableDefaults(t markdown.Table, spansAt map[int][]string) {
	if t.StartLine < 1 || t.StartLine > len(x.doc.Lines) {
		return
	}
	header := markdown.SplitRow(x.doc.Lines[t.StartLine-1])
	col := -1
	for i, h := range header {
		if strings.Contains(strings.ToLower(h), "default") || strings.Contains(h, "預設") {
			col = i
			break
		}
	}
	if col < 0 {
		return
	}
	for ln := t.StartLine + 2; ln <= t.EndLine && ln <= len(x.doc.Lines); ln++ {
		if x.ignored(ln) {
			continue
		}
		cells := markdown.SplitRow(x.doc.Lines[ln-1])
		if col >= len(cells) {
			continue
		}
		kind, name, ok := x.defaultSubject(spansAt[ln])
		if !ok {
			continue
		}
		v := strings.TrimSpace(strings.Trim(strings.TrimSpace(cells[col]), "`\""))
		v = strings.TrimRight(v, ".")
		if v == "—" || v == "–" || v == "-" || v == "" {
			v = "none"
		}
		if strings.ContainsAny(v, " \t") && !reLiteralish.MatchString(strings.ToLower(strings.Fields(v)[0])) {
			continue // a sentence, not a value
		}
		if i := strings.IndexAny(v, " \t"); i > 0 {
			v = v[:i]
		}
		x.emit(model.Reference{Kind: model.KindDefault, Text: name + " default " + v, Norm: kind + ":" + name + "|" + v, Confidence: model.High}, ln, 0, x.doc.SectionAt(ln), "")
	}
}
