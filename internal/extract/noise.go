package extract

import (
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"docrot/internal/markdown"
	"docrot/internal/model"
)

// This file holds the classifier's knowledge of what is *not* a claim,
// gathered from running docrot over the meowbase family of repositories:
// sentences that name a symbol in order to say it does not exist, brace
// patterns that stand for several files, file names with spaces, SQL and
// Go built-in calls, and capitalized "extensions" that are really members.

// Negation cues. A strong cue anywhere in the ~24 characters before the
// span, or a weak cue immediately before it, means the sentence says the
// thing does *not* exist (any more), so its absence is not a lie:
//
//	不提供 `backup.NewTask()`      No `retention.NewTask()`.
//	曾經存在的 `Codec` 介面（`codec.Raw`）    `core/errs` was removed
//	**刪除**：`core/errs`、`core/log`
var (
	reNegStrong = regexp.MustCompile(`(?i)(?:\b(?:removed|deleted|dropped|retired|abandoned|discontinued|obsolete|defunct|formerly|no longer)\b|does\s*n[o']t\s+(?:provide|exist|have|export|define|ship|offer)|there\s+is\s+no|不提供|不存在|不再有|沒有提供|已(?:被)?(?:刪除|移除|拿掉|廢棄|下線)|刪除|移除|拿掉|廢棄|曾經(?:存在)?|原本|舊的|過去的|以前的)`)
	reNegWeak   = regexp.MustCompile(`(?i)(?:\b(?:no|not|never|without|old|legacy|former|deprecated)\b|不是|非)[\s:：*_（(]{0,4}$`)
	reNegAfter  = regexp.MustCompile(`(?i)^.{0,30}?(?:\b(?:was|were|is|are|has been|have been|got|been|now)\s+(?:removed|deleted|dropped|retired|gone)\b|no longer exists?|does\s*n[o']t exist|已(?:被)?(?:刪除|移除|拿掉|廢棄)|不存在|不再存在|已經不在)`)
)

// negWindow is how far (in runes) a strong cue reaches to the right: far
// enough for "曾經存在的 `Codec` 介面（`codec.Proto`／`codec.Raw`）", where the
// cue governs a list of spans.
const negWindow = 48

// reSpanText is a complete code span.
var reSpanText = regexp.MustCompile("`[^`]*`")

// strongBefore reports whether a strong cue governs the span that starts
// right after before. The cue's reach ends at clause punctuation (; . 。 ；)
// and, once another span has intervened, at a colon: in "不提供 `X`：`Y` 本來
// 就是那個抽象" the colon hands over to an explanation that names Y as the
// thing that does exist.
func strongBefore(before string) bool {
	locs := reNegStrong.FindAllStringIndex(before, -1)
	if len(locs) == 0 {
		return false
	}
	// intervening spans collapse to "``" so that the dot in `codec.Proto`
	// does not read as the end of the clause
	seg := reSpanText.ReplaceAllString(before[locs[len(locs)-1][1]:], "``")
	if utf8.RuneCountInString(seg) > negWindow || strings.ContainsAny(seg, ";.。；") {
		return false
	}
	if i := strings.LastIndex(seg, "``"); i >= 0 && strings.ContainsAny(seg[i:], ":：") {
		return false
	}
	return true
}

// negated reports whether the code span sits in a sentence that denies the
// existence of what it names. Only existence claims (symbols, paths,
// commands) are affected; flags, routes and the like keep being checked.
func (x *extractor) negated(sp markdown.Span) bool {
	if sp.Line < 1 || sp.Line > len(x.doc.Lines) {
		return false
	}
	line := x.doc.Lines[sp.Line-1]
	start := sp.Col - 1 // byte index of the span content
	if start < 0 || start > len(line) {
		return false
	}
	before := line[:start]
	if strings.HasSuffix(before, "`") {
		before = before[:len(before)-1]
	}
	after := ""
	if i := strings.Index(line[start:], "`"); i >= 0 {
		after = line[start+i+1:]
	}
	return strongBefore(before) || reNegWeak.MatchString(before) || reNegAfter.MatchString(after)
}

