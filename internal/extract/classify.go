package extract

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"

	"docrot/internal/model"
)

// knownExt is the set of file extensions that make a token look like a
// file path even without a slash.
var knownExt = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`go md json txt ps1 py odin yaml yml toml sh exe dll zip pdb ini cfg
		html css js ts sql csv jsonl log mod sum proto env gif png jpg jpeg svg pdf lock bat cmd
		rs c h cpp hpp cc mtrace gguf safetensors wasm tmpl tpl gotmpl xml ico webp mp4 gz tar bz2 xz
		7z conf service plist rst adoc ipynb pyi mjs cjs tsx jsx vue scss less map
		pem crt key cff whl egg pyc pth cfg jsonc toml ini`) {
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
	// runtime and build artifacts
	"log": true, "logs": true, "out": true, "output": true, "outputs": true, "build": true,
	"bin": true, "target": true, "obj": true, "dist": true, "cache": true, "data": true,
	"temp": true, "run": true, "backup": true, "backups": true, "downloads": true,
	"upload": true, "uploads": true, "coverage": true, "node_modules": true, "vendor": true,
	"release": true, "releases": true, "artifacts": true, "services": true, "app": true,
}

var (
	reURL    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
	reFlag   = regexp.MustCompile(`^(--?)([A-Za-z][A-Za-z0-9_.-]*)(=.*)?$`)
	reEnv    = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+$`)
	reDotted = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)((?:\.[A-Za-z_][A-Za-z0-9_]*)+)(\[[^\]]*\])?(\(.*\))?$`)
	reCall   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(\[[^\]]*\])?\((.*)\)$`)
	// reColons matches a Rust or C++ path: crate::module::item, Type::new(),
	// ns::func, a macro call name!().
	reColons   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*(?:::[A-Za-z_][A-Za-z0-9_]*)+)!?(\(.*\))?$`)
	reInToken  = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+`)
	reInFlag   = regexp.MustCompile(`(?:^|[\s,;(\[])(--?[A-Za-z][A-Za-z0-9_-]*)`)
	reInEnv    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_$])([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+)`)
	reInPath   = regexp.MustCompile(`(?:^|[\s"'(\[=,])((?:\.{1,2}/)?[A-Za-z0-9_.@-]+(?:/[A-Za-z0-9_.@*-]+)+/?)`)
	reGoImport = regexp.MustCompile(`^\s*(?:import\s+)?(?:[A-Za-z_][A-Za-z0-9_]*\s+)?"([^"]+)"`)
	reGoSym    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.])([a-z][A-Za-z0-9_]*)\.([A-Z][A-Za-z0-9_]*)`)
	reGoTypeM  = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.])([A-Z][A-Za-z0-9_]*)\.([A-Z][A-Za-z0-9_]*)`)

	reLineSuffix = regexp.MustCompile(`:\d+(?::\d+)?:?$`)
	reDomainSeg  = regexp.MustCompile(`^[a-z0-9-]+(?:\.[a-z0-9-]+)+$`)
	reAnchorLine = regexp.MustCompile(`^l\d+(?:-l\d+)?$`)
	rePathSym    = regexp.MustCompile(`^((?:[A-Za-z0-9_.-]+/)+)([a-z][A-Za-z0-9_]*)\.([A-Z][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)$`)
)

