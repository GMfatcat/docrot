package gosym

import (
	"go/ast"
	"sort"
	"strings"
)

const (
	// crossPackageScore is the score given to a symbol that carries exactly
	// the wanted name but lives in another package: better than any real
	// edit, worse than an exact hit inside the requested package.
	crossPackageScore = 0.5

	// prefixScoreBase ranks the "shares a long prefix" fallback behind every
	// candidate accepted on edit distance alone. It catches renames such as
	// WriteJSON -> WriteData, where the edit distance is larger than the
	// distance budget but the names obviously belong together.
	prefixScoreBase = 1000.0

	// caseMismatchPenalty demotes a candidate that is exported when the
	// reference is not, or the other way round.
	caseMismatchPenalty = 0.25
)

// candidate is one suggestion with its ranking score.
type candidate struct {
	qual  string
	score float64
}

// Similar returns up to n qualified names close to the given reference, best
// first. Candidates come from the referenced package when the reference names
// one, from the members of the referenced type for a "Type.Member" reference,
// and from the bare set of top level identifiers otherwise. Ranking is by
// Damerau-Levenshtein distance on the last name part, case-insensitively,
// keeping distances up to max(2, len/4). A symbol with the same name in a
// different package is always offered, and a candidate sharing a long prefix
// with the reference is offered last as a rename hint.
func (ix *Index) Similar(qualified string, n int) []string {
	if n <= 0 {
		return nil
	}
	key := normalizeSymbol(qualified)
	if key == "" {
		return nil
	}
	parts := strings.Split(key, ".")
	last := parts[len(parts)-1]
	if last == "" {
		return nil
	}
	lastLower := strings.ToLower(last)
	maxDist := len(last) / 4
	if maxDist < 2 {
		maxDist = 2
	}
	minPrefix := len(last) / 2
	if minPrefix < 3 {
		minPrefix = 3
	}

	pkg, owner := "", ""
	if len(parts) >= 2 && ix.IsPackage(parts[0]) {
		pkg = parts[0]
	}
	if pkg == "" && len(parts) >= 2 && ix.types[parts[len(parts)-2]] {
		owner = parts[len(parts)-2]
	}

	wantExported := ast.IsExported(last)

	// score ranks one candidate name, returning a negative score to reject
	// it. A candidate whose exportedness differs from the reference is
	// slightly demoted, so "WriteJSON" prefers "WriteData" over "writeRaw".
	score := func(raw string) float64 {
		name := strings.ToLower(raw)
		d := damerau(name, lastLower)
		var s float64
		switch {
		case d <= maxDist:
			s = float64(d)
		case commonPrefix(name, lastLower) >= minPrefix:
			s = prefixScoreBase + float64(d)
		default:
			return -1
		}
		if ast.IsExported(raw) != wantExported {
			s += caseMismatchPenalty
		}
		return s
	}

	var cands []candidate
	seen := map[string]bool{}
	for _, e := range ix.entries {
		q := e.qual()
		if q == key || seen[q] {
			continue
		}
		name := strings.ToLower(e.name)
		s := -1.0
		switch {
		case pkg != "":
			if e.pkg == pkg {
				s = score(e.name)
			} else if name == lastLower {
				s = crossPackageScore
			}
		case owner != "":
			if e.owner == owner {
				s = score(e.name)
			}
		case len(parts) >= 2:
			// "Something.Member" with an unknown owner: anything may match.
			s = score(e.name)
		default:
			// Bare references only ever match top level identifiers.
			if e.kind == kindMethod || e.kind == kindField {
				continue
			}
			s = score(e.name)
		}
		if s < 0 {
			continue
		}
		seen[q] = true
		cands = append(cands, candidate{qual: q, score: s})
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score < cands[j].score
		}
		if len(cands[i].qual) != len(cands[j].qual) {
			return len(cands[i].qual) < len(cands[j].qual)
		}
		return cands[i].qual < cands[j].qual
	})
	if len(cands) > n {
		cands = cands[:n]
	}
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.qual)
	}
	return out
}

// commonPrefix returns how many leading runes two strings share.
func commonPrefix(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n := 0
	for n < len(ra) && n < len(rb) && ra[n] == rb[n] {
		n++
	}
	return n
}

// damerau returns the Damerau-Levenshtein distance (optimal string alignment
// variant: insertion, deletion, substitution and transposition of adjacent
// characters) between two strings, counted in runes.
func damerau(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	switch {
	case la == 0:
		return lb
	case lb == 0:
		return la
	}
	// Three rolling rows: two back, one back, current.
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
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			best := prev[j] + 1 // deletion
			if v := cur[j-1] + 1; v < best {
				best = v // insertion
			}
			if v := prev[j-1] + cost; v < best {
				best = v // substitution
			}
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				if v := prev2[j-2] + 1; v < best {
					best = v // transposition
				}
			}
			cur[j] = best
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[lb]
}
