// Package routes holds the HTTP routes a repository registers, in a shape
// that lets a documentation claim such as "GET /v1/items/{id}" be checked
// against them regardless of the web framework that declared them.
//
// The language indexes (gosym, py) recognise the registration calls and
// feed a Set; the resolver asks the Set whether a path exists and for which
// methods. Path parameters are normalised so that "/items/{id}",
// "/items/:id", "/items/<int:id>" and "/items/{id:[0-9]+}" are the same
// route, and a documented literal segment ("/items/42") matches a parameter
// segment in the code.
package routes

import (
	"sort"
	"strings"
	"sync"
)

// Route is one registered path.
type Route struct {
	Method string // upper-case, "" for any method
	Path   string // as written in the code, before normalisation
	File   string // relative, forward slashes
	Line   int
	// Prefix marks a router mount point (Group, Mount, PathPrefix, an
	// APIRouter prefix): a documented path equal to it is fine, but paths
	// below it must still match a real route.
	Prefix bool
}

// Match is the answer to a lookup.
type Match struct {
	OK bool
	// Methods lists the methods the path is registered for when it exists
	// but not for the requested method. Empty when OK or when the path is
	// unknown.
	Methods []string
	File    string
	Line    int
	// Mounted is set when the documented path matched a route only by its
	// trailing segments, i.e. the route is probably mounted under a prefix
	// the index did not see.
	Mounted bool
}

// entry is a normalised route.
type entry struct {
	segs   []string // normalised segments; "{}" = one parameter, "**" = the rest
	method string
	route  Route
}

// Set is a queryable collection of routes. Add before the first lookup;
// lookups are safe for concurrent use afterwards.
type Set struct {
	mu      sync.Mutex
	entries []entry
	listed  []string // "METHOD /path" display forms, sorted, built lazily
	paths   []string // distinct normalised paths, for did-you-mean
	built   bool
}

// New returns an empty Set.
func New() *Set { return &Set{} }

// Add records one route. Paths that do not start with "/" get one (Django's
// path("items/", …) omits it); an empty path is ignored.
func (s *Set) Add(r Route) {
	p := strings.TrimSpace(r.Path)
	if p == "" {
		return
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	r.Path = p
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry{segs: Normalize(p), method: strings.ToUpper(r.Method), route: r})
	s.built = false
}

// Len reports how many routes were added.
func (s *Set) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Empty reports whether nothing was added.
func (s *Set) Empty() bool { return s.Len() == 0 }

// Normalize splits a path into segments with parameters canonicalised:
// "{id}", "{id:[0-9]+}", ":id", "<id>", "<int:id>" and "*name" become "{}",
// "{path...}", "{rest:.*}", "*" and a bare "**" become "**" (the rest of
// the path), "{$}" (Go 1.22 exact-match marker) is dropped, a query string
// and a trailing slash are removed. The root path yields no segments.
func Normalize(p string) []string {
	p = strings.TrimSpace(p)
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	var out []string
	for _, seg := range strings.Split(p, "/") {
		if seg == "" {
			continue
		}
		out = append(out, normSeg(seg))
	}
	return out
}

func normSeg(seg string) string {
	switch {
	case seg == "{$}":
		return "{$}"
	case seg == "*" || seg == "**" || strings.HasSuffix(seg, "...}") || strings.HasSuffix(seg, ":.*}") || strings.HasSuffix(seg, ":*}") || strings.HasPrefix(seg, "*") || strings.HasSuffix(seg, "..>"):
		return "**"
	case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"):
		return "{}"
	case strings.HasPrefix(seg, ":") && len(seg) > 1:
		return "{}"
	case strings.HasPrefix(seg, "<") && strings.HasSuffix(seg, ">"):
		return "{}"
	case strings.Contains(seg, "{") && strings.Contains(seg, "}"):
		// "v{version}" or "file.{ext}": a partial parameter; keep the literal part
		return "{}"
	}
	return seg
}