// hasNonASCIIPunct reports whether s contains a non-ASCII rune that is not
// a letter or digit (full-width brackets, CJK punctuation, dashes…).
func hasNonASCIIPunct(s string) bool {
	for _, r := range s {
		if r > 127 && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// placeholderIdents are names documentation uses to mean "any name".
var placeholderIdents = map[string]bool{
	"Type": true, "Name": true, "Method": true, "Class": true, "Func": true, "Function": true,
	"pkg": true, "package": true, "module": true, "method": true, "func": true, "name": true,
	"field": true, "Field": true, "T": true, "K": true, "V": true, "X": true, "Y": true, "Z": true,
	"Foo": true, "Bar": true, "Baz": true, "foo": true, "bar": true, "baz": true, "MyType": true,
	"Example": true, "example": true, "Something": true, "something": true, "Ident": true,
	"ident": true, "symbol": true, "Symbol": true, "Struct": true, "Iface": true, "Interface": true,
	"flag": true, "option": true, "x": true, "y": true, "z": true, "xxx": true, "value": true,
	"UPPER_SNAKE": true, "ENV_VAR": true, "VAR_NAME": true, "FOO_BAR": true, "SOME_VAR": true,
	"YOUR_VAR": true, "MY_VAR": true, "NAME_HERE": true, "PREFIX_UPPER_SNAKE": true,
}

// placeholderSegments are path segments that mark an illustrative path.
var placeholderSegments = map[string]bool{
	"foo": true, "bar": true, "baz": true, "x": true, "y": true, "z": true, "xx": true, "yy": true,
	"xxx": true, "yyy": true, "placeholder": true, "your": true, "my": true, "some": true,
	"path": true, "to": true, "dummy": true, "sample": true, "whatever": true, "example": true,
}

// IsPlaceholderPath reports whether any segment of p is an illustrative
// name such as foo, x or path/to; the resolver skips such paths when they
// do not exist.
func IsPlaceholderPath(p string) bool {
	for i, seg := range strings.Split(p, "/") {
		stem := seg
		if k := strings.LastIndex(stem, "."); k > 0 {
			stem = stem[:k]
		}
		low := strings.ToLower(stem)
		if placeholderSegments[low] {
			return true
		}
		// "myapp/models.py", "mysite/settings.py": the reader's project
		if i == 0 && len(low) > 2 && strings.HasPrefix(low, "my") && low != "mypy" {
			return true
		}
	}
	return false
}

func isPlaceholderSymbol(norm string) bool {
	for _, part := range strings.Split(norm, ".") {
		if placeholderIdents[part] {
			return true
		}
	}
	return false
}

// tldSegments make "htmx.org" / "example.com" a host name, not a symbol.
var tldSegments = map[string]bool{"org": true, "com": true, "io": true, "net": true, "dev": true, "app": true, "ai": true, "local": true, "sh": true, "co": true}

// externalCommands are programs whose flags say nothing about this repo.
var externalCommands = map[string]bool{
	"go": true, "git": true, "curl": true, "wget": true, "docker": true, "npm": true, "npx": true,
	"pip": true, "python": true, "python3": true, "make": true, "cargo": true, "odin": true,
	"gofmt": true, "golangci-lint": true, "gh": true, "kubectl": true, "ssh": true, "tar": true,
	"zip": true, "pwsh": true, "powershell": true, "node": true, "cmake": true, "gcc": true,
	"clang": true, "rustc": true, "ls": true, "grep": true, "rg": true, "find": true, "sed": true,
	"awk": true, "cd": true, "cp": true, "mv": true, "rm": true, "mkdir": true, "cat": true,
	"sqlite3": true, "psql": true, "openssl": true, "systemctl": true, "journalctl": true,
	"dotnet": true, "java": true, "mvn": true, "gradle": true, "brew": true, "apt": true,
	"choco": true, "winget": true, "scoop": true, "ffmpeg": true, "jq": true, "sc": true,
}

// rejectChars are characters that mark a span as a template, expression or
// natural language rather than a concrete reference.
const rejectChars = "<>{}$%|\"'"

// classifyWhole tries to interpret the whole span as exactly one reference.
// It returns nil when the span is not a single reference.
func (x *extractor) classifyWhole(s string) *model.Reference {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 120 {
		return nil
	}
	// `GET /v1/items`, `/items/{item_id}`: before the template check, since
	// route parameters use braces
	if r := x.routeSpan(s); r != nil {
		return r
	}
	if strings.ContainsAny(s, rejectChars) {
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
	if strings.HasPrefix(s, "*") || strings.HasPrefix(s, "&") {
		// *net/http.Server, &Config{}: pointer markers on a symbol
		if r := x.classifyWhole(strings.TrimLeft(s, "*&")); r != nil && r.Kind != model.KindPath {
			r.Text = s
			return r
		}
		return nil
	}
	// file.md#anchor
	if i := strings.Index(s, "#"); i > 0 && strings.HasSuffix(strings.ToLower(s[:i]), ".md") {
		return x.anchorRef(s[:i], s[i+1:], model.High)
	}
	if m := reFlag.FindStringSubmatch(s); m != nil {
		if placeholderIdents[m[2]] {
			return nil
		}
		conf := model.High
		if m[1] == "-" {
			conf = model.Medium
		}
		return &model.Reference{Kind: model.KindFlag, Text: s, Norm: normFlag(m[2]), Confidence: conf}
	}
	if reEnv.MatchString(s) {
		if placeholderIdents[s] {
			return nil
		}
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
	// internal/api.Server → symbol Server in package api (dir-qualified)
	if m := rePathSym.FindStringSubmatch(s); m != nil && !knownExt[strings.ToLower(m[3])] {
		conf := model.Low
		if x.hints.IsGoPackage(m[2]) && x.isTopLevelDir(strings.SplitN(m[1], "/", 2)[0]) {
			conf = model.High
		}
		return &model.Reference{Kind: model.KindGoSymbol, Text: s, Norm: m[2] + "." + m[3], Confidence: conf}
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
	if hasNonASCIIPunct(s) {
		return nil
	}
	p := cleanPath(s)
	if p == "" || p == "." || p == ".." || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") {
		return nil
	}
	if len(p) > 1 && p[1] == ':' { // Windows drive
		return nil
	}
	// grep-style "file.go:12:" and "file.go:12:3" suffixes
	p = reLineSuffix.ReplaceAllString(p, "")
	if strings.Contains(p, ":") {
		return nil // host:port, URL-ish or PowerShell drive
	}
	if strings.HasSuffix(p, "/...") {
		p = strings.TrimSuffix(p, "/...")
	}
	if strings.Contains(p, "...") || strings.Contains(p, "//") {
		return nil
	}
	if strings.HasSuffix(p, "**") && !strings.HasSuffix(p, "/**") {
		return nil // "cgo/gcc**" is bold markup gone wrong, not a glob
	}
	hasSlash := strings.Contains(p, "/")
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(p), "."))
	segs := strings.Split(p, "/")
	first := segs[0]
	isTop := x.isTopLevelDir(first)
	if hasSlash && !isTop && reDomainSeg.MatchString(first) && !strings.HasPrefix(s, "./") {
		return nil // ghcr.io/org/image, github.com/x/y, largan.local/mod/pkg
	}
	if hasSlash && placeholderFirstSegments[strings.ToLower(first)] && !isTop {
		return nil
	}
	hasGlob := strings.ContainsAny(p, "*?")
	switch {
	case hasSlash && (knownExt[ext] || hasGlob):
		return x.mkPath(s, p, confCap(model.High, loose))
	case hasSlash && strings.HasSuffix(s, "/"):
		// "services/aoi-api/" — a directory claim; only high when anchored
		if isTop {
			return x.mkPath(s, p, confCap(model.High, loose))
		}
		return x.mkPath(s, p, model.Medium)
	case hasSlash:
		// a/b with no extension: only a path claim when it is anchored to a
		// real top-level directory or written explicitly (./a/b). Otherwise
		// it is prose ("health/ready", "net/http", "feat/x").
		if isTop {
			return x.mkPath(s, p, confCap(model.High, loose))
		}
		if strings.HasPrefix(s, "./") || strings.HasPrefix(s, `.\`) {
			return x.mkPath(s, p, model.Medium)
		}
		return nil
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

// symbolRef classifies dotted names, "::" paths and calls as Go symbols,
// symbols of a model.Langs language, or config keys. The Go index is asked
// first; then every present language in model.Langs order, so that a name
// which fits several languages is attributed to the first (the resolver
// still consults every language before reporting a miss).
func (x *extractor) symbolRef(s string) *model.Reference {
	s = strings.TrimLeft(s, "*&")
	if m := reColons.FindStringSubmatch(s); m != nil {
		return x.colonRef(s, m[1])
	}
	if m := reDotted.FindStringSubmatch(s); m != nil {
		first := m[1]
		rest := strings.TrimPrefix(m[2], ".")
		parts := append([]string{first}, strings.Split(rest, ".")...)
		norm := strings.Join(parts, ".")
		if len(parts) > 4 || (len(parts) == 2 && (knownExt[strings.ToLower(parts[1])] || tldSegments[strings.ToLower(parts[1])])) {
			return nil
		}
		if isPlaceholderSymbol(norm) {
			return nil
		}
		if IsStdlibPackage(first) && !x.hints.IsGoPackage(first) {
			return nil
		}
		ref := func(kind model.Kind, c model.Confidence) *model.Reference {
			return &model.Reference{Kind: kind, Text: s, Norm: norm, Confidence: c}
		}
		switch {
		case x.hints.IsGoPackage(first):
			return ref(model.KindGoSymbol, model.High)
		case len(parts) == 2 && x.hints.IsGoType(first):
			return ref(model.KindGoSymbol, model.High)
		}
		for _, kind := range x.hints.Languages() {
			if lg, _ := model.LangOf(kind); lg.Sep == "." && x.isNamespace(kind, first) {
				return ref(kind, model.High)
			}
		}
		last := parts[len(parts)-1]
		allLower := strings.ToLower(norm) == norm
		switch {
		case allLower:
			if kind, ok := x.langBy(".", model.NamingSnake, false); ok {
				return ref(kind, model.Low)
			}
			return ref(model.KindConfigKey, model.Low)
		case len(parts) == 2 && isCapitalized(first) && strings.ToLower(last) == last:
			// Class.method with a snake_case method (Go methods are Capitalized)
			if kind, ok := x.langBy(".", model.NamingSnake, true); ok {
				return ref(kind, model.Medium)
			}
		case len(parts) == 2 && isCapitalized(first) && isCamel(last):
			// Client.fetchAll: a JavaScript method
			if kind, ok := x.langBy(".", model.NamingCamel, true); ok {
				return ref(kind, model.Medium)
			}
		case allCapitalized(parts):
			// Namespace.Class.Method, Class.Method: C# when present
			if kind, ok := x.langBy(".", model.NamingPascal, true); ok {
				return ref(kind, model.Medium)
			}
		}
		if x.hints.ModulePath() != "" {
			return ref(model.KindGoSymbol, model.Low)
		}
		return nil
	}
	if m := reCall.FindStringSubmatch(s); m != nil {
		name := m[1]
		if placeholderIdents[name] {
			return nil
		}
		ref := func(kind model.Kind) *model.Reference {
			return &model.Reference{Kind: kind, Text: s, Norm: name, Confidence: model.Medium}
		}
		switch {
		case isSnake(name):
			if kind, ok := x.langBy("", model.NamingSnake, false); ok {
				return ref(kind)
			}
		case isCamel(name):
			if kind, ok := x.langBy("", model.NamingCamel, false); ok {
				return ref(kind)
			}
		}
		if x.hints.ModulePath() != "" {
			return ref(model.KindGoSymbol)
		}
		if isCapitalized(name) {
			if kind, ok := x.langBy("", model.NamingPascal, false); ok {
				return ref(kind)
			}
		}
	}
	return nil
}

// colonRef classifies a "::" path (norm keeps the separators) as a symbol
// of the first present language that qualifies names that way.
func (x *extractor) colonRef(s, norm string) *model.Reference {
	parts := strings.Split(norm, "::")
	if len(parts) > 5 || isPlaceholderSymbol(strings.Join(parts, ".")) {
		return nil
	}
	for _, kind := range x.hints.Languages() {
		lg, _ := model.LangOf(kind)
		if lg.Sep != "::" {
			continue
		}
		if lg.Stdlib[parts[0]] {
			return nil // std::io::Read
		}
		conf := model.Medium
		if x.isNamespace(kind, parts[0]) {
			conf = model.High
		}
		return &model.Reference{Kind: kind, Text: s, Norm: norm, Confidence: conf}
	}
	return nil
}

func isCapitalized(s string) bool { return s != "" && s[0] >= 'A' && s[0] <= 'Z' }

func isSnake(s string) bool {
	return strings.ToLower(s) == s && strings.Contains(s, "_")
}

// isCamel reports a lowerCamelCase identifier: a lower-case start, an
// upper-case letter somewhere after it, no underscore.
func isCamel(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' || strings.Contains(s, "_") {
		return false
	}
	return strings.ToLower(s) != s
}

func allCapitalized(parts []string) bool {
	for _, p := range parts {
		if !isCapitalized(p) {
			return false
		}
	}
	return len(parts) > 0
}

// langBy returns the first present language whose separator is sep (any
// when sep is empty) and whose bare-call naming (or method naming when
// methods is set) includes n.
func (x *extractor) langBy(sep string, n model.Naming, methods bool) (model.Kind, bool) {
	for _, kind := range x.hints.Languages() {
		lg, _ := model.LangOf(kind)
		if sep != "" && lg.Sep != sep {
			continue
		}
		have := lg.Naming
		if methods {
			have = lg.Methods
		}
		if have&n != 0 {
			return kind, true
		}
	}
	return "", false
}

// isNamespace reports whether name is a namespace of the language, or the
// first segment of one ("fastapi" for "fastapi.responses").
func (x *extractor) isNamespace(kind model.Kind, name string) bool {
	if x.nsCache == nil {
		x.nsCache = map[model.Kind]map[string]bool{}
	}
	set, ok := x.nsCache[kind]
	if !ok {
		set = map[string]bool{}
		lg, _ := model.LangOf(kind)
		for _, ns := range x.hints.Namespaces(kind) {
			set[ns] = true
			if i := strings.Index(ns, lg.Sep); i >= 0 {
				set[ns[:i]] = true // the top-level package
			}
		}
		x.nsCache[kind] = set
	}
	return set[name]
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
	fields := strings.Fields(s)
	// flags after an external program describe that program, except for
	// `go run ./cmd/x --flag`, which runs *our* binary.
	flagsMeaningful := len(fields) > 0 && (!externalCommands[fields[0]] || (fields[0] == "go" && len(fields) > 1 && fields[1] == "run"))
	for _, m := range reInFlag.FindAllStringSubmatch(s, -1) {
		if !flagsMeaningful {
			break
		}
		tok := m[1]
		if fm := reFlag.FindStringSubmatch(tok); fm != nil && !placeholderIdents[fm[2]] {
			conf := model.Medium
			if fm[1] == "-" {
				conf = model.Low
			}
			add(&model.Reference{Kind: model.KindFlag, Text: tok, Norm: normFlag(fm[2]), Confidence: conf})
		}
	}
	for _, m := range reInEnv.FindAllStringSubmatch(s, -1) {
		if placeholderIdents[m[1]] {
			continue
		}
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
