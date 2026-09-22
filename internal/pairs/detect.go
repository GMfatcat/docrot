// Package pairs finds source/translation document pairs and compares their
// structure, so that a translated README that drifted away from the original
// is reported before a reader trusts it (design §12).
//
// Nothing here judges translation quality: only structure (headings, code
// blocks, links, tables, numbers) and git lag are compared.
package pairs

import (
	"path"
	"sort"
	"strings"
)

// Pair ties a source document to its translation. Both are repo-relative
// paths with forward slashes.
type Pair struct {
	Source      string `json:"source"`
	Translation string `json:"translation"`
}

// StemPlaceholder is what a pattern uses to stand for the source path
// without its ".md" extension.
const StemPlaceholder = "{stem}"

// LangSiblings are the directory names that a path segment "en" is paired
// with by the directory convention (docs/en/x.md ↔ docs/zh/x.md).
var LangSiblings = []string{"zh", "zh-TW", "zh-CN", "ja", "ko"}

// Detect returns the source/translation pairs among docs.
//
// Explicit pairs come first and are kept only when both files are present in
// docs. Then every document is tried as a source against the patterns: each
// pattern must contain [StemPlaceholder], which is replaced by the source
// path without its ".md" extension, so "README.md" with "{stem}-zh.md"
// yields "README-zh.md" and "docs/a.md" with "{stem}.zh-TW.md" yields
// "docs/a.zh-TW.md". Finally the directory convention is applied: a path
// segment that is exactly "en" is mapped to a sibling segment from
// [LangSiblings], at any depth.
//
// A document is used as a translation at most once (first match wins) and
// the result is sorted by source, then translation.
func Detect(docs []string, explicit []Pair, patterns []string) []Pair {
	set := make(map[string]bool, len(docs))
	for _, d := range docs {
		if n := normPath(d); n != "" {
			set[n] = true
		}
	}

	paired := make(map[string]bool)
	var out []Pair
	add := func(src, tr string) {
		if src == "" || tr == "" || src == tr || paired[tr] || !set[src] || !set[tr] {
			return
		}
		paired[tr] = true
		out = append(out, Pair{Source: src, Translation: tr})
	}

	for _, p := range explicit {
		add(normPath(p.Source), normPath(p.Translation))
	}

	sources := make([]string, 0, len(set))
	for d := range set {
		sources = append(sources, d)
	}
	sort.Strings(sources)

	for _, src := range sources {
		stem := strings.TrimSuffix(src, ".md")
		for _, pat := range patterns {
			if !strings.Contains(pat, StemPlaceholder) {
				continue
			}
			add(src, normPath(strings.ReplaceAll(pat, StemPlaceholder, stem)))
		}
		for _, cand := range langDirs(src) {
			add(src, cand)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Translation < out[j].Translation
	})
	return out
}

// langDirs returns the candidate translations of src obtained by replacing
// one path segment "en" with each name in LangSiblings.
func langDirs(src string) []string {
	segs := strings.Split(src, "/")
	var out []string
	for i, seg := range segs {
		if seg != "en" {
			continue
		}
		for _, lang := range LangSiblings {
			cand := make([]string, len(segs))
			copy(cand, segs)
			cand[i] = lang
			out = append(out, strings.Join(cand, "/"))
		}
	}
	return out
}

// normPath makes a path comparable: forward slashes, no "./" prefix.
func normPath(p string) string {
	s := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	s = strings.TrimPrefix(s, "./")
	if s == "" || s == "." {
		return ""
	}
	return path.Clean(s)
}
