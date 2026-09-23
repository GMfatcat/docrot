// Package extract turns Markdown tokens into typed references: paths,
// symbols, flags, environment variables, config keys, anchors, URLs,
// shell commands and Go imports.
//
// The extractor is deliberately heuristic. Its job is to be *predictable*
// rather than clever: `docrot explain` prints exactly what it produced so
// users can tune ignore rules. Every heuristic here is described in
// docs/rules.md and in the design spec (§8).
package extract

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"docrot/internal/markdown"
	"docrot/internal/model"
)

// Hints is the subset of model.Index the extractor needs to classify text.
type Hints interface {
	ModulePath() string
	IsGoPackage(name string) bool
	IsGoType(name string) bool
	TopLevelDirs() []string
	HasOdin() bool
	OdinPackages() []string
	HasPython() bool
	PyModules() []string
	// HasRoutes reports whether the code registers HTTP routes; without
	// any, "/x" in a document is an absolute path, not a route claim.
	HasRoutes() bool
	// HasJSONKey and HasConfigKey decide whether a ```json block is a
	// configuration example worth checking key by key.
	HasJSONKey(dotted string) bool
	HasConfigKey(dotted string) bool
}

// Options controls extraction.
type Options struct {
	// Ignore patterns are matched against Reference.Text; a match drops the reference.
	Ignore []*regexp.Regexp
	// NoToolchain skips version-requirement claims ("requires Go 1.21"):
	// set for changelogs and other historical documents.
	NoToolchain bool
}

type extractor struct {
	doc      *markdown.Doc
	hints    Hints
	opts     Options
	topDirs  map[string]bool
	odinPkgs map[string]bool
	pyMods   map[string]bool
	seen     map[string]bool
	out      []model.Reference
}

// Extract returns every reference found in doc, in document order.
// Lines excluded by docrot:ignore directives are skipped, as is the whole
// document when it carries docrot:ignore-file.
func Extract(doc *markdown.Doc, h Hints, opts Options) []model.Reference {
	if doc == nil || doc.IgnoreFile {
		return nil
	}
	x := &extractor{doc: doc, hints: h, opts: opts, seen: map[string]bool{}}
	x.spans()
	x.links()
	x.fences()
	x.bare()
	x.routes()
	if !opts.NoToolchain {
		x.toolchainRefs()
	}
	sortRefs(x.out)
	return x.out
}

func (x *extractor) ignored(line int) bool {
	return x.doc.Ignored != nil && x.doc.Ignored(line)
}

