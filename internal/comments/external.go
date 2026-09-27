package comments

import (
	"os"
	"path/filepath"
	"strings"
)

// WordsOf collects the identifiers and string-literal words of the Go
// sources under roots — the sibling repositories a document's code builds
// against — so that a comment naming `NOT_FOUND` or `isValidRequestID`
// from a sibling's package is not reported as mentioning nothing.
// vendor/, node_modules/, testdata/, hidden directories and test files
// are skipped; files over 2 MiB are skipped too. nil when roots is empty.
func WordsOf(roots []string) map[string]bool {
	if len(roots) == 0 {
		return nil
	}
	words := map[string]bool{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				base := d.Name()
				if p != root && (skipWalk[base] || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_")) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			if info, err := d.Info(); err != nil || info.Size() > 2<<20 {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			for _, w := range reWord.FindAllString(string(data), -1) {
				words[w] = true
			}
			return nil
		})
	}
	return words
}

var skipWalk = map[string]bool{"vendor": true, "node_modules": true, "testdata": true, "dist": true, "third_party": true}

// externalKnown reports whether every identifier component of tok is a
// word of the sibling sources.
func externalKnown(tok string, words map[string]bool) bool {
	if len(words) == 0 {
		return false
	}
	parts := reWord.FindAllString(tok, -1)
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if !words[p] {
			return false
		}
	}
	return true
}
