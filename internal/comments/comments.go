// Package comments checks the comment attached to a declaration against the
// declaration itself: whether the code moved on after the comment was last
// edited (stale-comment, needs git) and whether the comment names things
// that no longer exist (comment-mentions-missing, no git needed).
//
// It runs "on the side": the engine hands it the declarations that documents
// referred to, so a repository is never scanned for comments wholesale
// unless `docrot comments` asks for it.
package comments

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"docrot/internal/extract"
	"docrot/internal/gitx"
	"docrot/internal/model"
)

// Options tunes the checks.
type Options struct {
	MinChurn        int            // distinct commits newer than the comment needed (default 2)
	MinFrac         float64        // or ≥1 commit and this fraction of body lines newer (default 0.5)
	StaleSeverity   model.Severity // default info
	MentionSeverity model.Severity // default warning
	Workers         int            // parallel blames (default 4)
}

// Lookup is the part of model.Index the mention check needs.
type Lookup interface {
	FileExists(rel string) bool
	DirExists(rel string) bool
	IsGoPackage(name string) bool
	SimilarPaths(rel string, n int) []string
	HasGoSymbol(qualified string) bool
	HasPySymbol(qualified string) bool
	HasOdinSymbol(qualified string) bool
	HasFlag(name string) bool
	HasEnv(name string) bool
	HasJSONKey(dotted string) bool
	HasConfigKey(dotted string) bool
	HasLiteral(s string) bool
}

// ReadLines returns the lines of a source file (relative path) or nil.
type ReadLines func(rel string) []string

var (
	reBacktick = regexp.MustCompile("`([^`\n]+)`")
	// identifier-like bare tokens: snake_case, camelCase, dotted, --flags,
	// paths. Plain English words are deliberately not matched.
	reBareIdent = regexp.MustCompile(`(?:^|[\s(,;:])(--?[A-Za-z][\w-]+|[A-Za-z_][\w]*(?:\.[A-Za-z_]\w*)+|[a-z]+[A-Z]\w*|[A-Za-z]\w*_\w+|(?:\./|\.\./)?[\w.-]+/[\w./-]+)`)
	reWord      = regexp.MustCompile(`[A-Za-z_][\w]*`)
)

// builtins and other words that look like identifiers but are language
// vocabulary, never a claim about this code.
var skipTokens = map[string]bool{
	"nil": true, "true": true, "false": true, "err": true, "error": true, "string": true, "int": true,
	"int64": true, "int32": true, "uint": true, "uint64": true, "byte": true, "bool": true, "float64": true,
	"rune": true, "map": true, "chan": true, "func": true, "struct": true, "interface": true, "any": true,
	"iota": true, "None": true, "True": true, "False": true, "self": true, "cls": true, "TODO": true,
	"FIXME": true, "NOTE": true, "XXX": true, "e.g": true, "i.e": true, "etc": true, "vs": true,
	"__init__": true, "__all__": true, "__name__": true, "__main__": true,
	// keywords
	"default": true, "case": true, "switch": true, "range": true, "select": true, "defer": true,
	"go": true, "return": true, "break": true, "continue": true, "type": true, "var": true,
	"const": true, "import": true, "package": true, "if": true, "else": true, "for": true,
	"while": true, "def": true, "class": true, "lambda": true, "yield": true, "pass": true,
	"raise": true, "except": true, "try": true, "with": true, "async": true, "await": true,
	"and": true, "or": true, "not": true, "in": true, "is": true, "del": true, "global": true,
	"nonlocal": true, "assert": true, "from": true, "as": true, "elif": true, "finally": true,
	"proc": true, "using": true, "when": true, "foreign": true, "distinct": true,
	// naming conventions named as such
	"snake_case": true, "camelCase": true, "PascalCase": true, "kebab-case": true,
	"UPPER_SNAKE": true, "UPPER_CASE": true, "lower_case": true, "SCREAMING_SNAKE_CASE": true,
	"snake_case_name": true, "CamelCase": true,
	// JSON Schema / OpenAPI vocabulary that reads like camelCase identifiers
	"anyOf": true, "oneOf": true, "allOf": true, "additionalProperties": true, "readOnly": true,
	"writeOnly": true, "exclusiveMinimum": true, "exclusiveMaximum": true, "minLength": true,
	"maxLength": true, "minItems": true, "maxItems": true, "uniqueItems": true, "multipleOf": true,
	"operationId": true, "requestBody": true, "securitySchemes": true,
}

// pyStdlib are Python standard-library modules a docstring may cite.
var pyStdlib = map[string]bool{}

