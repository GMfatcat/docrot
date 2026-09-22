package gitx

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Commit is a minimal commit record.
type Commit struct {
	Hash    string    // abbreviated hash
	Time    time.Time // committer time
	Subject string    // first line of the commit message
}

// Head returns the abbreviated hash of HEAD. It fails on a repository with
// no commits.
func (r *Repo) Head() (string, error) {
	v, err := r.once("head", func() (any, error) {
		out, err := r.run("rev-parse", "--short", "HEAD")
		if err != nil {
			return "", err
		}
		return firstLine(out), nil
	})
	s, _ := v.(string)
	return s, err
}

// IsTracked reports whether rel is tracked in the index. Errors (including
// git going away) are reported as false.
func (r *Repo) IsTracked(rel string) bool {
	v, err := r.once("tracked:"+rel, func() (any, error) {
		out, err := r.run("ls-files", "-z", "--", r.resolve(rel))
		if err != nil {
			return false, err
		}
		return len(trimNUL(out)) > 0, nil
	})
	if err != nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

// lastCommit is the cached result of LastCommitTime.
type lastCommit struct {
	t  time.Time
	ok bool
}

// LastCommitTime returns the committer time of the most recent commit that
// touched rel. ok is false when rel is untracked or no commit touches it.
// Renames are not followed (plain git log, which is cheaper).
func (r *Repo) LastCommitTime(rel string) (time.Time, bool, error) {
	v, err := r.once("lastcommit:"+rel, func() (any, error) {
		out, err := r.run("log", "-1", "--format=%ct", "--", r.resolve(rel))
		if err != nil {
			return lastCommit{}, err
		}
		line := firstLine(out)
		if line == "" {
			return lastCommit{}, nil
		}
		secs, perr := strconv.ParseInt(line, 10, 64)
		if perr != nil {
			return lastCommit{}, fmt.Errorf("gitx: bad committer time %q for %s: %w", line, rel, perr)
		}
		return lastCommit{t: time.Unix(secs, 0), ok: true}, nil
	})
	if err != nil {
		return time.Time{}, false, err
	}
	lc, _ := v.(lastCommit)
	return lc.t, lc.ok, nil
}

// CommitsSince lists the commits touching rel whose committer time is
// strictly after since, newest first. git's --since is inclusive at second
// granularity, so the result is post-filtered with Time.After.
func (r *Repo) CommitsSince(rel string, since time.Time) ([]Commit, error) {
	key := "since:" + strconv.FormatInt(since.Unix(), 10) + ":" + rel
	v, err := r.once(key, func() (any, error) {
		args := []string{"log", "--format=%h%x00%ct%x00%s"}
		if !since.IsZero() && since.Unix() > 0 {
			args = append(args, "--since=@"+strconv.FormatInt(since.Unix(), 10))
		}
		args = append(args, "--", r.resolve(rel))
		out, err := r.run(args...)
		if err != nil {
			return []Commit(nil), err
		}
		var commits []Commit
		for _, line := range splitLines(out) {
			parts := strings.Split(line, "\x00")
			if len(parts) < 3 {
				continue
			}
			secs, perr := strconv.ParseInt(parts[1], 10, 64)
			if perr != nil {
				continue
			}
			t := time.Unix(secs, 0)
			if !t.After(since) {
				continue
			}
			commits = append(commits, Commit{Hash: parts[0], Time: t, Subject: parts[2]})
		}
		return commits, nil
	})
	if err != nil {
		return nil, err
	}
	commits, _ := v.([]Commit)
	return commits, nil
}

// BlameLineTimes returns the committer time of the commit that last touched
// each line of rel at HEAD. The slice is 1-based: index 0 is unused and
// index i holds the time of line i, so len(result) == lines+1. Lines that
// are not committed yet (the all-zero blame hash) get the zero time.
//
// The error wraps [ErrUnavailable] when rel is not known to git.
func (r *Repo) BlameLineTimes(rel string) ([]time.Time, error) {
	v, err := r.once("blame:"+rel, func() (any, error) {
		out, err := r.run("blame", "--line-porcelain", "--", r.resolve(rel))
		if err != nil {
			return []time.Time(nil), fmt.Errorf("%w: cannot blame %s: %v", ErrUnavailable, rel, err)
		}
		return parseBlame(out), nil
	})
	if err != nil {
		return nil, err
	}
	times, _ := v.([]time.Time)
	return times, nil
}

// parseBlame reads `git blame --line-porcelain` output into a 1-based slice
// of committer times.
func parseBlame(out []byte) []time.Time {
	times := []time.Time{{}} // index 0 unused
	var (
		cur     time.Time
		zeroSHA bool
		line    int
	)
	for _, l := range splitLines(out) {
		switch {
		case strings.HasPrefix(l, "\t"):
			// Content line: close the current entry.
			if line <= 0 {
				continue
			}
			for len(times) <= line {
				times = append(times, time.Time{})
			}
			if !zeroSHA {
				times[line] = cur
			}
			line = 0
		case strings.HasPrefix(l, "committer-time "):
			if secs, err := strconv.ParseInt(strings.TrimSpace(l[len("committer-time "):]), 10, 64); err == nil {
				cur = time.Unix(secs, 0)
			}
		default:
			if h, n, ok := parseBlameHeader(l); ok {
				zeroSHA = isZeroHash(h)
				cur = time.Time{}
				line = n
			}
		}
	}
	return times
}

// parseBlameHeader recognises a porcelain header line
// "<sha> <origline> <finalline> [<numlines>]" and returns the hash and the
// final line number.
func parseBlameHeader(l string) (hash string, finalLine int, ok bool) {
	f := strings.Fields(l)
	if len(f) < 3 || !isHex(f[0]) || (len(f[0]) != 40 && len(f[0]) != 64) {
		return "", 0, false
	}
	n, err := strconv.Atoi(f[2])
	if err != nil || n < 1 {
		return "", 0, false
	}
	return f[0], n, true
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return len(s) > 0
}

func isZeroHash(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '0' {
			return false
		}
	}
	return len(s) > 0
}

