// Package literals collects the identifier-like string literals of a code
// base ("request_id", "X-Request-ID", "/openapi.json", "server.port"), the
// last line of defence before docrot reports a documented name as missing:
// log field names, header names, metric names, wire keys, enum strings and
// paths that the code spells only as strings.
package literals

import (
	"regexp"
	"strings"
)

// Set is a lookup table of literals. Add before the first lookup; lookups
// are safe for concurrent use afterwards.
type Set struct {
	m map[string]bool
}

// New returns an empty Set.
func New() *Set { return &Set{m: map[string]bool{}} }

// Add records s when it is identifier-like.
func (s *Set) Add(lit string) {
	if IdentLike(lit) {
		s.m[lit] = true
	}
}

// AddAll records every identifier-like literal in the list.
func (s *Set) AddAll(list []string) {
	for _, l := range list {
		s.Add(l)
	}
}

// Has reports whether lit was recorded, exactly as spelled.
func (s *Set) Has(lit string) bool { return s.m[lit] }

// Len reports how many distinct literals were recorded.
func (s *Set) Len() int { return len(s.m) }

// IdentLike reports whether a string literal is worth indexing: 2–80
// bytes, no whitespace or quotes, at least one letter, and only the
// characters identifiers, paths, dotted keys, header names and flags use.
func IdentLike(s string) bool {
	if len(s) < 2 || len(s) > 80 {
		return false
	}
	letter := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			letter = true
		case c >= '0' && c <= '9', c == '_', c == '-', c == '.', c == '/', c == ':', c == '{', c == '}', c == '*', c == '$', c == '@', c == '+', c == '<', c == '>', c == '~', c == '%':
		default:
			return false
		}
	}
	return letter
}

// Scan returns the identifier-like quoted strings of one source line.
// Comment-only lines (# or //) are skipped; a trailing comment after code
// is scanned, which is harmless. Escapes are honoured, and a string that
// contains one is not identifier-like anyway.
func Scan(line string) []string {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") || !strings.ContainsAny(t, `"'`) {
		return nil
	}
	var out []string
	for i := 0; i < len(line); i++ {
		q := line[i]
		if q != '"' && q != '\'' {
			continue
		}
		j := i + 1
		escaped := false
		for j < len(line) && line[j] != q {
			if line[j] == '\\' {
				escaped = true
				j++ // skip the escaped byte
			}
			j++
		}
		if j >= len(line) {
			break // unterminated: a triple-quoted string or a quote in prose
		}
		if v := line[i+1 : j]; !escaped && IdentLike(v) {
			out = append(out, v)
		}
		i = j
	}
	return out
}

// reTagValue finds the values of a Go struct tag: json:"name,omitempty"
// default:"/docs/". The part before the first comma is the value.
var reTagValue = regexp.MustCompile(`\w+:"([^"]*)"`)

// TagValues returns the identifier-like values of a raw struct tag.
func TagValues(tag string) []string {
	var out []string
	for _, m := range reTagValue.FindAllStringSubmatch(tag, -1) {
		v := m[1]
		if i := strings.IndexByte(v, ','); i >= 0 {
			v = v[:i]
		}
		if IdentLike(v) {
			out = append(out, v)
		}
	}
	return out
}