func init() {
	for _, m := range strings.Fields(`typing typing_extensions datetime enum dataclasses collections functools itertools
		os sys re json pathlib asyncio logging math random time uuid decimal fractions io shutil subprocess threading
		multiprocessing socket ssl http urllib email csv sqlite3 unittest pytest contextlib abc inspect types copy
		pickle struct hashlib hmac secrets base64 string textwrap operator warnings argparse configparser tempfile
		glob fnmatch zipfile tarfile gzip heapq bisect array queue weakref numbers statistics ipaddress mimetypes
		platform signal select selectors traceback importlib pkgutil builtins concurrent contextvars zoneinfo tomllib`) {
		pyStdlib[m] = true
	}
}

var tldSegments = map[string]bool{"org": true, "com": true, "io": true, "net": true, "dev": true, "app": true, "ai": true, "local": true, "sh": true, "co": true, "edu": true, "gov": true}

// Analyze runs both checks over spans. repo may be nil (stale-comment is
// then skipped). Spans are deduplicated by file and declaration line.
func Analyze(repo *gitx.Repo, spans []model.SymbolSpan, ix Lookup, read ReadLines, opts Options) []model.Finding {
	if opts.MinChurn <= 0 {
		opts.MinChurn = 2
	}
	if opts.MinFrac <= 0 {
		opts.MinFrac = 0.5
	}
	if opts.StaleSeverity == "" {
		opts.StaleSeverity = model.SevInfo
	}
	if opts.MentionSeverity == "" {
		opts.MentionSeverity = model.SevWarning
	}
	if opts.Workers <= 0 {
		opts.Workers = 4
	}

	// group by file, dedupe
	byFile := map[string][]model.SymbolSpan{}
	seen := map[string]bool{}
	var files []string
	for _, sp := range spans {
		if sp.DocStart == 0 || sp.File == "" || sp.BodyEnd < sp.BodyStart {
			continue
		}
		k := fmt.Sprintf("%s:%d", sp.File, sp.DeclLine)
		kc := fmt.Sprintf("%s:%d-%d", sp.File, sp.DocStart, sp.DocEnd)
		if seen[k] || seen[kc] {
			continue // a grouped var/const block shares one comment: check it once
		}
		seen[k], seen[kc] = true, true
		if _, ok := byFile[sp.File]; !ok {
			files = append(files, sp.File)
		}
		byFile[sp.File] = append(byFile[sp.File], sp)
	}
	sort.Strings(files)

	out := make([][]model.Finding, len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, opts.Workers)
	for i, f := range files {
		wg.Add(1)
		go func(i int, f string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = analyzeFile(repo, f, byFile[f], ix, read, opts)
		}(i, f)
	}
	wg.Wait()
	var all []model.Finding
	for _, fs := range out {
		all = append(all, fs...)
	}
	return all
}

func analyzeFile(repo *gitx.Repo, file string, spans []model.SymbolSpan, ix Lookup, read ReadLines, opts Options) []model.Finding {
	lines := read(file)
	var blame []gitx.BlameLine
	if repo != nil {
		if b, err := repo.BlameLines(file); err == nil {
			blame = b
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].DeclLine < spans[j].DeclLine })
	var out []model.Finding
	for _, sp := range spans {
		out = append(out, mentions(sp, lines, ix, opts)...)
		if blame != nil {
			if f, ok := stale(sp, blame, opts); ok {
				out = append(out, f)
			}
		}
	}
	return out
}

// --- stale-comment ----------------------------------------------------------