func (x *extractor) ignoredText(text string) bool {
	for _, re := range x.opts.Ignore {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func (x *extractor) context(line int) string {
	if line < 1 || line > len(x.doc.Lines) {
		return ""
	}
	s := strings.TrimSpace(x.doc.Lines[line-1])
	if len(s) > 160 {
		cut := 157
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut-- // never split a multi-byte rune (CJK lines)
		}
		s = s[:cut] + "..."
	}
	return s
}

func (x *extractor) emit(r model.Reference, line, col int, section, lang string) {
	if r.Norm == "" || x.ignoredText(r.Text) {
		return
	}
	key := string(r.Kind) + "|" + r.Norm + "|" + itoa(line)
	if x.seen[key] {
		return
	}
	x.seen[key] = true
	r.Loc = model.Location{File: x.doc.Path, Line: line, Col: col}
	r.Section = section
	r.Context = x.context(line)
	r.Lang = lang
	x.out = append(x.out, r)
}

func (x *extractor) spans() {
	for _, sp := range x.doc.Spans {
		if x.ignored(sp.Line) {
			continue
		}
		if r := x.classifyWhole(sp.Text); r != nil {
			x.emit(*r, sp.Line, sp.Col, sp.Section, "")
			continue
		}
		for _, r := range x.classifyTokens(sp.Text) {
			x.emit(r, sp.Line, sp.Col, sp.Section, "")
		}
		// `make build`, `pip install httpx`: an inline command line
		for _, r := range projectRefs(strings.Fields(sp.Text), model.Medium) {
			x.emit(r, sp.Line, sp.Col, sp.Section, "")
		}
	}
}

func (x *extractor) links() {
	all := make([]markdown.Link, 0, len(x.doc.Links)+len(x.doc.Images))
	all = append(all, x.doc.Links...)
	all = append(all, x.doc.Images...)
	for _, l := range all {
		if x.ignored(l.Line) {
			continue
		}
		t := strings.TrimSpace(l.Target)
		t = strings.TrimPrefix(strings.TrimSuffix(t, ">"), "<")
		if t == "" || strings.HasPrefix(t, "{{") || strings.Contains(t, "${") {
			continue
		}
		low := strings.ToLower(t)
		switch {
		case reURL.MatchString(t):
			x.emit(model.Reference{Kind: model.KindURL, Text: t, Norm: t, Confidence: model.Low}, l.Line, l.Col, l.Section, "")
			continue
		case strings.HasPrefix(low, "mailto:") || strings.HasPrefix(low, "tel:") || strings.HasPrefix(low, "javascript:") || strings.HasPrefix(low, "data:"):
			continue
		}
		if i := strings.Index(t, "?"); i >= 0 {
			t = t[:i]
		}
		file, frag := t, ""
		if i := strings.Index(t, "#"); i >= 0 {
			file, frag = t[:i], t[i+1:]
		}
		if file != "" {
			dec, err := url.PathUnescape(file)
			if err == nil {
				file = dec
			}
			if r := x.pathRef(file, false); r != nil {
				r.Confidence = model.High
				x.emit(*r, l.Line, l.Col, l.Section, "")
			} else if !strings.HasPrefix(file, "/") && !strings.ContainsAny(file, rejectChars) {
				// links are explicit: even an odd-looking target is a path claim
				p := cleanPath(file)
				if p != "" && p != "." && p != ".." {
					x.emit(model.Reference{Kind: model.KindPath, Text: file, Norm: p, Confidence: model.High}, l.Line, l.Col, l.Section, "")
				}
			}
		}
		if frag != "" && (file == "" || strings.HasSuffix(strings.ToLower(file), ".md")) {
			if reAnchorLine.MatchString(strings.ToLower(frag)) {
				continue // #L12 / #L10-L20 line anchors
			}
			r := x.anchorRef(file, frag, model.High)
			x.emit(*r, l.Line, l.Col, l.Section, "")
		}
	}
}

func (x *extractor) fences() {
	for _, f := range x.doc.Fences {
		if x.ignored(f.StartLine) {
			continue
		}
		section := x.doc.SectionAt(f.StartLine)
		lang := strings.ToLower(f.Lang)
		switch {
		case lang == "go" || lang == "golang":
			for _, r := range x.goFenceRefs(f.Content) {
				x.emit(r, f.StartLine, 0, section, lang)
			}
		case jsonLangs[lang]:
			for _, r := range x.jsonFenceRefs(f) {
				line := r.Loc.Line
				r.Loc = model.Location{}
				if x.ignored(line) {
					continue
				}
				x.emit(r, line, 0, section, "json")
			}
		case shellLangs[lang]:
			for i, line := range f.Content {
				ln := f.StartLine + 1 + i
				if x.ignored(ln) {
					continue
				}
				for _, r := range x.shellRefs(line, plainLangs[lang]) {
					x.emit(r, ln, 0, section, lang)
				}
			}
		}
	}
}

func (x *extractor) bare() {
	for _, sp := range x.doc.BarePaths {
		if x.ignored(sp.Line) {
			continue
		}
		if r := x.pathRef(sp.Text, true); r != nil {
			if r.Confidence > model.Medium {
				r.Confidence = model.Medium
			}
			x.emit(*r, sp.Line, sp.Col, sp.Section, "")
		}
	}
}

func sortRefs(refs []model.Reference) {
	// insertion sort keeps it dependency-free and inputs are small per doc
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && less(refs[j], refs[j-1]); j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

func less(a, b model.Reference) bool {
	if a.Loc.Line != b.Loc.Line {
		return a.Loc.Line < b.Loc.Line
	}
	if a.Loc.Col != b.Loc.Col {
		return a.Loc.Col < b.Loc.Col
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Norm < b.Norm
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}
