// Package anchors indexes the heading slugs of every Markdown document so
// that links of the form "docs/guide.md#usage" or "#usage" can be checked.
//
// Slugs follow GitHub's rules (see markdown.Slug), including CJK headings
// and "-1"/"-2" duplicate suffixes. Lookups are forgiving: besides the exact
// slug an Index also accepts the raw heading text (lower-cased) and a slug
// whose '-' and '_' have been swapped, because different Markdown renderers
// disagree about underscores.
//
// Concurrency: Add mutates the index and must be called from a single
// goroutine while the index is being built. Once building is finished, Has,
// Anchors, Docs and Similar are safe for concurrent use.
package anchors

import (
	"regexp"
	"sort"
	"strings"

	"docrot/internal/markdown"
)

// Index maps documents to the set of anchors they define.
type Index struct {
	docs map[string]*docAnchors
}

var customIDAll = regexp.MustCompile(`\{\s*#([\w-]+)\s*\}`)

type docAnchors struct {
	generated bool            // document contains mkdocstrings "::: " directives
	slugs     []string        // sorted, unique
	set       map[string]bool // exact slugs
	alt       map[string]bool // lower-cased heading text and normalised slugs
}

// New returns an empty Index.
func New() *Index {
	return &Index{docs: map[string]*docAnchors{}}
}

// Add indexes the headings of d under docRel, a repo-relative path with
// forward slashes. Calling Add twice for the same document merges the
// anchors. A nil Doc is ignored.
//
// Add is not safe for concurrent use; build the index from one goroutine.
func (ix *Index) Add(docRel string, d *markdown.Doc) {
	if d == nil {
		return
	}
	key := normPath(docRel)
	da := ix.docs[key]
	if da == nil {
		da = &docAnchors{set: map[string]bool{}, alt: map[string]bool{}}
		ix.docs[key] = da
	}
	addSlug := func(slug string) {
		if slug != "" && !da.set[slug] {
			da.set[slug] = true
			da.slugs = append(da.slugs, slug)
		}
		if slug != "" {
			da.alt[normSlug(slug)] = true
		}
	}
	// explicit ids anywhere: "## Title { #id }", "[](){#id}", standalone
	// "{#id}" lines (MkDocs / Python-Markdown attr_list conventions); and
	// mkdocstrings directives ("::: pkg.module") generate anchors we cannot
	// enumerate, so such documents accept any anchor.
	for _, line := range d.Lines {
		for _, m := range customIDAll.FindAllStringSubmatch(line, -1) {
			addSlug(strings.ToLower(m[1]))
		}
		if strings.HasPrefix(strings.TrimSpace(line), "::: ") {
			da.generated = true
		}
	}
	for _, h := range d.Headings {
		if id := markdown.CustomID(h.Text); id != "" {
			addSlug(id)
		}
		if h.Slug != "" && !da.set[h.Slug] {
			da.set[h.Slug] = true
			da.slugs = append(da.slugs, h.Slug)
		}
		da.alt[normSlug(h.Slug)] = true
		if t := strings.ToLower(strings.TrimSpace(h.Text)); t != "" {
			da.alt[t] = true
			da.alt[normSlug(t)] = true
		}
	}
	sort.Strings(da.slugs)
}

// Has reports whether docRel defines slug. The caller is expected to have
// lower-cased and URL-decoded slug already. Matching falls back to the raw
// heading text and to a '-'/'_'-insensitive comparison.
func (ix *Index) Has(docRel, slug string) bool {
	da := ix.docs[normPath(docRel)]
	if da == nil || slug == "" {
		return false
	}
	if da.generated {
		return true // API docs rendered by mkdocstrings: anchors are not in the source
	}
	if da.set[slug] {
		return true
	}
	s := strings.ToLower(slug)
	if da.set[s] || da.alt[s] {
		return true
	}
	return da.alt[normSlug(s)]
}

// Anchors returns the sorted slugs defined by docRel, or nil. The returned
// slice is a copy and may be modified by the caller.
func (ix *Index) Anchors(docRel string) []string {
	da := ix.docs[normPath(docRel)]
	if da == nil {
		return nil
	}
	out := make([]string, len(da.slugs))
	copy(out, da.slugs)
	return out
}

// Docs returns every indexed document path, sorted.
func (ix *Index) Docs() []string {
	out := make([]string, 0, len(ix.docs))
	for k := range ix.docs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Similar returns up to n anchors of docRel that are plausible corrections
// for slug, best (smallest edit distance, then alphabetical) first. The
// distance budget is max(2, len(slug)/4), matching the resolver's fuzzy
// rule. An exact match is never returned as a suggestion.
func (ix *Index) Similar(docRel, slug string, n int) []string {
	da := ix.docs[normPath(docRel)]
	if da == nil || n <= 0 || slug == "" {
		return nil
	}
	budget := len(slug) / 4
	if budget < 2 {
		budget = 2
	}
	type cand struct {
		slug string
		dist int
	}
	var cands []cand
	for _, s := range da.slugs {
		if s == slug {
			continue
		}
		d := levenshtein(slug, s)
		if d <= budget {
			cands = append(cands, cand{s, d})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].slug < cands[j].slug
	})
	if len(cands) == 0 {
		return nil
	}
	if len(cands) > n {
		cands = cands[:n]
	}
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.slug
	}
	return out
}

// normPath makes a document key: forward slashes, no "./" prefix.
func normPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return p
}

// normSlug folds the differences renderers disagree about: underscores
// become hyphens and runs of hyphens collapse.
func normSlug(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			if prevDash {
				continue
			}
			prevDash = true
		} else {
			prevDash = false
		}
		b.WriteByte(s[i])
	}
	return strings.Trim(b.String(), "-")
}

// levenshtein is a self-contained edit distance over runes, using a single
// rolling row.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