// existenceClaim reports the kinds a negated sentence turns off.
func existenceClaim(k model.Kind) bool {
	switch k {
	case model.KindPath, model.KindCommand, model.KindGoSymbol, model.KindImport:
		return true
	}
	_, ok := model.LangOf(k)
	return ok
}

// reBraces matches `scripts/smoke.{sh,ps1}`: a shell brace alternation
// inside one span, which stands for several files. Route parameters
// (`/items/{id}`) have no comma and never match.
var reBraces = regexp.MustCompile(`^([^{}\s]*)\{([^{}\s,]+(?:,[^{}\s,]+)+)\}([^{}\s]*)$`)

// expandBraces returns the alternatives a brace pattern stands for, or the
// text itself when it has none.
func expandBraces(s string) []string {
	m := reBraces.FindStringSubmatch(s)
	if m == nil {
		return []string{s}
	}
	alts := strings.Split(m[2], ",")
	out := make([]string, 0, len(alts))
	for _, a := range alts {
		out = append(out, m[1]+a+m[3])
	}
	return out
}

// reSpacedField is one word of a file name that contains spaces.
var reSpacedField = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// spacedPath classifies a span such as `docs/Local Artifact Relay PRD
// v0.1.md`: several words, the first with a slash and no extension, the
// last ending in a known extension. A command line never qualifies — its
// first word is a program (`go`, `python`) or a script with an extension.
func (x *extractor) spacedPath(s string) *model.Reference {
	if strings.ContainsAny(s, rejectChars+"()[]#@:") || strings.Contains(s, "://") {
		return nil
	}
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return nil
	}
	first := fields[0]
	if !strings.Contains(first, "/") || knownExt[strings.ToLower(strings.TrimPrefix(path.Ext(first), "."))] {
		return nil
	}
	for _, f := range fields {
		if !reSpacedField.MatchString(f) {
			return nil
		}
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(s), "."))
	if ext == "" || !knownExt[ext] {
		return nil
	}
	return x.mkPath(s, cleanPath(strings.Join(fields, " ")), model.High)
}

// goBuiltins are Go's predeclared functions: `make([]byte, n)`, `close(ch)`
// and `recover()` in prose describe the language, not this module.
var goBuiltins = map[string]bool{
	"make": true, "close": true, "recover": true, "panic": true, "len": true, "cap": true,
	"append": true, "new": true, "delete": true, "copy": true, "min": true, "max": true,
	"clear": true, "print": true, "println": true, "complex": true, "real": true, "imag": true,
}

// callPlaceholders are the names a document gives to "some function" or a
// local variable: `fn(ctx)`, `cancel()`, `cb(err)`.
var callPlaceholders = map[string]bool{
	"fn": true, "f": true, "cb": true, "callback": true, "cancel": true, "done": true,
	"next": true, "handler": true, "handle": true, "run": true, "do": true, "op": true,
	"loop": true, "step": true, "work": true, "task": true, "job": true, "visit": true,
}

