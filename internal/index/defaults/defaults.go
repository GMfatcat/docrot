// Package defaults holds the default values the code declares for its
// flags, configuration keys and environment variables, so that a document
// saying "--port (default 8080)" can be checked against flag.Int("port",
// 9090, …), a `default:"…"` struct tag or typer.Option(9090).
package defaults

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Set maps "flag:<name>", "key:<dotted>" and "env:<NAME>" to the default
// as the code spells it (quotes removed). Add before the first lookup.
type Set struct {
	mu sync.Mutex
	m  map[string]string
}

// New returns an empty Set.
func New() *Set { return &Set{m: map[string]string{}} }

// Add records a default; the first declaration of a name wins.
func (s *Set) Add(kind, name, value string) {
	if name == "" || value == "" {
		return
	}
	k := Key(kind, name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[k]; !ok {
		s.m[k] = value
	}
}

// Get returns the default recorded for kind/name.
func (s *Set) Get(kind, name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[Key(kind, name)]
	return v, ok
}

// Len reports how many defaults were recorded.
func (s *Set) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

// List returns "kind:name=value" lines, sorted.
func (s *Set) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.m))
	for k, v := range s.m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// Key normalises a lookup key: flags lose leading dashes and treat "_" and
// "-" alike, keys are lower-cased.
func Key(kind, name string) string {
	name = strings.TrimSpace(name)
	switch kind {
	case "flag":
		name = strings.ToLower(strings.ReplaceAll(strings.TrimLeft(name, "-"), "_", "-"))
	case "key":
		name = strings.ToLower(name)
	}
	return kind + ":" + name
}

// Same reports whether a documented default and a declared default mean
// the same value: quotes and backticks are ignored, booleans and numbers
// compare by value, durations by time.ParseDuration, and the usual ways of
// writing "no value" (empty, "", none, nil, null) are one value.
func Same(doc, code string) bool {
	a, b := norm(doc), norm(code)
	if a == b {
		return true
	}
	if fa, err1 := strconv.ParseFloat(a, 64); err1 == nil {
		if fb, err2 := strconv.ParseFloat(b, 64); err2 == nil {
			return fa == fb
		}
	}
	if da, err1 := time.ParseDuration(a); err1 == nil {
		if db, err2 := time.ParseDuration(b); err2 == nil {
			return da == db
		}
	}
	return false
}

func norm(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, "`")
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		v = v[1 : len(v)-1]
	}
	switch strings.ToLower(v) {
	case "", "none", "nil", "null", "(empty)", "empty", "unset", "not set", "-":
		return ""
	case "true", "yes", "on", "enabled":
		return "true"
	case "false", "no", "off", "disabled":
		return "false"
	}
	return v
}
