// Package config builds an index of dotted configuration key paths found in
// JSON (and JSON-with-"//"-comments) sample files, so that docrot can check
// whether a documentation reference to a config key ("server.addr") still
// exists in a sample config.
//
// Files are selected by glob patterns such as "config.json", "config*.json",
// "*.example.json" or "configs/**/*.json", matched against both the file's
// path relative to root and (for patterns without a "/") its base name.
//
// Once built, an Index is safe for concurrent read-only use.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Stats summarises what Build found.
type Stats struct {
	Files   int // files matched and successfully parsed
	Skipped int // files matched but skipped (invalid JSON)
	Keys    int // number of distinct dotted key paths (every prefix counted)
}

// Index is a queryable snapshot of every dotted key path found in matching
// config sample files under a root directory. Build it once with Build;
// every method is read-only.
type Index struct {
	stats Stats
	files []string // sorted, files successfully parsed (relative, forward slashes)

	exact []string        // sorted, unique dotted key paths, original case, every prefix included
	norm  map[string]bool // normalised (array indexes stripped, lower-cased) key set, for Has
}

// Build walks root, matches every file against patterns (globs; see package
// doc), parses the JSON-ish ones and extracts dotted key paths. It skips
// .git, vendor, node_modules and any path in exclude (prefix match on
// directories, exact match on files).
func Build(root string, patterns []string, exclude []string) (*Index, error) {
	compiled := make([]*globPattern, 0, len(patterns))
	for _, pat := range patterns {
		compiled = append(compiled, compileGlob(pat))
	}

	var candidates []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || isExcludedDir(rel, exclude) {
				return filepath.SkipDir
			}
			return nil
		}
		if isExcludedFile(rel, exclude) {
			return nil
		}
		if matchesAny(compiled, rel) {
			candidates = append(candidates, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(candidates)

	type parsed struct {
		rel  string
		keys map[string]bool
		ok   bool // true when the file was matched and parsed successfully
	}
	results := make([]parsed, len(candidates))

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(candidates) && len(candidates) > 0 {
		workers = len(candidates)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				rel := candidates[i]
				data, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
				if rerr != nil {
					continue
				}
				var v any
				if err := json.Unmarshal(stripLineComments(data), &v); err != nil {
					results[i] = parsed{rel: rel, ok: false}
					continue
				}
				keys := make(map[string]bool)
				collectKeys(v, "", keys)
				results[i] = parsed{rel: rel, keys: keys, ok: true}
			}
		}()
	}
	for i := range candidates {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	ix := &Index{norm: make(map[string]bool)}
	exactSet := make(map[string]bool)
	for _, r := range results {
		if !r.ok {
			ix.stats.Skipped++
			continue
		}
		ix.stats.Files++
		ix.files = append(ix.files, r.rel)
		for k := range r.keys {
			exactSet[k] = true
		}
	}
	sort.Strings(ix.files)

	for k := range exactSet {
		ix.exact = append(ix.exact, k)
		ix.norm[normalizeKey(k)] = true
	}
	sort.Strings(ix.exact)
	ix.stats.Keys = len(ix.exact)

	return ix, nil
}

// collectKeys recursively walks a decoded JSON value and records every
// dotted key path, including intermediate (non-leaf) paths. Arrays descend
// into their elements' object keys under the same path, without an index.
func collectKeys(v any, prefix string, out map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, vv := range t {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			out[p] = true
			collectKeys(vv, p, out)
		}
	case []any:
		for _, elem := range t {
			collectKeys(elem, prefix, out)
		}
	}
}

var reArrayIndex = regexp.MustCompile(`\[\d+\]`)

// normalizeKey strips array-index suffixes ("a[0].b" -> "a.b") and
// lower-cases the result, for case-insensitive, index-insensitive lookup.
func normalizeKey(s string) string {
	s = reArrayIndex.ReplaceAllString(s, "")
	return strings.ToLower(s)
}

// stripLineComments removes "//" line comments that are outside JSON string
// literals, using a small state machine, so that .jsonc-like sample files
// can be parsed with encoding/json.
func stripLineComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			continue
		}
		out = append(out, c)
	}
	return out
}

func isExcludedDir(rel string, exclude []string) bool {
	for _, e := range exclude {
		e = cleanExclude(e)
		if e == "" {
			continue
		}
		if rel == e || strings.HasPrefix(rel, e+"/") {
			return true
		}
	}
	return false
}

func isExcludedFile(rel string, exclude []string) bool {
	for _, e := range exclude {
		e = cleanExclude(e)
		if e == "" {
			continue
		}
		if rel == e {
			return true
		}
	}
	return false
}

