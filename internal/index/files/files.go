// Package files builds the file-tree index of a repository: the set of
// files and directories below a root, keyed by their path relative to that
// root, always with forward slashes and no leading "./".
//
// The index answers the questions the path resolver asks: does this path
// exist, is it a directory, which paths match this glob, and — when a
// document points at something that is not there — which existing paths
// look like the one the document meant.
//
// Symbolic links are never followed; a link is recorded as a plain entry.
// The ".git" directory is always skipped, on top of the caller's exclude
// globs.
package files

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"docrot/internal/globx"
)

// Index is an immutable snapshot of a file tree. All methods are safe for
// concurrent use once Build has returned.
type Index struct {
	root string

	files map[string]struct{}
	dirs  map[string]struct{}

	fileList []string // sorted
	dirList  []string // sorted
	allList  []string // sorted files + dirs
	topDirs  []string // sorted, first path segment of every directory

	lower  map[string][]string // lower-cased rel path -> real rel paths
	byBase map[string][]string // base name -> rel paths (files and dirs)
	byStem map[string][]string // base name without extension -> rel paths
}

// maxBaseScan caps how many distinct base names SimilarPaths inspects when
// looking for small edit-distance matches, so a huge tree cannot make a
// single suggestion expensive.
const maxBaseScan = 50000

// Build walks root and records every file and directory, skipping ".git"
// and anything matching one of the exclude globs. Exclude patterns are
// matched against the relative slash path with globx.Match, so a pattern
// without any "/" (such as "*.test") also matches a base name in any
// directory. A directory that matches is pruned, so its contents are never
// visited.
func Build(root string, exclude []string) (*Index, error) {
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	pats, err := globx.CompileAll(exclude)
	if err != nil {
		return nil, err
	}
	ix := &Index{
		root:   abs,
		files:  make(map[string]struct{}),
		dirs:   make(map[string]struct{}),
		lower:  make(map[string][]string),
		byBase: make(map[string][]string),
		byStem: make(map[string][]string),
	}
	walkErr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == abs {
				return err
			}
			// Unreadable entry somewhere in the tree: skip it, keep going.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(abs, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			if globx.MatchAnyCompiled(pats, rel) {
				return fs.SkipDir
			}
			ix.addDir(rel)
			return nil
		}
		if globx.MatchAnyCompiled(pats, rel) {
			return nil
		}
		ix.addFile(rel)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	ix.finish()
	return ix, nil
}

// Root returns the absolute root the index was built from.
func (ix *Index) Root() string { return ix.root }

func (ix *Index) addFile(rel string) {
	if _, ok := ix.files[rel]; ok {
		return
	}
	ix.files[rel] = struct{}{}
	ix.fileList = append(ix.fileList, rel)
	ix.record(rel)
}

func (ix *Index) addDir(rel string) {
	if _, ok := ix.dirs[rel]; ok {
		return
	}
	ix.dirs[rel] = struct{}{}
	ix.dirList = append(ix.dirList, rel)
	ix.record(rel)
}

func (ix *Index) record(rel string) {
	low := strings.ToLower(rel)
	ix.lower[low] = append(ix.lower[low], rel)
	base := baseOf(rel)
	ix.byBase[base] = append(ix.byBase[base], rel)
	if stem := stemOf(base); stem != base {
		ix.byStem[stem] = append(ix.byStem[stem], rel)
	}
}

func (ix *Index) finish() {
	sort.Strings(ix.fileList)
	sort.Strings(ix.dirList)
	ix.allList = make([]string, 0, len(ix.fileList)+len(ix.dirList))
	ix.allList = append(ix.allList, ix.fileList...)
	ix.allList = append(ix.allList, ix.dirList...)
	sort.Strings(ix.allList)

	seen := make(map[string]struct{})
	for _, d := range ix.dirList {
		top := d
		if i := strings.IndexByte(d, '/'); i >= 0 {
			top = d[:i]
		}
		if _, ok := seen[top]; ok {
			continue
		}
		seen[top] = struct{}{}
		ix.topDirs = append(ix.topDirs, top)
	}
	sort.Strings(ix.topDirs)
}

// Norm normalises a path the way the index stores it: backslashes become
// forward slashes, a leading "./" and trailing slashes are removed.
func Norm(rel string) string { return globx.Clean(rel) }

// FileExists reports whether rel names an indexed file.
func (ix *Index) FileExists(rel string) bool {
	_, ok := ix.files[Norm(rel)]
	return ok
}

// DirExists reports whether rel names an indexed directory.
func (ix *Index) DirExists(rel string) bool {
	_, ok := ix.dirs[Norm(rel)]
	return ok
}

// Exists reports whether rel names an indexed file or directory.
func (ix *Index) Exists(rel string) bool {
	rel = Norm(rel)
	if _, ok := ix.files[rel]; ok {
		return true
	}
	_, ok := ix.dirs[rel]
	return ok
}

// Files returns every indexed file, sorted.
func (ix *Index) Files() []string { return append([]string(nil), ix.fileList...) }

// Dirs returns every indexed directory, sorted.
func (ix *Index) Dirs() []string { return append([]string(nil), ix.dirList...) }

// TopLevelDirs returns the names of the directories directly under the
// root, sorted.
func (ix *Index) TopLevelDirs() []string { return append([]string(nil), ix.topDirs...) }