// Lookup reports whether a documented path exists, for the given method
// ("" = any). The path is matched exactly first, then as the tail of a
// registered route (a router mounted under a prefix the index cannot see).
func (s *Set) Lookup(method, p string) Match {
	method = strings.ToUpper(strings.TrimSpace(method))
	want := Normalize(p)
	s.mu.Lock()
	entries := s.entries
	s.mu.Unlock()

	var methods []string
	var mounted *entry
	for i := range entries {
		e := &entries[i]
		if !segsMatch(e.segs, want) {
			if !e.route.Prefix && len(e.segs) > 0 && len(want) > len(e.segs) && segsMatch(e.segs, want[len(want)-len(e.segs):]) {
				if mounted == nil && (method == "" || e.method == "" || e.method == method) {
					mounted = e
				}
			}
			continue
		}
		if method == "" || e.method == "" || e.method == method {
			return Match{OK: true, File: e.route.File, Line: e.route.Line}
		}
		methods = appendUnique(methods, e.method)
	}
	if len(methods) > 0 {
		sort.Strings(methods)
		return Match{Methods: methods}
	}
	if mounted != nil {
		return Match{OK: true, File: mounted.route.File, Line: mounted.route.Line, Mounted: true}
	}
	return Match{}
}

// segsMatch reports whether a documented path (want) is accepted by a
// registered pattern (pat). A "{}" in either side matches one segment; a
// "**" in the pattern matches everything that follows.
func segsMatch(pat, want []string) bool {
	if len(pat) > 0 && pat[len(pat)-1] == "{$}" {
		pat = pat[:len(pat)-1]
	}
	if len(want) > 0 && want[len(want)-1] == "{$}" {
		want = want[:len(want)-1] // the document quotes the Go 1.22 pattern itself
	}
	for i, ps := range pat {
		if ps == "**" {
			return true
		}
		if i >= len(want) {
			return false
		}
		ws := want[i]
		if ps == "{}" || ws == "{}" || ws == "**" || ps == ws {
			continue
		}
		return false
	}
	return len(pat) == len(want)
}

// List returns every route as "METHOD /path" (or "/path" for any method),
// sorted and de-duplicated. Prefix entries are listed with a trailing "/**".
func (s *Set) List() []string {
	s.build()
	return append([]string(nil), s.listed...)
}

// Paths returns the distinct normalised paths ("/items/{}"), sorted; the
// pool for did-you-mean suggestions.
func (s *Set) Paths() []string {
	s.build()
	return append([]string(nil), s.paths...)
}

// Display renders a normalised path for messages.
func Display(segs []string) string {
	if len(segs) == 0 {
		return "/"
	}
	return "/" + strings.Join(segs, "/")
}

func (s *Set) build() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.built {
		return
	}
	seen := map[string]bool{}
	seenPath := map[string]bool{}
	s.listed = s.listed[:0]
	s.paths = s.paths[:0]
	for _, e := range s.entries {
		disp := Display(e.segs)
		if e.route.Prefix {
			disp += "/**"
		}
		key := disp
		if e.method != "" {
			key = e.method + " " + disp
		}
		if !seen[key] {
			seen[key] = true
			s.listed = append(s.listed, key)
		}
		if !seenPath[disp] {
			seenPath[disp] = true
			s.paths = append(s.paths, disp)
		}
	}
	sort.Strings(s.listed)
	sort.Strings(s.paths)
	s.built = true
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// Methods are the HTTP methods docrot recognises in documentation and in
// route registrations.
var Methods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
	"HEAD": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

// SplitPattern splits a Go 1.22 ServeMux pattern ("GET /items/{id}",
// "example.com/x", "/x") into method and path; a host prefix is dropped.
// ok is false when the pattern is not a path at all.
func SplitPattern(pat string) (method, path string, ok bool) {
	pat = strings.TrimSpace(pat)
	if i := strings.IndexByte(pat, ' '); i > 0 && Methods[strings.ToUpper(pat[:i])] {
		method = strings.ToUpper(pat[:i])
		pat = strings.TrimSpace(pat[i+1:])
	}
	if !strings.HasPrefix(pat, "/") {
		// "example.com/path": host-qualified pattern
		if i := strings.IndexByte(pat, '/'); i > 0 && !strings.ContainsAny(pat[:i], " \t") {
			pat = pat[i:]
		} else {
			return "", "", false
		}
	}
	return method, pat, true
}
