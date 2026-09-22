package markdown

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
)

// HeadingSig is one heading in the structural fingerprint: only its level
// and its position matter, never its text (translations rename headings).
type HeadingSig struct {
	Level   int `json:"level"`
	Ordinal int `json:"ordinal"` // 1-based position in document order
}

// CodeSig is one fenced code block: its language and the SHA-256 of its
// content. Code is expected to be identical across translations.
type CodeSig struct {
	Lang   string `json:"lang"`
	SHA256 string `json:"sha256"`
}

// TableSig is the shape of one table.
type TableSig struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

// Fingerprint is the structural signature of a document, used by the pairs
// stage to compare a source document with its translation.
type Fingerprint struct {
	Headings []HeadingSig `json:"headings"`
	Codes    []CodeSig    `json:"codes"`
	Links    []string     `json:"links"`  // sorted unique targets, "#anchor" excluded
	Images   []string     `json:"images"` // sorted unique targets
	Tables   []TableSig   `json:"tables"`
	Numbers  []string     `json:"numbers"` // sorted unique numbers/versions in prose
}

var numberRe = regexp.MustCompile(`\b\d+(?:\.\d+)*\b`)

// Fingerprint computes the document's structural signature.
//
// Headings and code blocks and tables keep document order; link, image and
// number sets are sorted and de-duplicated. Code hashes are taken over the
// block content joined with "\n" after trailing whitespace is trimmed from
// every line, so that indentation-preserving editors do not create false
// differences.
func (d *Doc) Fingerprint() Fingerprint {
	var f Fingerprint

	for i, h := range d.Headings {
		f.Headings = append(f.Headings, HeadingSig{Level: h.Level, Ordinal: i + 1})
	}

	for _, fc := range d.Fences {
		var b strings.Builder
		for i, ln := range fc.Content {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(strings.TrimRight(ln, " \t"))
		}
		sum := sha256.Sum256([]byte(b.String()))
		f.Codes = append(f.Codes, CodeSig{Lang: fc.Lang, SHA256: hex.EncodeToString(sum[:])})
	}

	f.Links = sortedTargets(d.Links, true)
	f.Images = sortedTargets(d.Images, false)

	for _, t := range d.Tables {
		f.Tables = append(f.Tables, TableSig{Rows: t.Rows, Cols: t.Cols})
	}

	f.Numbers = d.Numbers()
	return f
}

// Numbers returns the sorted unique integers and dotted versions that
// appear in prose. Fenced code blocks, front matter and link targets are
// excluded; inline code spans are not.
func (d *Doc) Numbers() []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range d.numLines {
		if line == "" {
			continue
		}
		for _, m := range numberRe.FindAllString(line, -1) {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	sort.Strings(out)
	return out
}

// sortedTargets collects unique link targets. When dropAnchors is set,
// links that point only at a heading in the same document ("#intro") are
// skipped, because translations legitimately have different slugs.
func sortedTargets(links []Link, dropAnchors bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range links {
		t := l.Target
		if t == "" || seen[t] {
			continue
		}
		if dropAnchors && strings.HasPrefix(t, "#") {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
