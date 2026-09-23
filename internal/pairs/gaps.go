package pairs

import (
	"sort"
	"strings"

	"docrot/internal/model"
)

// translationSet is one directory-convention translation tree: every
// document under <prefix>en/ is expected under <prefix><lang>/ and vice
// versa. Suffix pairs (README-zh.md) form no set: one untranslated README
// is not a gap.
type translationSet struct {
	prefix string // "docs/" or "" — the part before the language segment
	lang   string // "zh", "ja"…
}

// Gaps reports the holes of the directory-convention translation trees
// that prs reveal: a source page with no counterpart in a language the
// tree does translate into (pair-missing, on the source) and a translation
// whose source no longer exists (pair-orphan, on the translation). docs is
// every discovered document; a tree is only a translation set when at
// least one pair in it was detected, so a repository with a lone docs/zh/
// directory and no docs/en/ gets nothing.
func Gaps(docs []string, prs []Pair, opts Options) []model.Finding {
	set := make(map[string]bool, len(docs))
	for _, d := range docs {
		if n := normPath(d); n != "" {
			set[n] = true
		}
	}
	seen := map[translationSet]bool{}
	var sets []translationSet
	for _, p := range prs {
		ts, ok := dirSet(p)
		if !ok || seen[ts] {
			continue
		}
		seen[ts] = true
		sets = append(sets, ts)
	}
	sort.Slice(sets, func(i, j int) bool {
		if sets[i].prefix != sets[j].prefix {
			return sets[i].prefix < sets[j].prefix
		}
		return sets[i].lang < sets[j].lang
	})

	var out []model.Finding
	for _, ts := range sets {
		srcDir, trDir := ts.prefix+"en/", ts.prefix+ts.lang+"/"
		for _, d := range sortedDocs(set) {
			if rest, ok := strings.CutPrefix(d, srcDir); ok {
				if want := trDir + rest; !set[want] {
					out = append(out, gapFinding(model.RulePairMissing, d, want, opts.severity(model.RulePairMissing),
						"no "+ts.lang+" translation: "+want+" does not exist"))
				}
				continue
			}
			if rest, ok := strings.CutPrefix(d, trDir); ok {
				if want := srcDir + rest; !set[want] {
					out = append(out, gapFinding(model.RulePairOrphan, d, want, opts.severity(model.RulePairOrphan),
						"source "+want+" does not exist; the translation is orphaned"))
				}
			}
		}
	}
	return out
}

// dirSet recognises a pair made by the directory convention: the two paths
// differ in exactly one segment, "en" on the source side and one of
// LangSiblings on the translation side.
func dirSet(p Pair) (translationSet, bool) {
	src := strings.Split(p.Source, "/")
	tr := strings.Split(p.Translation, "/")
	if len(src) != len(tr) {
		return translationSet{}, false
	}
	at := -1
	for i := range src {
		if src[i] == tr[i] {
			continue
		}
		if at >= 0 || src[i] != "en" || !isLangSibling(tr[i]) {
			return translationSet{}, false
		}
		at = i
	}
	if at < 0 {
		return translationSet{}, false
	}
	prefix := ""
	if at > 0 {
		prefix = strings.Join(src[:at], "/") + "/"
	}
	return translationSet{prefix: prefix, lang: tr[at]}, true
}

func isLangSibling(seg string) bool {
	for _, l := range LangSiblings {
		if l == seg {
			return true
		}
	}
	return false
}

func sortedDocs(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// gapFinding builds a whole-document finding on line 1; the fingerprint
// names the counterpart that is missing, so it survives edits to the file.
func gapFinding(rule, file, counterpart string, sev model.Severity, msg string) model.Finding {
	return model.Finding{
		Rule:        rule,
		Severity:    sev,
		Message:     msg,
		Loc:         model.Location{File: file, Line: 1},
		Fingerprint: model.Fingerprint(rule, file, counterpart),
		Data:        map[string]any{"counterpart": counterpart},
	}
}
