package extract

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"docrot/internal/model"
)

// knownExt is the set of file extensions that make a token look like a
// file path even without a slash.
var knownExt = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`go md json txt ps1 py odin yaml yml toml sh exe dll zip pdb ini cfg
		html css js ts sql csv jsonl log mod sum proto env gif png jpg jpeg svg pdf lock bat cmd
		rs c h cpp hpp cc mtrace gguf safetensors wasm tmpl tpl gotmpl xml ico webp mp4 gz tar bz2 xz
		7z conf service plist rst adoc ipynb pyi mjs cjs tsx jsx vue scss less map`) {
		knownExt[e] = true
	}
}

// placeholderFirstSegments are first path segments that almost always mean
// "this is an example, not a real path".
var placeholderFirstSegments = map[string]bool{
	"path": true, "foo": true, "bar": true, "baz": true, "your": true, "some": true,
	"my": true, "xxx": true, "a": true, "b": true, "x": true, "y": true, "dir": true,
	"example": true, "xx": true, "yy": true, "tmp": true, "usr": true, "etc": true,
	"var": true, "home": true, "mnt": true, "opt": true, "proc": true, "dev": true,
	"c": true, "d": true, "e": true, "localhost": true, "user": true, "users": true,
}

var (
	reURL      = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
	reFlag     = regexp.MustCompile(`^(--?)([A-Za-z][A-Za-z0-9_.-]*)(=.*)?$`)
	reEnv      = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+$`)
	reDotted   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)((?:\.[A-Za-z_][A-Za-z0-9_]*)+)(\[[^\]]*\])?(\(.*\))?$`)
	reCall     = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(\[[^\]]*\])?\((.*)\)$`)
	reInToken  = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+`)
	reInFlag   = regexp.MustCompile(`(?:^|[\s,;(\[])(--?[A-Za-z][A-Za-z0-9_-]*)`)
	reInEnv    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_$])([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+)`)
	reInPath   = regexp.MustCompile(`(?:^|[\s"'(\[=,])((?:\.{1,2}/)?[A-Za-z0-9_.@-]+(?:/[A-Za-z0-9_.@*-]+)+/?)`)
	reGoImport = regexp.MustCompile(`^\s*(?:import\s+)?(?:[A-Za-z_][A-Za-z0-9_]*\s+)?"([^"]+)"`)
	reGoSym    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.])([a-z][A-Za-z0-9_]*)\.([A-Z][A-Za-z0-9_]*)`)
	reGoTypeM  = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.])([A-Z][A-Za-z0-9_]*)\.([A-Z][A-Za-z0-9_]*)`)
)

// rejectChars are characters that mark a span as a template, expression or
// natural language rather than a concrete reference.
const rejectChars = "<>{}$%|\"'"

// classifyWhole tries to interpret the whole span as exactly one reference.
// It returns nil when the span is not a single reference.
func (x *extractor) classifyWhole(s string) *model.Reference {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 120 || strings.ContainsAny(s, rejectChars) {
		return nil
	}
	if strings.ContainsAny(s, " \t") {
		// `pkg.Func(a, b)` — a call with spaced arguments is still one reference
		if i := strings.Index(s, "("); i > 0 && strings.HasSuffix(s, ")") && !strings.ContainsAny(s[:i], " \t") {
			if r := x.symbolRef(s[:i] + "()"); r != nil {
				r.Text = s
				return r
			}
		}
		return nil
	}
	if reURL.MatchString(s) {
		return &model.Reference{Kind: model.KindURL, Text: s, Norm: s, Confidence: model.Low}
	}
	// file.md#anchor
	if i := strings.Index(s, "#"); i > 0 && strings.HasSuffix(strings.ToLower(s[:i]), ".md") {
		return x.anchorRef(s[:i], s[i+1:], model.High)
	}
	if m := reFlag.FindStringSubmatch(s); m != nil {
		conf := model.High
		if m[1] == "-" {
			conf = model.Medium
		}
		return &model.Reference{Kind: model.KindFlag, Text: s, Norm: normFlag(m[2]), Confidence: conf}
	}
	if reEnv.MatchString(s) {
		return &model.Reference{Kind: model.KindEnv, Text: s, Norm: s, Confidence: model.High}
	}
	// module-path qualified symbol or import path: largan.local/mod/httpx.WriteData
	if mp := x.hints.ModulePath(); mp != "" && (s == mp || strings.HasPrefix(s, mp+"/")) {
		last := s[strings.LastIndex(s, "/")+1:]
		if dot := strings.Index(last, "."); dot > 0 && s != mp {
			q := last // pkg.Name
			return &model.Reference{Kind: model.KindGoSymbol, Text: s, Norm: strings.TrimSuffix(stripCall(q), ""), Confidence: model.High}
		}
		return &model.Reference{Kind: model.KindImport, Text: s, Norm: strings.TrimSuffix(s, "/..."), Confidence: model.High}
	}
	if r := x.pathRef(s, false); r != nil {
		return r
	}
	if r := x.symbolRef(s); r != nil {
		return r
	}
	return nil
}

