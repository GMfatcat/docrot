// Package fuzzy provides the approximate string matching docrot uses to turn
// a failed lookup into a "did you mean …?" suggestion.
//
// The distance metric is Damerau-Levenshtein in its optimal string alignment
// (OSA) form: insertions, deletions, substitutions and transpositions of two
// adjacent characters each cost 1, and no substring is edited more than once.
// Everything works on runes, so multi-byte text (CJK headings, accented
// identifiers) is compared character by character rather than byte by byte.
//
// The package depends on nothing but the standard library.
package fuzzy

import (
	"sort"
	"strings"
)

// Distance returns the Damerau-Levenshtein (optimal string alignment) edit
// distance between a and b, counted in runes. The comparison is exact:
// callers that want case-insensitive behaviour lower-case their inputs first
// (Rank does this for them).
func Distance(a, b string) int {
	if a == b {
		return 0
	}
	ar, br := []rune(a), []rune(b)
	la, lb := len(ar), len(br)
	switch {
	case la == 0:
		return lb
	case lb == 0:
		return la
	}

	// Three rolling rows: prev2 = row i-2, prev = row i-1, cur = row i.
	prev2 := make([]int, lb+1)
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			d := min(cur[j-1]+1, prev[j]+1) // insertion, deletion
			d = min(d, prev[j-1]+cost)      // substitution
			if i > 1 && j > 1 && ar[i-1] == br[j-2] && ar[i-2] == br[j-1] {
				d = min(d, prev2[j-2]+1) // transposition
			}
			cur[j] = d
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[lb]
}

// Candidate is one suggestion produced by Rank. Score is the edit distance to
// the query, so lower is better.
type Candidate struct {
	Text  string
	Score int
}

// DefaultMaxDist returns the distance cutoff Rank applies when it is given a
// non-positive maxDist: max(2, len(q)/4) measured in runes.
func DefaultMaxDist(q string) int {
	return max(2, len([]rune(q))/4)
}

// Rank returns up to n entries of pool that are close to q, best first.
//
// Matching is case-insensitive. Candidates whose distance exceeds maxDist are
// dropped; a maxDist of zero or less means DefaultMaxDist(q). Duplicate pool
// entries are collapsed. Ties on distance are broken by the length of the
// shared (case-insensitive) prefix with q, longer first, and then
// alphabetically, so the result is deterministic for a given pool.
//
// n of zero or less means "no limit".
func Rank(q string, pool []string, n, maxDist int) []Candidate {
	if len(pool) == 0 {
		return nil
	}
	lq := strings.ToLower(q)
	if maxDist <= 0 {
		maxDist = DefaultMaxDist(q)
	}

	type scored struct {
		text   string
		score  int
		prefix int
	}
	seen := make(map[string]struct{}, len(pool))
	hits := make([]scored, 0, 8)
	for _, p := range pool {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		lp := strings.ToLower(p)
		d := Distance(lq, lp)
		if d > maxDist {
			continue
		}
		hits = append(hits, scored{text: p, score: d, prefix: commonPrefix(lq, lp)})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		switch {
		case a.score != b.score:
			return a.score < b.score
		case a.prefix != b.prefix:
			return a.prefix > b.prefix
		}
		return a.text < b.text
	})
	if len(hits) == 0 {
		return nil
	}
	if n > 0 && len(hits) > n {
		hits = hits[:n]
	}
	out := make([]Candidate, len(hits))
	for i, h := range hits {
		out[i] = Candidate{Text: h.text, Score: h.score}
	}
	return out
}

// Best returns the single closest entry of pool to q using Rank's default
// distance cutoff. ok is false when nothing is close enough.
func Best(q string, pool []string) (string, bool) {
	c := Rank(q, pool, 1, 0)
	if len(c) == 0 {
		return "", false
	}
	return c[0].Text, true
}

// commonPrefix returns the number of leading runes a and b share.
func commonPrefix(a, b string) int {
	ar, br := []rune(a), []rune(b)
	n := min(len(ar), len(br))
	for i := 0; i < n; i++ {
		if ar[i] != br[i] {
			return i
		}
	}
	return n
}
