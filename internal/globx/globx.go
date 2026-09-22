// Package globx implements doublestar glob matching for slash-separated
// paths, because the standard library's path.Match has no "**".
//
// Syntax:
//
//	?          matches exactly one character, never '/'
//	*          matches zero or more characters within one path segment
//	[a-z]      character class, as in path.Match
//	**         matches zero or more whole path segments
//
// "**" is only special when it is an entire segment. Because it matches
// zero segments, "**/x" matches "x" at the repo root as well as "a/b/x",
// and "a/**" matches "a" itself plus everything under it.
//
// Patterns and names always use forward slashes and are relative to the
// repo root without a leading "./". A leading "/" in a pattern is ignored
// (every pattern is anchored at the root already) and trailing slashes are
// stripped.
//
// Two matching modes exist:
//
//   - MatchPath is strict: the pattern must match the whole path.
//   - Match adds one convenience: a pattern that contains no "/" at all
//     (for example "*.md" or "*.test") also matches the base name of the
//     path in any directory, so "*.md" matches "docs/a.md". This is what
//     users expect from exclude lists; use MatchPath when you want plain
//     whole-path semantics.
package globx

import (
	"fmt"
	"path"
	"strings"
)

// ErrBadPattern indicates a malformed pattern.
var ErrBadPattern = path.ErrBadPattern

// Pattern is a compiled glob pattern, safe for concurrent use.
type Pattern struct {
	raw     string   // pattern as given
	segs    []string // normalised segments
	baseOK  bool     // true when the pattern has no '/' (base-name convenience)
	literal string   // non-empty when the pattern has no metacharacters
}

// Compile parses pattern once so it can be matched many times.
func Compile(pattern string) (*Pattern, error) {
	segs := split(pattern)
	for i, s := range segs {
		if s == "**" {
			continue
		}
		// "**" inside a larger segment (e.g. "a**b") degrades to "*".
		if strings.Contains(s, "**") {
			s = collapseStars(s)
			segs[i] = s
		}
		if _, err := path.Match(s, "x"); err != nil {
			return nil, &BadPatternError{Pattern: pattern, Err: err}
		}
	}
	segs = collapseDoubleStars(segs)
	p := &Pattern{raw: pattern, segs: segs, baseOK: !strings.Contains(strings.Trim(pattern, "/"), "/")}
	if !hasMeta(pattern) {
		p.literal = strings.Join(segs, "/")
	}
	return p, nil
}

// MustCompile is Compile but panics on a malformed pattern. Intended for
// package-level pattern variables.
func MustCompile(pattern string) *Pattern {
	p, err := Compile(pattern)
	if err != nil {
		panic(err)
	}
	return p
}

// BadPatternError reports a malformed glob pattern.
type BadPatternError struct {
	Pattern string
	Err     error
}

func (e *BadPatternError) Error() string {
	return fmt.Sprintf("globx: bad pattern %q: %v", e.Pattern, e.Err)
}

func (e *BadPatternError) Unwrap() error { return e.Err }

// String returns the pattern as it was given to Compile.
func (p *Pattern) String() string { return p.raw }

// MatchPath reports whether the whole path name matches the pattern.
func (p *Pattern) MatchPath(name string) bool {
	if p == nil {
		return false
	}
	name = Clean(name)
	if p.literal != "" {
		return p.literal == name
	}
	return matchSegs(p.segs, split(name))
}

// Match is MatchPath plus the base-name convenience described in the
// package doc: a pattern without any "/" also matches the base name of
// name in any directory.
func (p *Pattern) Match(name string) bool {
	if p == nil {
		return false
	}
	if p.MatchPath(name) {
		return true
	}
	if !p.baseOK {
		return false
	}
	name = Clean(name)
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return p.MatchPath(name[i+1:])
	}
	return false
}

// MatchPath reports whether name matches pattern as a whole path.
// A malformed pattern never matches.
func MatchPath(pattern, name string) bool {
	p, err := Compile(pattern)
	if err != nil {
		return false
	}
	return p.MatchPath(name)
}

// Match reports whether name matches pattern, allowing a pattern that
// contains no "/" to match the base name of name in any directory.
// A malformed pattern never matches.
func Match(pattern, name string) bool {
	p, err := Compile(pattern)
	if err != nil {
		return false
	}
	return p.Match(name)
}

// MatchAny reports whether name matches at least one of patterns, using
// the convenience semantics of Match.
func MatchAny(patterns []string, name string) bool {
	for _, pat := range patterns {
		if Match(pat, name) {
			return true
		}
	}
	return false
}

// CompileAll compiles every pattern, reporting the first failure.
func CompileAll(patterns []string) ([]*Pattern, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	out := make([]*Pattern, 0, len(patterns))
	for _, pat := range patterns {
		p, err := Compile(pat)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// MatchAnyCompiled reports whether name matches at least one compiled
// pattern, using the convenience semantics of Match.
func MatchAnyCompiled(patterns []*Pattern, name string) bool {
	for _, p := range patterns {
		if p.Match(name) {
			return true
		}
	}
	return false
}

// Clean normalises a path or pattern for matching: backslashes become
// forward slashes, a leading "./" or "/" and trailing slashes are removed
// and repeated slashes collapse.
func Clean(s string) string {
	s = strings.ReplaceAll(s, "\\", "/")
	for strings.HasPrefix(s, "./") {
		s = s[2:]
	}
	s = strings.TrimPrefix(s, "/")
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	s = strings.TrimSuffix(s, "/")
	if s == "." {
		return ""
	}
	return s
}

// HasMeta reports whether s contains any glob metacharacter.
func HasMeta(s string) bool { return hasMeta(s) }

func hasMeta(s string) bool { return strings.ContainsAny(s, "*?[") }

func split(s string) []string {
	s = Clean(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "/")
}

func collapseStars(s string) string {
	for strings.Contains(s, "**") {
		s = strings.ReplaceAll(s, "**", "*")
	}
	return s
}

// collapseDoubleStars removes redundant consecutive "**" segments.
func collapseDoubleStars(segs []string) []string {
	out := segs[:0]
	for i, s := range segs {
		if s == "**" && i > 0 && segs[i-1] == "**" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// matchSegs matches pattern segments against name segments, where "**"
// consumes zero or more name segments.
func matchSegs(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegs(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		ok, err := path.Match(pat[0], name[0])
		if err != nil || !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}
