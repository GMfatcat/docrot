package resolve

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// externalFlags belong to tools a document talks about around this
// program — docker, git, go test — and are never this program's claim
// unless the code defines them too (the index is asked first). Names a
// program commonly owns itself (--verbose, --force, --all, --json, --tags)
// are deliberately absent.
var externalFlags = map[string]bool{
	// docker run / build
	"read-only": true, "platform": true, "target": true, "cap-drop": true, "cap-add": true,
	"memory": true, "cpus": true, "detach": true, "network": true, "publish": true, "volume": true,
	"mount": true, "env-file": true, "restart": true, "no-cache": true, "pull": true, "push": true,
	"entrypoint": true, "workdir": true, "privileged": true, "security-opt": true, "tmpfs": true,
	"build-arg": true, "progress": true, "load": true, "cache-from": true, "cache-to": true,
	"health-cmd": true, "health-interval": true, "pids-limit": true, "memory-swap": true, "ulimit": true,
	"no-new-privileges": true, "init": true, "gpus": true, "shm-size": true, "add-host": true,
	// go test / go build
	"race": true, "count": true, "bench": true, "benchmem": true, "cover": true, "coverprofile": true,
	"covermode": true, "coverpkg": true, "mod": true, "trimpath": true, "ldflags": true, "gcflags": true,
	"asmflags": true, "short": true, "failfast": true, "shuffle": true, "fuzz": true, "fuzztime": true,
	"buildvcs": true, "vet": true, "cpuprofile": true, "memprofile": true, "blockprofile": true,
	"mutexprofile": true, "benchtime": true, "parallel": true, "run": true, "skip": true, "exec": true,
	"modfile": true, "overlay": true, "pgo": true, "toolexec": true, "installsuffix": true, "msan": true, "asan": true,
	// git
	"oneline": true, "amend": true, "no-verify": true, "force-with-lease": true, "rebase": true,
	"staged": true, "cached": true, "hard": true, "soft": true, "mixed": true, "prune": true,
	"ff-only": true, "no-ff": true, "squash": true, "name-only": true, "name-status": true,
	"set-upstream": true, "no-edit": true, "allow-empty": true, "autostash": true, "onto": true,
	"stat": true, "follow": true, "decorate": true, "graph": true, "ignore-all-space": true,
	"ignore-blank-lines": true, "word-diff": true, "no-pager": true, "porcelain": true, "bare": true,
	"depth": true, "single-branch": true, "recurse-submodules": true, "mirror": true, "orphan": true,
}

// reGoDecl matches the exported declarations a sibling package may hold.
var reGoDecl = regexp.MustCompile(`(?m)^(?:func(?:\s*\([^)]*\))?\s+|type\s+|var\s+|const\s+|\t)([A-Z][A-Za-z0-9_]*)\b`)

// siblingPackages indexes, lazily and once, the exported names of the Go
// packages found under the configured sibling roots, keyed by package
// directory name. A document that says `httpx.Response` next to this
// module's own httpx may mean the sibling's package of the same name.
type siblingPackages struct {
	once  sync.Once
	names map[string]map[string]bool // dir name → exported identifiers
	roots map[string][]string        // dir name → sibling roots that hold it
}

// skipDirs are never walked for sibling packages.
var skipDirs = map[string]bool{"vendor": true, "node_modules": true, "testdata": true, ".git": true, "dist": true, "third_party": true}

func (s *siblingPackages) load(roots []string) {
	s.names = map[string]map[string]bool{}
	s.roots = map[string][]string{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				base := d.Name()
				if p != root && (skipDirs[base] || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_")) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.Size() > 2<<20 {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			pkg := filepath.Base(filepath.Dir(p))
			set := s.names[pkg]
			if set == nil {
				set = map[string]bool{}
				s.names[pkg] = set
				s.roots[pkg] = append(s.roots[pkg], root)
			} else if rs := s.roots[pkg]; rs[len(rs)-1] != root {
				s.roots[pkg] = append(rs, root)
			}
			for _, m := range reGoDecl.FindAllStringSubmatch(string(data), -1) {
				set[m[1]] = true
			}
			return nil
		})
	}
}

// siblingHas returns the sibling root whose package pkg exports name, or
// "" when none does.
func (r *Resolver) siblingHas(pkg, name string) string {
	if len(r.opts.Siblings) == 0 {
		return ""
	}
	r.sibs.once.Do(func() { r.sibs.load(r.opts.Siblings) })
	if r.sibs.names[pkg][name] {
		if roots := r.sibs.roots[pkg]; len(roots) > 0 {
			return roots[0]
		}
	}
	return ""
}