func cleanExclude(e string) string {
	e = filepath.ToSlash(e)
	return strings.Trim(e, "/")
}

// Empty reports whether the index has no keys (no file matched any pattern,
// or every matching file failed to parse).
func (ix *Index) Empty() bool { return len(ix.exact) == 0 }

// Keys returns every dotted key path, sorted and deduplicated, including
// every intermediate prefix (e.g. both "server" and "server.addr").
func (ix *Index) Keys() []string {
	out := make([]string, len(ix.exact))
	copy(out, ix.exact)
	return out
}

// Files returns every file (relative, forward slashes) that matched a
// pattern and was parsed successfully, sorted.
func (ix *Index) Files() []string {
	out := make([]string, len(ix.files))
	copy(out, ix.files)
	return out
}

// Stats returns index-wide counters.
func (ix *Index) Stats() Stats { return ix.stats }

// Has reports whether dotted exists, ignoring case and array-index
// suffixes ("a[0].b" matches the same key as "a.b").
func (ix *Index) Has(dotted string) bool {
	return ix.norm[normalizeKey(dotted)]
}

// Similar returns up to n existing dotted key paths close (Damerau-
// Levenshtein distance <= max(2, len/4) on the normalised form) to dotted.
// Best (smallest distance) first; ties broken alphabetically.
func (ix *Index) Similar(dotted string, n int) []string {
	if n <= 0 {
		return nil
	}
	target := normalizeKey(dotted)
	threshold := len([]rune(target)) / 4
	if threshold < 2 {
		threshold = 2
	}

	type cand struct {
		dist int
		key  string
	}
	seen := make(map[string]bool)
	var cands []cand
	for _, k := range ix.exact {
		nk := normalizeKey(k)
		if seen[nk] {
			continue
		}
		seen[nk] = true
		dist := damerauLevenshtein(target, nk)
		if dist <= threshold {
			cands = append(cands, cand{dist: dist, key: k})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].key < cands[j].key
	})
	if len(cands) > n {
		cands = cands[:n]
	}
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.key
	}
	return out
}

// damerauLevenshtein computes the optimal-string-alignment edit distance
// between a and b (insert, delete, substitute, adjacent transposition).
func damerauLevenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := d[i-1][j] + 1
			ins := d[i][j-1] + 1
			sub := d[i-1][j-1] + cost
			best := del
			if ins < best {
				best = ins
			}
			if sub < best {
				best = sub
			}
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				if t := d[i-2][j-2] + cost; t < best {
					best = t
				}
			}
			d[i][j] = best
		}
	}
	return d[la][lb]
}

// --- tiny self-contained doublestar glob matcher ---
//
// Syntax: '*' matches within one path segment; '**' matches zero or more
// whole path segments; everything else is literal. Patterns are matched
// against the candidate path with forward slashes. A pattern containing no
// '/' also matches the candidate's base name in any directory.

type globPattern struct {
	raw      string
	segments []string
	baseOnly bool // pattern has no '/', so it may match a base name anywhere
}

func compileGlob(pattern string) *globPattern {
	clean := strings.Trim(filepath.ToSlash(pattern), "/")
	return &globPattern{
		raw:      pattern,
		segments: strings.Split(clean, "/"),
		baseOnly: !strings.Contains(clean, "/"),
	}
}

func matchesAny(patterns []*globPattern, rel string) bool {
	for _, p := range patterns {
		if p.match(rel) {
			return true
		}
	}
	return false
}

func (p *globPattern) match(rel string) bool {
	segs := strings.Split(strings.Trim(rel, "/"), "/")
	if matchSegments(p.segments, segs) {
		return true
	}
	if p.baseOnly {
		base := segs[len(segs)-1]
		return matchSegments(p.segments, []string{base})
	}
	return false
}

// matchSegments matches pattern segments against name segments, where "**"
// consumes zero or more whole name segments and "*" (within matchSegment)
// matches any run of characters within a single segment.
func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegments(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 || !matchSegment(pat[0], name[0]) {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// matchSegment matches a single non-"**" pattern segment (which may contain
// "*") against a single name segment, without crossing '/'.
func matchSegment(pat, name string) bool {
	// Standard "*"-only glob matching via dynamic programming.
	dp := make([]bool, len(name)+1)
	dp[0] = true
	for _, pc := range pat {
		ndp := make([]bool, len(name)+1)
		if pc == '*' {
			any := false
			for i := 0; i <= len(name); i++ {
				any = any || dp[i]
				ndp[i] = any
			}
		} else {
			for i := 0; i < len(name); i++ {
				if dp[i] && rune(name[i]) == pc {
					ndp[i+1] = true
				}
			}
		}
		dp = ndp
	}
	return dp[len(name)]
}