// Renames returns the file renames recorded in history as old→new, with
// paths relative to the root passed to [Open]. Chains are collapsed
// (old→mid→new becomes old→new) and the newest rename of a given old path
// wins. Renames whose endpoints fall outside root are skipped. The result is
// computed once and must not be modified by callers.
func (r *Repo) Renames() (map[string]string, error) {
	v, err := r.once("renames", func() (any, error) {
		out, err := r.run("log", "--diff-filter=R", "--name-status", "--format=", "-M", "-z")
		if err != nil {
			return map[string]string(nil), err
		}
		return r.parseRenames(out), nil
	})
	if err != nil {
		return nil, err
	}
	m, _ := v.(map[string]string)
	return m, nil
}

// parseRenames walks the NUL-separated `--name-status -z` stream. git log
// lists commits newest first, so the first rename seen for an old path wins,
// and a destination already known to have been renamed again is followed to
// its final name.
func (r *Repo) parseRenames(out []byte) map[string]string {
	fields := strings.Split(string(trimNUL(out)), "\x00")
	m := make(map[string]string)
	for i := 0; i < len(fields); i++ {
		status := strings.TrimSpace(fields[i])
		if status == "" || !isRenameStatus(status) {
			continue
		}
		if i+2 >= len(fields) {
			break
		}
		oldTop, newTop := fields[i+1], fields[i+2]
		i += 2
		oldRel, ok1 := r.unresolve(oldTop)
		newRel, ok2 := r.unresolve(newTop)
		if !ok1 || !ok2 || oldRel == "" || newRel == "" {
			continue
		}
		if _, seen := m[oldRel]; seen {
			continue // a newer rename of this path already won
		}
		if final, ok := m[newRel]; ok {
			newRel = final // collapse old→mid→new
		}
		m[oldRel] = newRel
	}
	return m
}

// isRenameStatus reports whether a --name-status token is a rename or copy
// record ("R", "R100", "C75"), which are followed by two paths.
func isRenameStatus(s string) bool {
	if s == "" || (s[0] != 'R' && s[0] != 'C') {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// trimNUL drops trailing NUL bytes and CR/LF from git's -z output.
func trimNUL(b []byte) []byte {
	return []byte(strings.Trim(string(b), "\x00\r\n"))
}

// firstLine returns the first line of git output, trimmed.
func firstLine(b []byte) string {
	lines := splitLines(b)
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
