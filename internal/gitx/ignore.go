package gitx

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// Ignored reports, for each relative path, whether .gitignore rules would
// ignore it. Paths need not exist: a missing "dist/app.exe" that matches an
// ignore rule is a build artifact, not a documentation lie. The result is
// keyed by the paths as given. A git failure yields an empty map and the
// error; exit status 1 (nothing ignored) is not an error.
func (r *Repo) Ignored(rels []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(rels) == 0 {
		return out, nil
	}
	var in bytes.Buffer
	back := map[string]string{} // top-level path → as given
	for _, rel := range rels {
		top := r.resolve(rel)
		back[top] = rel
		in.WriteString(top)
		in.WriteByte(0)
	}

	r.sem <- struct{}{}
	defer func() { <-r.sem }()
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "check-ignore", "-z", "--stdin")
	cmd.Dir = r.topLevel
	cmd.Env = gitEnv()
	cmd.Stdin = &in
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	r.statsMu.Lock()
	r.stats.Commands++
	r.stats.Duration += time.Since(start)
	r.statsMu.Unlock()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return out, nil // nothing ignored
		}
		return out, err
	}
	for _, p := range strings.Split(string(stdout.Bytes()), "\x00") {
		if p == "" {
			continue
		}
		if rel, ok := back[slash(p)]; ok {
			out[rel] = true
		}
	}
	return out, nil
}