// Len returns the number of indexed files and directories.
func (ix *Index) Len() int { return len(ix.files) + len(ix.dirs) }

// Glob returns every indexed file and directory whose whole path matches
// pattern ('*', '?', '[...]' and '**' are supported), sorted. It returns
// nil when nothing matches or when the pattern is malformed. Unlike the
// exclude patterns of Build, Glob is strict: "*.md" matches only
// Markdown files at the root, not "docs/a.md".
func (ix *Index) Glob(pattern string) []string {
	p, err := globx.Compile(pattern)
	if err != nil {
		return nil
	}
	var out []string
	for _, rel := range ix.allList {
		if p.MatchPath(rel) {
			out = append(out, rel)
		}
	}
	return out
}

// scored is one suggestion candidate; lower rank is better.
type scored struct {
	path string
	rank int
}

// Suggestion ranks. Lower is better; a tie is broken by path order.
const (
	rankCaseOnly = 0    // same path, different case
	rankSameBase = 100  // same base name in another directory
	rankNearBase = 1000 // base name within a small edit distance
	rankOtherExt = 2000 // same stem, different extension
)

// SimilarPaths returns up to n existing paths that plausibly are what rel
// was meant to be, best first and without duplicates. The heuristics, in
// order of preference, are:
//
//  1. the same path with different letter case;
//  2. the same base name in another directory, nearest directory first;
//  3. a base name within an edit distance of 2, nearest first;
//  4. when rel has an extension, the same stem with another extension.
//
// rel itself is never suggested.
func (ix *Index) SimilarPaths(rel string, n int) []string {
	rel = Norm(rel)
	if rel == "" || n <= 0 {
		return nil
	}
	base := baseOf(rel)
	dir := dirOf(rel)
	stem := stemOf(base)

	cands := make(map[string]int)
	add := func(p string, rank int) {
		if p == rel {
			return
		}
		if old, ok := cands[p]; !ok || rank < old {
			cands[p] = rank
		}
	}

	// (a) exact match ignoring case.
	for _, p := range ix.lower[strings.ToLower(rel)] {
		add(p, rankCaseOnly)
	}

	// (b) same base name elsewhere.
	for _, p := range ix.byBase[base] {
		add(p, rankSameBase+dirDistance(dir, dirOf(p)))
	}
	// Case-insensitive base name matches count too, one step worse.
	lowBase := strings.ToLower(base)
	if lowBase != base {
		for _, p := range ix.byBase[lowBase] {
			add(p, rankSameBase+1+dirDistance(dir, dirOf(p)))
		}
	}

	// (c) base name within a small edit distance.
	const maxDist = 2
	scanned := 0
	for cand, paths := range ix.byBase {
		if scanned >= maxBaseScan {
			break
		}
		scanned++
		if cand == base {
			continue
		}
		if abs(len(cand)-len(base)) > maxDist {
			continue
		}
		d := levenshtein(strings.ToLower(cand), lowBase, maxDist)
		if d > maxDist {
			continue
		}
		for _, p := range paths {
			add(p, rankNearBase+d*100+dirDistance(dir, dirOf(p)))
		}
	}

	// (d) same stem, different extension.
	if stem != base {
		for _, p := range ix.byStem[stem] {
			if baseOf(p) == base {
				continue
			}
			add(p, rankOtherExt+dirDistance(dir, dirOf(p)))
		}
	}

	if len(cands) == 0 {
		return nil
	}
	list := make([]scored, 0, len(cands))
	for p, r := range cands {
		list = append(list, scored{path: p, rank: r})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].rank != list[j].rank {
			return list[i].rank < list[j].rank
		}
		if len(list[i].path) != len(list[j].path) {
			return len(list[i].path) < len(list[j].path)
		}
		return list[i].path < list[j].path
	})
	if len(list) > n {
		list = list[:n]
	}
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.path
	}
	return out
}

// dirDistance counts how many segments separate two directories: the
// segments of each that are not part of their common prefix.
func dirDistance(a, b string) int {
	if a == b {
		return 0
	}
	as := splitDir(a)
	bs := splitDir(b)
	common := 0
	for common < len(as) && common < len(bs) && as[common] == bs[common] {
		common++
	}
	return (len(as) - common) + (len(bs) - common)
}

func splitDir(d string) []string {
	if d == "" {
		return nil
	}
	return strings.Split(d, "/")
}

func baseOf(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[i+1:]
	}
	return rel
}

func dirOf(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return ""
}

// stemOf strips the final extension from a base name. A leading dot (as in
// ".gitignore") is not treated as an extension.
func stemOf(base string) string {
	i := strings.LastIndexByte(base, '.')
	if i <= 0 {
		return base
	}
	return base[:i]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// levenshtein returns the edit distance between a and b, giving up (and
// returning max+1) as soon as it is clear the distance exceeds max. It is
// deliberately self-contained so this package depends on nothing but
// globx.
func levenshtein(a, b string, max int) int {
	if a == b {
		return 0
	}
	ar := []rune(a)
	br := []rune(b)
	if len(ar) < len(br) {
		ar, br = br, ar
	}
	if len(ar)-len(br) > max {
		return max + 1
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		best := cur[0]
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			v := prev[j-1] + cost
			if d := prev[j] + 1; d < v {
				v = d
			}
			if d := cur[j-1] + 1; d < v {
				v = d
			}
			cur[j] = v
			if v < best {
				best = v
			}
		}
		if best > max {
			return max + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}