func stale(sp model.SymbolSpan, blame []gitx.BlameLine, opts Options) (model.Finding, bool) {
	if sp.DocEnd >= len(blame) || sp.BodyEnd >= len(blame) {
		return model.Finding{}, false
	}
	var commentT time.Time
	for l := sp.DocStart; l <= sp.DocEnd; l++ {
		if blame[l].Time.IsZero() {
			return model.Finding{}, false // being edited right now
		}
		if blame[l].Time.After(commentT) {
			commentT = blame[l].Time
		}
	}
	newer := map[string]time.Time{}
	newerLines, bodyLines := 0, 0
	for l := sp.BodyStart; l <= sp.BodyEnd; l++ {
		if l >= sp.DocStart && l <= sp.DocEnd {
			continue // a docstring inside the body
		}
		bodyLines++
		if blame[l].Time.IsZero() {
			return model.Finding{}, false
		}
		if blame[l].Time.After(commentT) {
			newerLines++
			if t, ok := newer[blame[l].Hash]; !ok || blame[l].Time.After(t) {
				newer[blame[l].Hash] = blame[l].Time
			}
		}
	}
	if bodyLines == 0 || len(newer) == 0 {
		return model.Finding{}, false
	}
	frac := float64(newerLines) / float64(bodyLines)
	if len(newer) < opts.MinChurn && frac < opts.MinFrac {
		return model.Finding{}, false
	}
	type c struct {
		hash string
		t    time.Time
	}
	var cs []c
	for h, t := range newer {
		cs = append(cs, c{h, t})
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].t.After(cs[j].t) })
	var hashes []string
	for i, x := range cs {
		if i == 5 {
			hashes = append(hashes, "…")
			break
		}
		hashes = append(hashes, x.hash[:min(7, len(x.hash))])
	}
	msg := fmt.Sprintf("comment on `%s` last edited %s; the body changed in %d commit%s since (%d/%d lines): %s",
		sp.Qualified, commentT.UTC().Format("2006-01-02"), len(newer), plural(len(newer)), newerLines, bodyLines, strings.Join(hashes, ", "))
	return model.Finding{
		Rule:        model.RuleStaleComment,
		Severity:    opts.StaleSeverity,
		Message:     msg,
		Loc:         model.Location{File: sp.File, Line: sp.DocStart},
		Fingerprint: model.Fingerprint(model.RuleStaleComment, sp.File, sp.Qualified),
		Data: map[string]any{
			"symbol": sp.Qualified, "commentEdited": commentT.UTC().Format(time.RFC3339),
			"newerCommits": len(newer), "newerLines": newerLines, "bodyLines": bodyLines,
		},
	}, true
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// --- comment-mentions-missing ----------------------------------------------

func mentions(sp model.SymbolSpan, lines []string, ix Lookup, opts Options) []model.Finding {
	if len(lines) == 0 {
		return nil
	}
	// text of the declaration itself (signature + body), doc lines excluded
	var body strings.Builder
	for l := sp.BodyStart; l <= sp.BodyEnd && l-1 < len(lines); l++ {
		if l >= sp.DocStart && l <= sp.DocEnd {
			continue
		}
		body.WriteString(lines[l-1])
		body.WriteByte('\n')
	}
	bodyWords := wordSet(body.String())
	var rest strings.Builder // the file without this comment (it would match itself)
	for i, l := range lines {
		if i+1 >= sp.DocStart && i+1 <= sp.DocEnd {
			continue
		}
		rest.WriteString(l)
		rest.WriteByte('\n')
	}
	fileWords := wordSet(rest.String())
	own := wordSet(sp.Qualified)
	for _, p := range sp.Params {
		own[p] = true
	}

	var out []model.Finding
	reported := map[string]bool{}
	for _, tok := range candidates(sp.Doc) {
		norm := strings.Trim(tok, "*&()[]{}<>\"',;:.!?")
		if norm == "" || reported[norm] || skipTokens[norm] || !plausible(norm, ix, sp.Kind) {
			continue
		}
		if known(norm, bodyWords, fileWords, own, ix, sp.Kind, dirOf(sp.File)) {
			continue
		}
		reported[norm] = true
		msg := fmt.Sprintf("comment on `%s` mentions `%s`, which appears neither in the declaration nor anywhere docrot can find", sp.Qualified, norm)
		out = append(out, model.Finding{
			Rule:        model.RuleCommentMentions,
			Severity:    opts.MentionSeverity,
			Message:     msg,
			Loc:         model.Location{File: sp.File, Line: sp.DocStart},
			Fingerprint: model.Fingerprint(model.RuleCommentMentions, sp.File, sp.Qualified, norm),
			Data:        map[string]any{"symbol": sp.Qualified, "mention": norm},
		})
	}
	return out
}

// candidates extracts identifier-like tokens from comment text: anything in
// backticks, plus bare tokens that look like code (snake_case, camelCase,
// dotted names, --flags, paths). Plain words are never candidates.
func candidates(doc []string) []string {
	var out []string
	inFence := false
	for _, line := range doc {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
			continue
		}
		if inFence || isCodeExample(t) {
			continue // doctest / example code inside a docstring is not a claim
		}
		rest := line
		for _, m := range reBacktick.FindAllStringSubmatch(line, -1) {
			t := strings.TrimSpace(m[1])
			if t != "" && !strings.ContainsAny(t, " \t") && len(t) < 80 && !isKebabProse(t) && !isPlainWord(t) && !isFormatPattern(t) {
				out = append(out, t)
			}
			rest = strings.Replace(rest, "`"+m[1]+"`", " ", 1)
		}
		if strings.Contains(rest, "://") {
			continue
		}
		for _, m := range reBareIdent.FindAllStringSubmatch(rest, -1) {
			t := m[1]
			if len(t) < 3 || strings.HasSuffix(t, ".") || isFormatPattern(t) {
				continue
			}
			out = append(out, t)
		}
	}
	return out
}