// sqlFunctions are SQL built-ins as they appear in prose about queries:
// `MIN(c)`, `typeof(x)`, `wal_checkpoint(TRUNCATE)`, `julianday()`.
var sqlFunctions = map[string]bool{
	"min": true, "max": true, "sum": true, "count": true, "avg": true, "total": true, "abs": true,
	"round": true, "typeof": true, "length": true, "substr": true, "substring": true, "instr": true,
	"upper": true, "lower": true, "trim": true, "ltrim": true, "rtrim": true, "replace": true,
	"printf": true, "format": true, "hex": true, "unhex": true, "quote": true, "random": true,
	"randomblob": true, "zeroblob": true, "coalesce": true, "ifnull": true, "nullif": true, "iif": true,
	"cast": true, "exists": true, "date": true, "time": true, "datetime": true, "julianday": true,
	"unixepoch": true, "strftime": true, "timediff": true, "now": true, "current_timestamp": true,
	"last_insert_rowid": true, "changes": true, "total_changes": true, "sqlite_version": true,
	"group_concat": true, "string_agg": true, "json": true, "json_extract": true, "json_object": true,
	"json_array": true, "json_each": true, "json_tree": true, "json_group_array": true, "json_group_object": true,
	"json_set": true, "json_insert": true, "json_replace": true, "json_remove": true, "json_type": true,
	"json_valid": true, "json_patch": true, "json_array_length": true, "jsonb": true,
	"wal_checkpoint": true, "incremental_vacuum": true, "pragma_table_info": true, "pragma_table_xinfo": true,
	"pragma_index_list": true, "pragma_foreign_key_check": true, "pragma_integrity_check": true,
	"generate_series": true, "bm25": true, "highlight": true, "snippet": true, "row_number": true,
	"rank": true, "dense_rank": true, "ntile": true, "lag": true, "lead": true, "first_value": true,
	"last_value": true, "nth_value": true, "percent_rank": true, "cume_dist": true, "likelihood": true,
	"likely": true, "unlikely": true, "glob": true, "like": true, "char": true, "unicode": true,
	"soundex": true, "load_extension": true, "sqlite_source_id": true, "concat": true, "concat_ws": true,
	"octet_length": true, "sign": true, "floor": true, "ceil": true, "ceiling": true, "pow": true,
	"power": true, "sqrt": true, "mod": true, "exp": true, "ln": true, "log": true, "log10": true, "log2": true,
	"pi": true, "trunc": true, "asin": true, "acos": true, "atan": true, "atan2": true, "sin": true, "cos": true,
	"tan": true, "degrees": true, "radians": true, "gen_random_uuid": true, "uuid": true, "nextval": true,
	"array_agg": true, "unnest": true, "to_char": true, "to_date": true, "extract": true, "date_trunc": true,
	"lpad": true, "rpad": true, "left": true, "right": true, "position": true, "regexp_replace": true,
	"array_length": true, "jsonb_build_object": true, "row_to_json": true, "current_date": true, "current_time": true,
}

// builtinCall reports a bare call that describes a language or SQL rather
// than this module: a Go built-in, a placeholder name or a SQL function.
func builtinCall(name string) bool {
	return goBuiltins[name] || callPlaceholders[name] || sqlFunctions[strings.ToLower(name)]
}

// isAllCaps reports a name spelled entirely in capitals (MIN, TRUNCATE):
// a SQL keyword or aggregate, a C macro when C is present — never a Go
// function.
func isAllCaps(name string) bool {
	return len(name) >= 2 && strings.ToUpper(name) == name && strings.ContainsAny(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
}

// bareExtensions are dotted tokens that name a kind of file, not a file:
// `.tmp`, `.bak`, `.md`. Real dotfiles (`.env`, `.gitignore`) are listed
// in dotfiles and stay paths.
var bareExtensions = map[string]bool{
	"tmp": true, "bak": true, "old": true, "orig": true, "swp": true, "part": true, "new": true,
	// inner segments of generated or variant files: .pb.go, .min.js, .d.ts, .spec.ts
	"pb": true, "pb2": true, "min": true, "d": true, "test": true, "spec": true, "gen": true, "generated": true,
}

// isBareExtension reports a span such as `.tmp` or `.json`: a dot followed
// by a known extension, which speaks about a file type.
func isBareExtension(p string) bool {
	if strings.Contains(p, "/") {
		return false
	}
	if strings.HasPrefix(p, "_") { // `_test.go`, `_windows.go`: a file-name suffix
		if i := strings.LastIndex(p, "."); i > 0 && knownExt[strings.ToLower(p[i+1:])] && isIdent(p[1:i]) {
			return true
		}
		return false
	}
	if !strings.HasPrefix(p, ".") {
		return false
	}
	rest := strings.ToLower(p[1:])
	if dotfiles[rest] {
		return false
	}
	for _, seg := range strings.Split(rest, ".") { // `.pb.go`, `.tar.gz`: every segment an extension
		if !knownExt[seg] && !bareExtensions[seg] {
			return false
		}
	}
	return true
}

// universalGlobals are runtime objects of JavaScript that no Go, Python or
// Rust module is named after: `Date.now`, `Math.max`, `console.log` in a
// document describe the browser or Node, whatever languages the repository
// holds.
var universalGlobals = map[string]bool{
	"console": true, "window": true, "document": true, "Math": true, "Date": true, "JSON": true,
	"Promise": true, "Number": true, "Object": true, "Array": true, "Reflect": true, "Intl": true,
	"navigator": true, "localStorage": true, "sessionStorage": true, "globalThis": true,
}
