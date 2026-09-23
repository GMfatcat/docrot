package gitx

import (
	"sort"
	"strings"
)

// Changed returns the files (relative to root, forward slashes, sorted)
// that differ from HEAD in the index or the work tree, plus untracked files
// that are not ignored. With a non-empty base it also includes every file
// changed on the current branch since its merge base with base (as in a
// pull request: "origin/main"). Deleted files are left out: there is
// nothing to check in them.
func (r *Repo) Changed(base string) ([]string, error) {
	set := map[string]bool{}
	add := func(out []byte) {
		for _, p := range strings.Split(string(out), "\x00") {
			if p == "" {
				continue
			}
			if rel, ok := r.unresolve(p); ok && rel != "" {
				set[rel] = true
			}
		}
	}
	// work tree and index against HEAD; a repository without commits has
	// no HEAD, and then everything tracked counts as untracked below
	if out, err := r.run("diff", "--name-only", "-z", "--diff-filter=d", "HEAD"); err == nil {
		add(out)
	} else if !strings.Contains(err.Error(), "unknown revision") && !strings.Contains(err.Error(), "bad revision") {
		return nil, err
	}
	out, err := r.run("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	add(out)
	if base != "" {
		out, err := r.run("diff", "--name-only", "-z", "--diff-filter=d", base+"...HEAD")
		if err != nil {
			// no merge base (shallow clone, unrelated history): fall back
			// to a plain two-dot diff
			out, err = r.run("diff", "--name-only", "-z", "--diff-filter=d", base, "HEAD")
			if err != nil {
				return nil, err
			}
		}
		add(out)
	}
	list := make([]string, 0, len(set))
	for p := range set {
		list = append(list, p)
	}
	sort.Strings(list)
	return list, nil
}