// plausible filters candidates that cannot be claims about this code:
// struct-tag fragments, slash-separated word lists, stdlib or unknown
// package prefixes (errors.Is, logger.Info), and host names.
func plausible(tok string, ix Lookup, kind model.Kind) bool {
	if strings.ContainsAny(tok, "\"'=") {
		return false // env:"NAME", key=value
	}
	if strings.HasPrefix(tok, "-") && !strings.HasPrefix(tok, "--") {
		return false // -ldflags, -race: some other program's option
	}
	if strings.HasPrefix(tok, "/") || strings.HasPrefix(tok, "@") || strings.Contains(tok, "://") || strings.ContainsAny(tok, "<>|") {
		return false // site-absolute links, decorators, URLs, comparisons
	}
	if isHexish(tok) || isSingleWord(tok) || strings.Contains(tok, "\\") || isNumericLiteral(tok) {
		return false // deadbeef12346, HTCPCP, Host, GET, , 1_000_000
	}
	if i := strings.LastIndex(tok, "/"); i >= 0 && isNumeric(tok[i+1:]) {
		return false // HTTP/1.1, HTCPCP/1.0
	}
	if strings.Contains(tok, "/") && !strings.HasPrefix(tok, "./") && !strings.HasPrefix(tok, "../") {
		// a path claim needs an extension or a real top-level directory;
		// otherwise it is prose like VERIFYING/COMPLETED/FAILED
		first, _, _ := strings.Cut(tok, "/")
		ext := tok[strings.LastIndex(tok, ".")+1:]
		if !(strings.Contains(tok, ".") && len(ext) <= 5 && ext != tok) && (ix == nil || !ix.DirExists(first)) {
			return false
		}
	}
	if strings.Contains(tok, ".") && !strings.Contains(tok, "/") {
		parts := strings.Split(tok, ".")
		first, last := parts[0], parts[len(parts)-1]
		if tldSegments[strings.ToLower(last)] {
			return false // golang.org
		}
		if first == strings.ToLower(first) {
			// lower-case prefix: only meaningful when it is a package or module of this repo
			isPkg := false
			if ix != nil {
				switch kind {
				case model.KindPySym:
					isPkg = ix.HasPySymbol(first) && !pyStdlib[first]
				case model.KindOdinSym:
					isPkg = ix.HasOdinSymbol(first)
				default:
					isPkg = ix.IsGoPackage(first) && !extract.IsStdlibPackage(first)
				}
			}
			if !isPkg {
				return false
			}
		} else if ix != nil {
			// Capitalised owner (Parameter.empty, Client.Push): only when the
			// owner is something this repository declares
			switch kind {
			case model.KindPySym:
				if !ix.HasPySymbol(first) {
					return false
				}
			case model.KindOdinSym:
				if !ix.HasOdinSymbol(first) {
					return false
				}
			default:
				if !ix.HasGoSymbol(first) {
					return false
				}
			}
		}
	}
	return true
}

// isCodeExample recognises docstring lines that are code rather than prose.
func isCodeExample(t string) bool {
	switch {
	case strings.HasPrefix(t, ">>>"), strings.HasPrefix(t, "..."), strings.HasPrefix(t, "@"),
		strings.HasPrefix(t, "def "), strings.HasPrefix(t, "class "), strings.HasPrefix(t, "import "),
		strings.HasPrefix(t, "from "), strings.HasPrefix(t, "print("), strings.HasPrefix(t, "assert "),
		strings.HasPrefix(t, "#"):
		return true
	}
	return strings.Contains(t, " = ") || strings.Contains(t, " == ") || strings.HasSuffix(t, ":") && strings.Contains(t, "(")
}

// isSingleWord reports whether tok is one word without identifier
// punctuation: "Host", "GET", "Cookie" — protocol vocabulary, not a claim.
func isSingleWord(tok string) bool {
	if strings.ContainsAny(tok, "_.-/") {
		return false
	}
	hasLower, hasUpper := false, false
	for _, r := range tok {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= '0' && r <= '9':
			return false
		default:
			return false
		}
	}
	if !hasLower || !hasUpper {
		return true // all lower or all upper
	}
	// Capitalised single word (Cookie) vs camelCase (retryBudget)
	return tok[0] >= 'A' && tok[0] <= 'Z' && strings.ToLower(tok[1:]) == tok[1:]
}

