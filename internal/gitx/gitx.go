// Package gitx is a small, cached, concurrency-limited wrapper around the
// git command line.
//
// docrot treats git as an optional accelerator: the stale (§11) and pair-lag
// (§12) analyses need commit times, but everything else must keep working
// when git is missing or the directory is not a work tree. Therefore Open
// reports [ErrUnavailable] instead of a hard failure, and callers are
// expected to silently disable git-backed rules when they see it.
//
// A Repo is safe for concurrent use. Every query is memoised per Repo and
// de-duplicated, so two goroutines asking about the same file spawn at most
// one git process, and at most Options.Concurrency git processes run at once.
//
// All paths accepted by Repo methods are relative to the root passed to
// [Open], use forward slashes, and are translated internally to the work
// tree top level.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrUnavailable reports that git cannot be used: the binary is not on PATH,
// the directory is not inside a work tree, or git was disabled. Errors
// returned by [Open] wrap it, so test with errors.Is.
var ErrUnavailable = errors.New("gitx: git unavailable")

// Default option values used when a field of [Options] is zero.
const (
	DefaultTimeout     = 10 * time.Second
	DefaultConcurrency = 4
)

// Options tunes how git subprocesses are run.
type Options struct {
	// Timeout bounds a single git command. Zero means DefaultTimeout.
	Timeout time.Duration
	// Concurrency is the maximum number of git processes running at once.
	// Zero means DefaultConcurrency. Negative is treated as 1.
	Concurrency int
}

func (o Options) normalized() Options {
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.Concurrency == 0 {
		o.Concurrency = DefaultConcurrency
	}
	if o.Concurrency < 1 {
		o.Concurrency = 1
	}
	return o
}

// Stats counts the work a Repo did, for the summary line of a report.
type Stats struct {
	// Commands is the number of git processes started.
	Commands int
	// CacheHits is the number of queries answered from the cache (including
	// queries that waited for an identical in-flight query).
	CacheHits int
	// Duration is the wall-clock time spent inside git processes. Because
	// commands run concurrently it may exceed the elapsed time.
	Duration time.Duration
}

var (
	availableOnce sync.Once
	availableVal  bool
)

// Available reports whether a git executable is on PATH. The lookup is done
// once per process and cached.
func Available() bool {
	availableOnce.Do(func() {
		_, err := exec.LookPath("git")
		availableVal = err == nil
	})
	return availableVal
}

// call is one memoised query: the first caller computes it, later callers
// wait on done and then read val/err.
type call struct {
	done chan struct{}
	val  any
	err  error
}

// Repo is a handle on a git work tree. The zero value is not usable; obtain
// one from [Open].
type Repo struct {
	opts     Options
	topLevel string // work tree top level, forward slashes, no trailing slash
	prefix   string // root relative to topLevel, forward slashes, "" or "sub/"

	sem chan struct{} // counting semaphore limiting git processes

	mu    sync.Mutex
	calls map[string]*call

	statsMu sync.Mutex
	stats   Stats
}

// Open prepares a Repo for the work tree containing root.
//
// It returns an error wrapping [ErrUnavailable] when git is not on PATH or
// root is not inside a work tree. The work tree top level is recorded, and
// paths passed to later calls are interpreted relative to root.
func Open(root string, opts Options) (*Repo, error) {
	if !Available() {
		return nil, fmt.Errorf("%w: git not found in PATH", ErrUnavailable)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrUnavailable, root)
	}
	r := &Repo{
		opts:     opts.normalized(),
		topLevel: abs,
		calls:    make(map[string]*call),
	}
	r.sem = make(chan struct{}, r.opts.Concurrency)

	out, err := r.run("rev-parse", "--show-toplevel", "--show-prefix")
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not inside a git work tree: %v", ErrUnavailable, root, err)
	}
	lines := splitLines(out)
	if len(lines) == 0 || lines[0] == "" {
		return nil, fmt.Errorf("%w: %s is not inside a git work tree", ErrUnavailable, root)
	}
	r.topLevel = strings.TrimSuffix(slash(lines[0]), "/")
	if len(lines) > 1 {
		p := slash(lines[1])
		if p != "" && !strings.HasSuffix(p, "/") {
			p += "/"
		}
		r.prefix = p
	}
	return r, nil
}

// TopLevel returns the absolute path of the work tree top level, with
// forward slashes.
func (r *Repo) TopLevel() string { return r.topLevel }

// Prefix returns the position of the root passed to [Open] relative to the
// work tree top level: "" when root is the top level, otherwise a
// slash-terminated path such as "sub/".
func (r *Repo) Prefix() string { return r.prefix }

// Stats returns a snapshot of the counters.
func (r *Repo) Stats() Stats {
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	return r.stats
}

// resolve turns a path relative to root into one relative to the work tree
// top level. It tolerates backslashes and "./" prefixes. An empty or "."
// input means "everything under root".
func (r *Repo) resolve(rel string) string {
	s := slash(rel)
	s = strings.TrimPrefix(s, "./")
	if s == "" {
		s = "."
	}
	s = path.Clean(s)
	if s == "." {
		if r.prefix == "" {
			return "."
		}
		return strings.TrimSuffix(r.prefix, "/")
	}
	return r.prefix + s
}

// unresolve turns a path relative to the work tree top level into one
// relative to root. ok is false when the path lies outside root.
func (r *Repo) unresolve(top string) (string, bool) {
	s := slash(top)
	if r.prefix == "" {
		return s, true
	}
	if !strings.HasPrefix(s, r.prefix) {
		return "", false
	}
	return strings.TrimPrefix(s, r.prefix), true
}

// once memoises fn under key, de-duplicating concurrent callers so that only
// the first one runs git.
func (r *Repo) once(key string, fn func() (any, error)) (any, error) {
	r.mu.Lock()
	if c, ok := r.calls[key]; ok {
		r.mu.Unlock()
		<-c.done
		r.statsMu.Lock()
		r.stats.CacheHits++
		r.statsMu.Unlock()
		return c.val, c.err
	}
	c := &call{done: make(chan struct{})}
	r.calls[key] = c
	r.mu.Unlock()

	c.val, c.err = fn()
	close(c.done)
	return c.val, c.err
}

// gitEnv returns the environment for every git child process: no optional
// lock files (cheaper and read-only friendly), byte-stable C locale, and no
// credential prompts.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
		"GIT_TERMINAL_PROMPT=0",
	)
}

// run executes git with args in the work tree top level, bounded by the
// configured timeout and by the concurrency semaphore. On timeout the
// returned error wraps context.DeadlineExceeded.
func (r *Repo) run(args ...string) ([]byte, error) {
	r.sem <- struct{}{}
	defer func() { <-r.sem }()

	ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.topLevel
	cmd.Env = gitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	r.statsMu.Lock()
	r.stats.Commands++
	r.stats.Duration += elapsed
	r.statsMu.Unlock()

	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("gitx: git %s timed out after %s: %w",
			strings.Join(args, " "), r.opts.Timeout, context.DeadlineExceeded)
	}
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("gitx: git %s: %v%s",
			strings.Join(args, " "), err, stderrSuffix(stderr.Bytes()))
	}
	return stdout.Bytes(), nil
}

// stderrSuffix formats the first line of git's stderr for an error message.
func stderrSuffix(b []byte) string {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return ": " + s
}

// slash normalises backslashes to forward slashes.
func slash(s string) string { return strings.ReplaceAll(s, "\\", "/") }

// splitLines splits git output into lines, tolerating CRLF and dropping the
// trailing empty element.
func splitLines(b []byte) []string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
