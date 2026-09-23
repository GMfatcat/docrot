package gitx

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A persistent cache of blame and log answers, kept between runs in the
// output directory. Blame results are keyed by the blob hash of the file
// at HEAD (the same content with the same history blames the same way, and
// a rewritten history changes the blobs' ancestry rarely enough to ignore);
// files with uncommitted changes are never cached. Log results are keyed
// by HEAD, so they survive until the next commit. Only the entries a run
// touched are written back, which keeps the file pruned.

const cacheVersion = 1

// blameRun is a run of consecutive lines last changed by the same commit.
type blameRun struct {
	H string `json:"h"`
	T int64  `json:"t"`
	N int    `json:"n"`
}

type cacheFile struct {
	Version int                        `json:"version"`
	Blame   map[string][]blameRun      `json:"blame"`
	Log     map[string]json.RawMessage `json:"log"`
}

type cache struct {
	path string
	mu   sync.Mutex
	data cacheFile
	used map[string]bool // "b:<blob>" and "l:<key>" entries touched this run
	hits int
}

func openCache(path string) *cache {
	c := &cache{path: path, used: map[string]bool{}}
	c.data = cacheFile{Version: cacheVersion, Blame: map[string][]blameRun{}, Log: map[string]json.RawMessage{}}
	if b, err := os.ReadFile(path); err == nil {
		var f cacheFile
		if json.Unmarshal(b, &f) == nil && f.Version == cacheVersion {
			if f.Blame != nil {
				c.data.Blame = f.Blame
			}
			if f.Log != nil {
				c.data.Log = f.Log
			}
		}
	}
	return c
}

func (c *cache) getBlame(blob string) ([]BlameLine, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	runs, ok := c.data.Blame[blob]
	if !ok {
		return nil, false
	}
	c.used["b:"+blob] = true
	c.hits++
	return expandRuns(runs), true
}

func (c *cache) putBlame(blob string, lines []BlameLine) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data.Blame[blob] = compressRuns(lines)
	c.used["b:"+blob] = true
}

func (c *cache) getLog(key string, v any) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, ok := c.data.Log[key]
	if !ok || json.Unmarshal(raw, v) != nil {
		return false
	}
	c.used["l:"+key] = true
	c.hits++
	return true
}

func (c *cache) putLog(key string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data.Log[key] = raw
	c.used["l:"+key] = true
}

// save writes the entries this run used, atomically.
func (c *cache) save() error {
	c.mu.Lock()
	out := cacheFile{Version: cacheVersion, Blame: map[string][]blameRun{}, Log: map[string]json.RawMessage{}}
	for k := range c.used {
		if blob, ok := strings.CutPrefix(k, "b:"); ok {
			if v, ok := c.data.Blame[blob]; ok {
				out.Blame[blob] = v
			}
		} else if key, ok := strings.CutPrefix(k, "l:"); ok {
			if v, ok := c.data.Log[key]; ok {
				out.Log[key] = v
			}
		}
	}
	c.mu.Unlock()
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp := c.path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func compressRuns(lines []BlameLine) []blameRun {
	var runs []blameRun
	for i, l := range lines {
		if i == 0 {
			continue // index 0 is unused
		}
		t := int64(0)
		if !l.Time.IsZero() {
			t = l.Time.Unix()
		}
		if n := len(runs); n > 0 && runs[n-1].H == l.Hash && runs[n-1].T == t {
			runs[n-1].N++
			continue
		}
		runs = append(runs, blameRun{H: l.Hash, T: t, N: 1})
	}
	return runs
}

func expandRuns(runs []blameRun) []BlameLine {
	lines := []BlameLine{{}}
	for _, r := range runs {
		var t time.Time
		if r.T != 0 {
			t = time.Unix(r.T, 0)
		}
		for k := 0; k < r.N; k++ {
			lines = append(lines, BlameLine{Hash: r.H, Time: t})
		}
	}
	return lines
}

// SaveCache writes the persistent cache when one is configured. It is safe
// to call when no cache is in use.
func (r *Repo) SaveCache() error {
	if r.cache == nil {
		return nil
	}
	return r.cache.save()
}

// CacheHits reports how many answers this run took from the persistent
// cache.
func (r *Repo) CacheHits() int {
	if r.cache == nil {
		return 0
	}
	r.cache.mu.Lock()
	defer r.cache.mu.Unlock()
	return r.cache.hits
}

// headBlobs maps every tracked path (relative to root) to its blob hash at
// HEAD, from a single `git ls-tree`. Computed once.
func (r *Repo) headBlobs() map[string]string {
	v, err := r.once("headblobs", func() (any, error) {
		out, err := r.run("ls-tree", "-r", "-z", "HEAD")
		if err != nil {
			return map[string]string{}, nil // no HEAD: nothing is cacheable
		}
		m := map[string]string{}
		for _, ent := range strings.Split(string(out), "\x00") {
			// "<mode> <type> <hash>\t<path>"
			tab := strings.IndexByte(ent, '\t')
			if tab < 0 {
				continue
			}
			f := strings.Fields(ent[:tab])
			if len(f) < 3 {
				continue
			}
			if rel, ok := r.unresolve(ent[tab+1:]); ok {
				m[rel] = f[2]
			}
		}
		return m, nil
	})
	if err != nil {
		return nil
	}
	m, _ := v.(map[string]string)
	return m
}

// dirtyPaths is the set of tracked paths (relative to root) that differ
// from HEAD in the index or the work tree. Computed once.
func (r *Repo) dirtyPaths() map[string]bool {
	v, _ := r.once("dirty", func() (any, error) {
		m := map[string]bool{}
		out, err := r.run("diff", "--name-only", "-z", "HEAD")
		if err != nil {
			return m, nil
		}
		for _, p := range strings.Split(string(out), "\x00") {
			if rel, ok := r.unresolve(p); ok && rel != "" {
				m[rel] = true
			}
		}
		return m, nil
	})
	m, _ := v.(map[string]bool)
	return m
}

// cacheableBlob returns the blob hash under which rel's blame may be
// cached: only when the file is tracked and unmodified.
func (r *Repo) cacheableBlob(rel string) (string, bool) {
	if r.cache == nil {
		return "", false
	}
	rel = strings.TrimPrefix(slash(rel), "./")
	if r.dirtyPaths()[rel] {
		return "", false
	}
	blob, ok := r.headBlobs()[rel]
	return blob, ok && blob != ""
}

// logKey builds the cache key of a log query: valid until HEAD moves.
func (r *Repo) logKey(parts ...string) (string, bool) {
	if r.cache == nil {
		return "", false
	}
	head, err := r.Head()
	if err != nil || head == "" {
		return "", false
	}
	return head + "|" + strings.Join(parts, "|"), true
}

// removeCache deletes a cache file; used by tests.
func removeCache(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		_ = err
	}
}