func isHexish(tok string) bool {
	digits := false
	for _, r := range tok {
		switch {
		case r >= '0' && r <= '9':
			digits = true
		case r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return digits && len(tok) >= 6
}

// isNumericLiteral matches 1_000_000 and 0x1F-style literals.
func isNumericLiteral(s string) bool {
	if s == "" || !(s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r == '_' || r == '.' || r == 'x' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r == '.') {
			return false
		}
	}
	return true
}

// isPlainWord reports whether a backticked token is a single lower-case
// word ("addr", "null", "integer"): emphasis, not an identifier claim.
func isPlainWord(t string) bool {
	for _, r := range t {
		if !(r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

// isFormatPattern recognises date/time layout placeholders such as
// YYYYMMDD_HHMMSS or HH:MM.
func isFormatPattern(t string) bool {
	u := strings.Trim(t, "_-")
	if u == "" {
		return false
	}
	for _, r := range u {
		if !strings.ContainsRune("YMDHSZ_-:", r) {
			return false
		}
	}
	return true
}

// isKebabProse reports whether a backticked token is just hyphenated words
// ("definition-ref"), not a flag or identifier.
func isKebabProse(t string) bool {
	if !strings.Contains(t, "-") || strings.HasPrefix(t, "-") {
		return false
	}
	return !strings.ContainsAny(t, "._/()")
}

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range reWord.FindAllString(s, -1) {
		m[w] = true
	}
	return m
}

// known reports whether a mention resolves: as a word in the declaration,
// as a word anywhere in the same file, or through the index by shape.
func dirOf(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return ""
}

func known(tok string, body, file, own map[string]bool, ix Lookup, kind model.Kind, dir string) bool {
	// every identifier component must be present somewhere for a compound
	parts := reWord.FindAllString(tok, -1)
	if len(parts) == 0 {
		return true
	}
	allIn := func(set map[string]bool) bool {
		for _, p := range parts {
			if !set[p] {
				return false
			}
		}
		return true
	}
	if allIn(body) || allIn(own) || allIn(file) {
		return true
	}
	if ix == nil {
		return false
	}
	switch {
	case strings.HasPrefix(tok, "-"):
		return ix.HasFlag(strings.TrimLeft(tok, "-"))
	case strings.Contains(tok, "/"):
		p := strings.TrimPrefix(tok, "./")
		if ix.FileExists(p) || ix.DirExists(p) {
			return true
		}
		if dir != "" && (ix.FileExists(dir+"/"+p) || ix.DirExists(dir+"/"+p)) {
			return true // relative to the file that holds the comment
		}
		for _, s := range ix.SimilarPaths(p, 5) { // exists under a sub-tree
			if strings.HasSuffix(s, "/"+p) {
				return true
			}
		}
		return false
	case strings.ToUpper(tok) == tok && strings.Contains(tok, "_"):
		return ix.HasEnv(tok)
	}
	if ix.HasGoSymbol(tok) || ix.HasPySymbol(tok) || ix.HasOdinSymbol(tok) {
		return true
	}
	if ix.HasLiteral(tok) || ix.HasLiteral(strings.TrimSuffix(tok, "()")) {
		return true // the code spells it as a string: a key, an event, a header
	}
	if strings.Contains(tok, "_") && (ix.HasJSONKey(tok) || ix.HasConfigKey(tok)) {
		return true // a wire/config key named in the comment
	}
	if i := strings.LastIndex(tok, "."); i > 0 && i < len(tok)-1 && !strings.Contains(tok[:i], ".") {
		// main.go, config.json: a file name — anywhere in the repo counts
		for _, p := range ix.SimilarPaths(tok, 5) {
			if strings.EqualFold(p, tok) || strings.HasSuffix(p, "/"+tok) {
				return true
			}
		}
	}
	if strings.Contains(tok, ".") {
		return ix.HasJSONKey(tok) || ix.HasConfigKey(tok)
	}
	// a bare identifier: accept if the index knows any symbol by that name
	switch kind {
	case model.KindPySym:
		return ix.HasPySymbol(tok)
	case model.KindOdinSym:
		return ix.HasOdinSymbol(tok)
	}
	return ix.HasGoSymbol(tok)
}