// stripCall removes a trailing call "(…)" and generic instantiation "[…]".
func stripCall(s string) string {
	if i := strings.Index(s, "("); i > 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "["); i > 0 {
		s = s[:i]
	}
	return s
}

func normFlag(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", "-"))
}

// anchorRef builds an anchor reference. file may be empty (same document).
func (x *extractor) anchorRef(file, frag string, conf model.Confidence) *model.Reference {
	frag, _ = url.PathUnescape(frag)
	frag = strings.ToLower(strings.TrimSpace(frag))
	target := ""
	if file != "" {
		target = x.resolveDocRelative(cleanPath(file))
	}
	text := file + "#" + frag
	return &model.Reference{Kind: model.KindAnchor, Text: text, Norm: target + "#" + frag, Confidence: conf}
}

// resolveDocRelative joins rel to the document's directory when rel is
// relative, producing a root-relative path.
func (x *extractor) resolveDocRelative(rel string) string {
	if rel == "" {
		return ""
	}
	dir := path.Dir(x.doc.Path)
	if dir == "." {
		return rel
	}
	return path.Clean(dir + "/" + rel)
}

// cleanPath normalises slashes and strips a leading "./".
func cleanPath(s string) string {
	s = strings.ReplaceAll(s, `\`, "/")
	for strings.HasPrefix(s, "./") {
		s = s[2:]
	}
	if len(s) > 1 {
		s = strings.TrimSuffix(s, "/")
	}
	return s
}

// pathRef decides whether s is a path. When loose is true the token came
// from tokenizing a larger span and gets at most Medium confidence.
func (x *extractor) pathRef(s string, loose bool) *model.Reference {
	if strings.HasPrefix(s, "-") || strings.Contains(s, "://") || strings.ContainsAny(s, "()[]#@") {
		return nil
	}
	p := cleanPath(s)
	if p == "" || p == "." || p == ".." || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") {
		return nil
	}
	if len(p) > 1 && p[1] == ':' { // Windows drive
		return nil
	}
	if strings.HasSuffix(p, "/...") {
		p = strings.TrimSuffix(p, "/...")
	}
	if strings.Contains(p, "...") || strings.Contains(p, "//") {
		return nil
	}
	hasSlash := strings.Contains(p, "/")
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(p), "."))
	segs := strings.Split(p, "/")
	first := segs[0]
	isTop := x.isTopLevelDir(first)
	if hasSlash && placeholderFirstSegments[strings.ToLower(first)] && !isTop {
		return nil
	}
	hasGlob := strings.ContainsAny(p, "*?")
	switch {
	case hasSlash && (knownExt[ext] || hasGlob || strings.HasSuffix(s, "/")):
		return x.mkPath(s, p, confCap(model.High, loose))
	case hasSlash:
		// a/b with no extension: directory or package path.
		if isTop {
			return x.mkPath(s, p, confCap(model.High, loose))
		}
		// dotted first segment like largan.local/x is a module path, not ours.
		if strings.Contains(first, ".") && !knownExt[ext] {
			return nil
		}
		return x.mkPath(s, p, model.Medium)
	case knownExt[ext] && len(segs) == 1 && ext != "":
		stem := strings.TrimSuffix(p, path.Ext(p))
		if stem == "" || (ext == "js" && stem[0] >= 'A' && stem[0] <= 'Z') {
			return nil // ".gitignore"-style handled below; "Node.js" is not a file
		}
		return x.mkPath(s, p, model.Medium)
	case !hasSlash && isTop && !loose:
		return x.mkPath(s, p, model.High)
	}
	if strings.HasPrefix(p, ".") && !strings.Contains(p, "/") && len(p) > 2 && !strings.Contains(p[1:], ".") {
		// dotfile like .gitignore, .docrot.json handled above via ext
		return x.mkPath(s, p, model.Medium)
	}
	return nil
}

func confCap(c model.Confidence, loose bool) model.Confidence {
	if loose && c > model.Medium {
		return model.Medium
	}
	return c
}

func (x *extractor) mkPath(text, norm string, conf model.Confidence) *model.Reference {
	return &model.Reference{Kind: model.KindPath, Text: text, Norm: norm, Confidence: conf}
}

func (x *extractor) isTopLevelDir(name string) bool {
	if x.topDirs == nil {
		x.topDirs = map[string]bool{}
		for _, d := range x.hints.TopLevelDirs() {
			x.topDirs[d] = true
		}
	}
	return x.topDirs[name]
}

// symbolRef classifies dotted names and calls as Go/Odin/Python symbols or
// config keys.
func (x *extractor) symbolRef(s string) *model.Reference {
	s = strings.TrimLeft(s, "*&")
	if m := reDotted.FindStringSubmatch(s); m != nil {
		first := m[1]
		rest := strings.TrimPrefix(m[2], ".")
		parts := append([]string{first}, strings.Split(rest, ".")...)
		norm := strings.Join(parts, ".")
		if len(parts) > 4 || (len(parts) == 2 && knownExt[strings.ToLower(parts[1])]) {
			return nil
		}
		if IsStdlibPackage(first) && !x.hints.IsGoPackage(first) {
			return nil
		}
		switch {
		case x.hints.IsGoPackage(first):
			return &model.Reference{Kind: model.KindGoSymbol, Text: s, Norm: norm, Confidence: model.High}
		case len(parts) == 2 && x.hints.IsGoType(first):
			return &model.Reference{Kind: model.KindGoSymbol, Text: s, Norm: norm, Confidence: model.High}
		case x.hints.HasOdin() && x.isOdinPackage(first):
			return &model.Reference{Kind: model.KindOdinSym, Text: s, Norm: norm, Confidence: model.High}
		case x.hints.HasPython() && x.isPyModule(first):
			return &model.Reference{Kind: model.KindPySym, Text: s, Norm: norm, Confidence: model.High}
		}
		allLower := strings.ToLower(norm) == norm
		switch {
		case allLower && x.hints.HasOdin():
			return &model.Reference{Kind: model.KindOdinSym, Text: s, Norm: norm, Confidence: model.Low}
		case allLower && x.hints.HasPython():
			return &model.Reference{Kind: model.KindPySym, Text: s, Norm: norm, Confidence: model.Low}
		case allLower:
			return &model.Reference{Kind: model.KindConfigKey, Text: s, Norm: norm, Confidence: model.Low}
		case x.hints.HasPython() && len(parts) == 2 && isCapitalized(first):
			// Class.method in a Python repo
			return &model.Reference{Kind: model.KindPySym, Text: s, Norm: norm, Confidence: model.Medium}
		case x.hints.ModulePath() != "":
			return &model.Reference{Kind: model.KindGoSymbol, Text: s, Norm: norm, Confidence: model.Low}
		}
		return nil
	}
	if m := reCall.FindStringSubmatch(s); m != nil {
		name := m[1]
		switch {
		case x.hints.HasOdin() && isSnake(name):
			return &model.Reference{Kind: model.KindOdinSym, Text: s, Norm: name, Confidence: model.Medium}
		case x.hints.HasPython() && isSnake(name):
			return &model.Reference{Kind: model.KindPySym, Text: s, Norm: name, Confidence: model.Medium}
		case x.hints.ModulePath() != "":
			return &model.Reference{Kind: model.KindGoSymbol, Text: s, Norm: name, Confidence: model.Medium}
		}
	}
	return nil
}

func isCapitalized(s string) bool { return s != "" && s[0] >= 'A' && s[0] <= 'Z' }

func isSnake(s string) bool {
	return strings.ToLower(s) == s && strings.Contains(s, "_")
}

func (x *extractor) isOdinPackage(name string) bool {
	if x.odinPkgs == nil {
		x.odinPkgs = map[string]bool{}
		for _, p := range x.hints.OdinPackages() {
			x.odinPkgs[p] = true
		}
	}
	return x.odinPkgs[name]
}

func (x *extractor) isPyModule(name string) bool {
	if x.pyMods == nil {
		x.pyMods = map[string]bool{}
		for _, p := range x.hints.PyModules() {
			x.pyMods[p] = true
			if i := strings.LastIndex(p, "."); i >= 0 {
				x.pyMods[p[i+1:]] = true
			}
		}
	}
	return x.pyMods[name]
}

// classifyTokens scans a multi-token span (an expression, a command line,
// a list) for embedded references.
func (x *extractor) classifyTokens(s string) []model.Reference {
	if len(s) > 200 {
		return nil
	}
	var out []model.Reference
	seen := map[string]bool{}
	add := func(r *model.Reference) {
		if r == nil {
			return
		}
		k := string(r.Kind) + "|" + r.Norm
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, *r)
	}
	for _, m := range reInToken.FindAllString(s, -1) {
		// skip float literals and ellipses
		if strings.ContainsAny(m[:1], "0123456789") {
			continue
		}
		if r := x.symbolRef(m); r != nil && r.Kind != model.KindConfigKey {
			if r.Confidence > model.High {
				r.Confidence = model.High
			}
			add(r)
		}
	}
	for _, m := range reInFlag.FindAllStringSubmatch(s, -1) {
		tok := m[1]
		if fm := reFlag.FindStringSubmatch(tok); fm != nil {
			conf := model.Medium
			if fm[1] == "-" {
				conf = model.Low
			}
			add(&model.Reference{Kind: model.KindFlag, Text: tok, Norm: normFlag(fm[2]), Confidence: conf})
		}
	}
	for _, m := range reInEnv.FindAllStringSubmatch(s, -1) {
		add(&model.Reference{Kind: model.KindEnv, Text: m[1], Norm: m[1], Confidence: model.Medium})
	}
	for _, m := range reInPath.FindAllStringSubmatch(s, -1) {
		tok := m[1]
		if r := x.pathRef(tok, true); r != nil {
			add(r)
		}
	}
	return out
}
