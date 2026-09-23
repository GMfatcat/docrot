// Package resolve checks every extracted reference against the repository
// index and turns misses into findings with "did you mean" suggestions.
//
// Severity policy (see docs/rules.md): paths, anchors, commands, imports
// and symbols follow the extractor's confidence (high → error, medium →
// warning, low → info). Flags and environment variables are one step
// softer (high → warning, medium → info, low → dropped) because they are
// far more often about *other* programs. Config keys are always info.
package resolve

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"docrot/internal/extract"
	"docrot/internal/fuzzy"
	"docrot/internal/model"
)

// Options tunes the resolver.
type Options struct {
	Root          string                    // repo root on disk (for stat fallback of excluded paths)
	MinConfidence model.Confidence          // references below this are ignored (default Low)
	Net           bool                      // check URLs over the network
	NetTimeout    time.Duration             // per URL, default 5s
	Severity      map[string]model.Severity // per-rule override
	Renames       map[string]string         // old → new from git history
	Siblings      []string                  // absolute sibling repo roots where missing paths may live
}

// Result is the outcome of resolving one reference.
type Result struct {
	OK      bool           // the reference resolved
	Skipped bool           // the reference was not checked (below confidence, no index, external)
	File    string         // resolved file or directory (relative) for stale analysis; "" if none
	Finding *model.Finding // non-nil when OK is false and not skipped
}

// Resolver resolves references against an index. Safe for concurrent use.
type Resolver struct {
	ix   model.Index
	opts Options

	httpOnce   sync.Once
	httpClient *http.Client
	urlCache   sync.Map // url → error string ("" = ok)
}

// New creates a Resolver.
func New(ix model.Index, opts Options) *Resolver {
	if opts.MinConfidence == 0 {
		opts.MinConfidence = model.Low
	}
	if opts.NetTimeout == 0 {
		opts.NetTimeout = 5 * time.Second
	}
	return &Resolver{ix: ix, opts: opts}
}

// Resolve checks one reference.
func (r *Resolver) Resolve(ref model.Reference) Result {
	if ref.Confidence < r.opts.MinConfidence {
		return Result{Skipped: true}
	}
	if lg, ok := model.LangOf(ref.Kind); ok {
		return r.resolveLangSymbol(ref, lg)
	}
	switch ref.Kind {
	case model.KindPath, model.KindCommand:
		return r.resolvePath(ref)
	case model.KindGoSymbol:
		return r.resolveGoSymbol(ref)
	case model.KindFlag:
		return r.resolveFlag(ref)
	case model.KindEnv:
		return r.resolveEnv(ref)
	case model.KindConfigKey:
		return r.resolveConfigKey(ref)
	case model.KindAnchor:
		return r.resolveAnchor(ref)
	case model.KindURL:
		return r.resolveURL(ref)
	case model.KindImport:
		return r.resolveImport(ref)
	case model.KindRoute:
		return r.resolveRoute(ref)
	case model.KindTarget:
		return r.resolveTarget(ref)
	case model.KindInstall:
		return r.resolveInstall(ref)
	case model.KindToolchain:
		return r.resolveToolchain(ref)
	case model.KindDefault:
		return r.resolveDefault(ref)
	}
	return Result{Skipped: true}
}

func (r *Resolver) severity(rule string, def model.Severity) model.Severity {
	if s, ok := r.opts.Severity[rule]; ok && s != "" {
		return s
	}
	return def
}

func (r *Resolver) finding(rule string, def model.Severity, ref model.Reference, msg string, cands []string) *model.Finding {
	f := model.NewFinding(rule, r.severity(rule, def), ref, msg)
	if len(cands) > 0 {
		f.Suggestion = cands[0]
		if len(cands) > 1 {
			f.Data = map[string]any{"candidates": cands}
		}
	}
	return &f
}

// --- paths -----------------------------------------------------------------

func (r *Resolver) candidates(ref model.Reference) []string {
	norm := ref.Norm
	docDir := path.Dir(ref.Loc.File)
	var cands []string
	if ref.Rooted {
		// "/topics/x" from docs/howto/y.txt: the source root is docs/, or
		// some other ancestor of the document
		for d := docDir; ; d = path.Dir(d) {
			if d == "." || d == "" {
				cands = append(cands, path.Clean(norm))
				break
			}
			cands = append(cands, path.Clean(d+"/"+norm))
		}
		return cands
	}
	if docDir != "." && docDir != "" {
		cands = append(cands, path.Clean(docDir+"/"+norm))
	}
	if !strings.HasPrefix(norm, "../") {
		cands = append(cands, path.Clean(norm))
	} else if docDir != "." && docDir != "" {
		// "../../docs_src/x.py" written from a docs tree whose base is not
		// the document's own directory (MkDocs docs_dir, Sphinx source):
		// climb the ancestors and accept the first that resolves.
		for d := path.Dir(docDir); ; d = path.Dir(d) {
			c := path.Clean(d + "/" + norm)
			if !strings.HasPrefix(c, "../") {
				cands = append(cands, c)
			}
			if d == "." || d == "/" {
				break
			}
		}
	}
	return cands
}

// existsOnDisk is the fallback for paths hidden from the index by exclude
// rules (vendor/, dist/, …).
func (r *Resolver) existsOnDisk(rel string) bool {
	if r.opts.Root == "" || strings.HasPrefix(rel, "..") {
		return false
	}
	if ExistsExact(r.opts.Root, rel) {
		return true
	}
	for _, sib := range r.opts.Siblings {
		if ExistsExact(sib, rel) {
			return true
		}
		// "meowbase/httpx/README.md" written from the parent directory's view
		if first, rest, ok := strings.Cut(rel, "/"); ok && first == filepath.Base(sib) {
			if ExistsExact(sib, rest) {
				return true
			}
		}
	}
	return false
}

// ExistsExact reports whether rel exists under root with exactly this
// spelling. os.Stat is case-insensitive on Windows and macOS, so a document
// saying "Readme.md" would pass there and fail on Linux; docrot compares
// every path component against the directory listing instead, so the
// verdict is the same on every platform.
func ExistsExact(root, rel string) bool {
	rel = strings.Trim(rel, "/")
	if rel == "" || rel == "." {
		return true
	}
	dir := root
	for _, comp := range strings.Split(rel, "/") {
		if comp == "" || comp == "." {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		found := false
		for _, e := range entries {
			if e.Name() == comp {
				found = true
				break
			}
		}
		if !found {
			return false
		}
		dir = filepath.Join(dir, comp)
	}
	return true
}

func (r *Resolver) resolvePath(ref model.Reference) Result {
	isGlob := strings.ContainsAny(ref.Norm, "*?")
	for _, c := range r.candidates(ref) {
		if isGlob {
			if m := r.ix.Glob(c); len(m) > 0 {
				return Result{OK: true, File: path.Dir(c)}
			}
			if !strings.Contains(c, "/") {
				if m := r.ix.Glob("**/" + c); len(m) > 0 {
					return Result{OK: true}
				}
			}
			continue
		}
		if r.ix.FileExists(c) || r.ix.DirExists(c) || r.existsOnDisk(c) {
			return Result{OK: true, File: c}
		}
	}
	if extract.IsPlaceholderPath(ref.Norm) {
		return Result{Skipped: true} // docs/foo.md, ./cmd/x: illustrative
	}
	if genericManifests[ref.Norm] {
		return Result{Skipped: true} // "package.json", "Makefile": names of things, not claims about this repo
	}
	rule := model.RuleMissingPath
	if ref.Kind == model.KindCommand {
		rule = model.RuleMissingCommand
	}
	sev := model.SeverityFor(ref.Confidence)
	var msg string
	var cands []string
	if isGlob {
		msg = "no file matches `" + ref.Text + "`"
		sev = model.SevInfo
	} else if segs := strings.Split(ref.Norm, "/"); len(segs) >= 2 && r.allTopLevel(segs) {
		// "errx/logx/timex" is a list of packages, not a path
		return Result{Skipped: true}
	} else {
		msg = "`" + ref.Text + "` not found"
		if ref.Kind == model.KindCommand {
			msg = "command path `" + ref.Text + "` not found"
		}
		if to, ok := r.opts.Renames[path.Clean(ref.Norm)]; ok {
			cands = append(cands, to)
			msg += " (renamed in git history)"
		}
		for _, c := range r.candidates(ref) {
			for _, s := range r.ix.SimilarPaths(c, 3) {
				cands = appendUnique(cands, s)
			}
		}
		if len(cands) > 3 {
			cands = cands[:3]
		}
		// Only the letter case differs: exists on Windows/macOS, breaks on Linux.
		for _, c := range r.candidates(ref) {
			if len(cands) > 0 && strings.EqualFold(cands[0], c) && cands[0] != c {
				msg = "`" + ref.Text + "` differs from `" + cands[0] + "` only by letter case (works on case-insensitive file systems, breaks on Linux)"
				break
			}
		}
		// The same relative path exists under a sub-tree (a template
		// directory, an example, a package): the doc is describing that tree.
		if sev == model.SevError && strings.Contains(ref.Norm, "/") {
			under := ""
			if len(cands) > 0 && strings.HasSuffix(cands[0], "/"+path.Clean(ref.Norm)) {
				under = cands[0]
			} else if m := r.ix.Glob("**/" + path.Clean(ref.Norm)); len(m) > 0 {
				under = m[0]
				cands = append([]string{under}, cands...)
				if len(cands) > 3 {
					cands = cands[:3]
				}
			}
			if under != "" {
				sev = model.SevWarning
				msg = "`" + ref.Text + "` not found at the repo root (exists under " + strings.TrimSuffix(under, "/"+path.Clean(ref.Norm)) + "/)"
			}
		}
		// A bare file name ("main.go", "config.json") is a weak claim: it
		// usually means "the main.go of whatever we are talking about".
		if !strings.Contains(ref.Norm, "/") && sev.Rank() > model.SevInfo.Rank() {
			sev = model.SevInfo
		}
	}
	return Result{Finding: r.finding(rule, sev, ref, msg, cands)}
}

// genericManifests are bare file names that documents use as common nouns
// ("keep it next to package.json"); their absence is not documentation rot.
var genericManifests = map[string]bool{
	"package.json": true, "package-lock.json": true, "pyproject.toml": true, "setup.py": true,
	"setup.cfg": true, "requirements.txt": true, "go.mod": true, "go.sum": true, "go.work": true,
	"Makefile": true, "GNUmakefile": true, "justfile": true, "Taskfile.yml": true, "Cargo.toml": true,
	"Dockerfile": true, "docker-compose.yml": true, "compose.yml": true, ".gitignore": true,
	".dockerignore": true, ".editorconfig": true, "tsconfig.json": true, "Gemfile": true,
	"pom.xml": true, "build.gradle": true, "CMakeLists.txt": true, "poetry.lock": true, "uv.lock": true,
}

// cleanRel strips a leading "/" and "./" from a link target.
func cleanRel(p string) string {
	p = strings.TrimPrefix(strings.TrimSpace(p), "/")
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return p
}

func isCapitalized(s string) bool { return s != "" && s[0] >= 'A' && s[0] <= 'Z' }

// allTopLevel reports whether every segment names a top-level directory.
func (r *Resolver) allTopLevel(segs []string) bool {
	for _, s := range segs {
		if !r.ix.DirExists(s) {
			return false
		}
	}
	return true
}

// maxDist is the edit-distance budget for did-you-mean suggestions: one
// edit for short names, two for longer ones.
func maxDist(s string) int {
	if len(s) < 8 {
		return 1
	}
	return 2
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// --- Go symbols ------------------------------------------------------------

func (r *Resolver) resolveGoSymbol(ref model.Reference) Result {
	if r.ix.ModulePath() == "" && len(r.ix.GoPackages()) == 0 {
		return Result{Skipped: true}
	}
	q := ref.Norm
	if r.ix.HasGoSymbol(q) {
		f, _ := r.ix.GoSymbolFile(q)
		return Result{OK: true, File: f}
	}
	parts := strings.Split(q, ".")
	first, last := parts[0], parts[len(parts)-1]
	if r.ix.HasLiteral(q) {
		return Result{OK: true} // "http.requests": a metric, logger or RPC name spelled as a string
	}
	sev := model.SeverityFor(ref.Confidence)
	var msg string
	switch {
	case len(parts) == 1:
		msg = "function or type `" + ref.Text + "` not found in any package"
		if ref.Confidence < model.High {
			sev = model.SevInfo // a bare call is usually a method or a local helper
		}
	case r.ix.IsGoPackage(first):
		if r.ix.HasJSONKey(q) || r.ix.HasConfigKey(q) {
			return Result{OK: true} // stale.minChurn: a config key that starts with a package name
		}
		if strings.ToLower(last) == last && strings.Contains(last, "_") {
			return r.resolveConfigKey(model.Reference{Kind: model.KindConfigKey, Text: ref.Text, Norm: q, Confidence: model.Low, Loc: ref.Loc, Section: ref.Section, Context: ref.Context})
		}
		msg = "`" + q + "` not found in package " + first
		if strings.ToLower(last) == last {
			// db.synchronous, htmx.trigger: an unexported name or not Go at all
			sev = model.SevInfo
		}
		if len(parts) == 2 && r.ix.HasGoMember(last) && isReceiverish(first) {
			// `cfg.Addr` where cfg is both a package and a common variable name
			msg += " (if `" + first + "` is a variable, its type may have `" + last + "`)"
			sev = model.SevInfo
		}
	case r.ix.IsGoType(first):
		if strings.ToLower(last) == last && r.ix.HasJSONKey(last) {
			return Result{OK: true} // Envelope.request_id: a wire field, not a Go member
		}
		msg = "`" + q + "` not found on type " + first
	default:
		if ref.Confidence <= model.Low && (first == "" || first[0] < 'A' || first[0] > 'Z') {
			// receiver variable (app.Run, s.Addr) or another module's package: not our business.
			return Result{Skipped: true}
		}
		msg = "package or type `" + first + "` not found in this module (`" + q + "`)"
		sev = model.SevInfo
	}
	cands := r.ix.SimilarGoSymbols(q, 3)
	return Result{Finding: r.finding(model.RuleMissingSymbol, sev, ref, msg, cands)}
}

// isReceiverish reports whether name looks like a short variable that
// commonly collides with a package name (cfg, log, config, app, ...).
func isReceiverish(name string) bool {
	if len(name) <= 4 {
		return true
	}
	switch name {
	case "config", "logger", "client", "server", "store", "router", "handler", "worker", "service", "runner", "engine", "index", "opts", "options", "flags", "state", "cache", "queue", "pool", "conn", "resp", "req",
		"request", "response", "session", "model", "instance", "self", "cls", "user", "item", "items", "result", "results", "data", "settings", "context", "message", "event", "task", "job", "record", "row", "field", "value", "values", "params", "args", "kwargs", "template", "templates", "schema", "form", "file", "files":
		return true
	}
	return false
}

// --- other languages ----------------------------------------------------

// resolveLangSymbol checks a symbol of one of model.Langs against that
// language's index first and then against every other present language
// (a mixed repository documents both), so a classification that guessed
// the language wrong never produces a finding when the name exists.
func (r *Resolver) resolveLangSymbol(ref model.Reference, lg model.Lang) Result {
	if !r.ix.HasLang(ref.Kind) {
		return Result{Skipped: true}
	}
	anyHas := func(q string) bool {
		for _, k := range r.ix.Languages() {
			if r.ix.HasSymbol(k, q) {
				return true
			}
		}
		return false
	}
	if anyHas(ref.Norm) {
		return Result{OK: true}
	}
	// Type.field where Type is a known declaration: members are not indexed
	first, _, dotted := strings.Cut(ref.Norm, lg.Sep)
	if dotted && isCapitalized(first) && anyHas(first) {
		return Result{OK: true}
	}
	// owner.attr where the owner is a known class, function or module-level
	// object (not a namespace): an attribute the declaration index cannot
	// see (set in __init__, a proxy, a descriptor). A missing name *in a
	// namespace* stays a finding.
	if i := strings.LastIndex(ref.Norm, lg.Sep); i > 0 {
		owner := strings.TrimSuffix(ref.Norm[:i], "()")
		if anyHas(owner) && !r.ix.IsNamespace(ref.Kind, owner) {
			return Result{OK: true}
		}
	}
	if r.ix.HasLiteral(strings.TrimSuffix(ref.Norm, "()")) {
		return Result{OK: true} // a name the code spells as a string (an event, a command, a key)
	}
	sev := model.SeverityFor(ref.Confidence)
	switch {
	case !dotted && !lg.Flat:
		sev = model.SevInfo // a bare call is usually a method or a local helper
	case dotted && lg.Stdlib[first]:
		return Result{Skipped: true} // typing.Annotated, std::io::Read
	case dotted && lg.Methods != 0 && isReceiverish(first):
		return Result{Skipped: true} // app.routes, client.get: an instance
	case dotted && r.ix.IsExample(ref.Kind, first):
		sev = model.SevInfo // app.main from docs_src: a tutorial layout
	}
	if ref.Confidence <= model.Low {
		// a lower-case dotted name may be a config key instead
		if r.ix.HasConfigKey(ref.Norm) || r.ix.HasJSONKey(ref.Norm) {
			return Result{OK: true}
		}
		if looksLikeDomain(ref.Norm) {
			return Result{Skipped: true}
		}
		if len(r.ix.JSONKeys()) > 0 || len(r.ix.ConfigKeys()) > 0 {
			// ambiguous lower-case dotted name: report it the config-key way
			return r.resolveConfigKey(ref)
		}
	}
	msg := lg.Name + " symbol `" + ref.Text + "` not found"
	cands := r.ix.SimilarSymbols(ref.Kind, ref.Norm, 3)
	if dotted {
		// "pydantic.utils.to_camel": the module exists, the name lives in
		// another module of the same package (moved, or reachable through a
		// compatibility shim) — worth a look, not a build break
		if i := strings.LastIndex(ref.Norm, lg.Sep); i > 0 && r.ix.IsNamespace(ref.Kind, ref.Norm[:i]) {
			bare := strings.TrimSuffix(ref.Norm[i+len(lg.Sep):], "()")
			for _, c := range cands {
				if strings.HasSuffix(c, lg.Sep+bare) && sev == model.SevError {
					sev = model.SevWarning
					msg = lg.Name + " symbol `" + ref.Text + "` not found in module `" + ref.Norm[:i] + "` (a `" + bare + "` exists at " + c + ")"
					cands = append([]string{c}, cands...)
					break
				}
			}
		}
	}
	return Result{Finding: r.finding(model.RuleMissingSymbol, sev, ref, msg, cands)}
}

// --- flags -----------------------------------------------------------------

func (r *Resolver) resolveFlag(ref model.Reference) Result {
	if ref.Confidence <= model.Low {
		return Result{Skipped: true}
	}
	flags := r.ix.Flags()
	if len(flags) == 0 {
		return Result{Skipped: true}
	}
	if r.ix.HasFlag(ref.Norm) {
		return Result{OK: true}
	}
	if r.ix.HasLiteral(ref.Norm) || r.ix.HasLiteral("--"+ref.Norm) || r.ix.HasLiteral("-"+ref.Norm) || r.ix.HasLiteral(strings.ReplaceAll(ref.Norm, "-", "_")) {
		return Result{OK: true} // defined by a flag library the index does not parse (pflag, cobra, argparse…)
	}
	sev := model.SevWarning
	if ref.Confidence == model.Medium {
		sev = model.SevInfo
	}
	pool := make([]string, 0, len(flags))
	for _, f := range flags {
		pool = append(pool, strings.ToLower(strings.ReplaceAll(f, "_", "-")))
	}
	var cands []string
	for _, c := range fuzzy.Rank(ref.Norm, pool, 3, maxDist(ref.Norm)) {
		cands = append(cands, "--"+c.Text)
	}
	msg := "flag `" + ref.Text + "` is not defined by any flag.* call"
	return Result{Finding: r.finding(model.RuleUnknownFlag, sev, ref, msg, cands)}
}

// --- env -------------------------------------------------------------------

var externalEnvPrefixes = []string{
	"GO", "GIT_", "GITHUB_", "GITLAB_", "CI_", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
	"CUDA_", "NVIDIA_", "DOCKER_", "COMPOSE_", "NODE_", "NPM_", "PYTHON", "PIP_", "LC_",
	"LANG", "TZ", "TMP", "TEMP", "HOME", "PATH", "USER", "SHELL", "TERM", "NO_COLOR",
	"CLICOLOR", "XDG_", "PROGRAM", "SYSTEM", "APPDATA", "LOCALAPPDATA", "COMSPEC", "PWD",
	"OS", "WINDIR", "CGO_", "ANTHROPIC_", "OPENAI_", "AWS_", "AZURE_", "GOOGLE_", "K8S_",
	"KUBE", "HELM_", "TF_", "VAULT_", "SSH_", "GPG_", "EDITOR", "VISUAL", "PAGER", "MSYS",
	"MINGW", "CI", "BUILD_", "RUNNER_", "JOB_", "ACTIONS_", "ODIN_", "RUST", "CARGO_",
}

// envContext reports whether the line around a reference talks about
// environment variables at all; UPPER_SNAKE names are also error codes,
// constants and date layouts.
func envContext(line string) bool {
	l := strings.ToLower(line)
	for _, kw := range []string{"env", "export ", "environment", "環境", "变量", "變數", "$", "set ", "setx "} {
		if strings.Contains(l, kw) {
			return true
		}
	}
	return false
}

// IsExternalEnv reports whether name is a well-known environment variable
// owned by the OS, the toolchain or another program (GOPATH, GIT_*, HOME…).
func IsExternalEnv(name string) bool { return isExternalEnv(name) }

func isExternalEnv(name string) bool {
	for _, p := range externalEnvPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func (r *Resolver) resolveEnv(ref model.Reference) Result {
	if ref.Confidence <= model.Low {
		return Result{Skipped: true}
	}
	if r.ix.HasEnv(ref.Norm) || r.ix.HasLiteral(ref.Norm) {
		return Result{OK: true} // read through a wrapper the index does not recognise
	}
	if len(r.ix.Envs()) == 0 || isExternalEnv(ref.Norm) || !envContext(ref.Context) {
		return Result{Skipped: true}
	}
	sev := model.SevWarning
	if ref.Confidence == model.Medium {
		sev = model.SevInfo
	}
	var cands []string
	for _, c := range fuzzy.Rank(ref.Norm, r.ix.Envs(), 3, maxDist(ref.Norm)) {
		cands = append(cands, c.Text)
	}
	msg := "environment variable `" + ref.Text + "` is never read by the code"
	return Result{Finding: r.finding(model.RuleUnknownEnv, sev, ref, msg, cands)}
}

// --- config keys -----------------------------------------------------------

var tlds = map[string]bool{
	"com": true, "net": true, "org": true, "io": true, "local": true, "dev": true, "app": true,
	"ai": true, "tw": true, "cn": true, "jp": true, "co": true, "uk": true, "edu": true,
	"gov": true, "info": true, "me": true, "sh": true, "gg": true, "xyz": true, "cloud": true,
	"internal": true, "lan": true, "home": true, "corp": true, "test": true, "example": true,
	"localhost": true, "de": true, "fr": true, "us": true, "eu": true, "kr": true, "in": true,
	"html": true, "md": true, "json": true, "go": true, "py": true, "js": true, "ts": true,
	"txt": true, "yaml": true, "yml": true, "toml": true, "exe": true, "dll": true, "so": true,
}

func looksLikeDomain(dotted string) bool {
	parts := strings.Split(dotted, ".")
	last := parts[len(parts)-1]
	if tlds[strings.ToLower(last)] {
		return true
	}
	single := true
	for _, p := range parts {
		if len(p) > 1 {
			single = false
		}
	}
	return single
}

func (r *Resolver) resolveConfigKey(ref model.Reference) Result {
	if looksLikeDomain(ref.Norm) {
		return Result{Skipped: true}
	}
	if len(r.ix.JSONKeys()) == 0 && len(r.ix.ConfigKeys()) == 0 {
		return Result{Skipped: true}
	}
	if r.ix.HasJSONKey(ref.Norm) || r.ix.HasConfigKey(ref.Norm) || r.ix.HasLiteral(ref.Norm) {
		return Result{OK: true} // viper.Get("server.port")-style lookups spell the whole key
	}
	fromJSON := ref.Lang == "json"
	if !fromJSON {
		// only report when the top-level segment is a real config section;
		// otherwise "rec.status" is just a variable in prose
		top, _, _ := strings.Cut(ref.Norm, ".")
		if !r.ix.HasJSONKey(top) && !r.ix.HasConfigKey(top) {
			return Result{Skipped: true}
		}
	}
	pool := append(append([]string{}, r.ix.JSONKeys()...), r.ix.ConfigKeys()...)
	var cands []string
	if fromJSON {
		cands = siblingKeys(ref.Norm, pool)
	} else {
		for _, c := range fuzzy.Rank(ref.Norm, pool, 3, maxDist(ref.Norm)) {
			cands = append(cands, c.Text)
		}
	}
	msg := "config key `" + ref.Text + "` not found in any config struct tag or sample file"
	sev := model.SevInfo
	if fromJSON {
		// a JSON example whose other keys are real config keys: this one is
		// a dropped or misspelled key, not a passing mention
		msg = "key `" + ref.Text + "` in the JSON example is not in any config struct tag or sample file"
		if ref.Confidence == model.High {
			sev = model.SevWarning
		}
	}
	return Result{Finding: r.finding(model.RuleUnknownConfigKey, sev, ref, msg, cands)}
}

// siblingKeys suggests keys that live under the same parent as dotted,
// ranked by the edit distance of the last segment with a generous budget:
// "server.timeout" against "server.timeout_ms" is three edits apart, but
// there is nothing else it could mean.
func siblingKeys(dotted string, pool []string) []string {
	parent, last := "", dotted
	if i := strings.LastIndex(dotted, "."); i >= 0 {
		parent, last = dotted[:i], dotted[i+1:]
	}
	var sibs []string
	for _, k := range pool {
		kp, kl := "", k
		if i := strings.LastIndex(k, "."); i >= 0 {
			kp, kl = k[:i], k[i+1:]
		}
		if kp == parent {
			sibs = append(sibs, kl)
		}
	}
	var out []string
	for _, c := range fuzzy.Rank(last, sibs, 3, max(3, len(last)/3)) {
		if parent != "" {
			out = append(out, parent+"."+c.Text)
		} else {
			out = append(out, c.Text)
		}
	}
	return out
}

// --- anchors ---------------------------------------------------------------

func (r *Resolver) resolveAnchor(ref model.Reference) Result {
	file, slug, _ := strings.Cut(ref.Norm, "#")
	slug = strings.TrimPrefix(slug, "user-content-")
	target := file
	if target == "" {
		target = ref.Loc.File
	}
	if ref.Rooted {
		alt, _, _ := strings.Cut(ref.Text, "#")
		target = ""
		for _, c := range r.candidates(model.Reference{Norm: cleanRel(alt), Loc: ref.Loc, Rooted: true}) {
			if r.ix.FileExists(c) {
				target = c
				break
			}
		}
		if target == "" {
			return Result{Skipped: true} // the path reference reports the missing file
		}
	}
	if !r.ix.FileExists(target) {
		// The target is doc-relative; the path resolver also accepts a
		// root-relative spelling, so try that before giving up. Otherwise
		// the path reference already reports the missing file.
		alt, _, _ := strings.Cut(ref.Text, "#")
		alt = path.Clean(alt)
		if alt == "" || alt == "." || !r.ix.FileExists(alt) {
			return Result{Skipped: true}
		}
		target = alt
	}
	if slug == "" || r.ix.HasAnchor(target, slug) {
		return Result{OK: true, File: target}
	}
	var cands []string
	for _, c := range fuzzy.Rank(slug, r.ix.Anchors(target), 3, maxDist(slug)) {
		cands = append(cands, "#"+c.Text)
	}
	msg := "no heading `#" + slug + "` in " + target
	sev := model.SeverityFor(ref.Confidence)
	if ref.Confidence == model.Medium && r.ix.Project().Intersphinx {
		sev = model.SevInfo // a :ref: label may come from another project's inventory
		msg = "no label `" + slug + "` in this documentation set (intersphinx is configured, so it may be external)"
	}
	return Result{Finding: r.finding(model.RuleBrokenAnchor, sev, ref, msg, cands)}
}

// --- imports ---------------------------------------------------------------

func (r *Resolver) resolveImport(ref model.Reference) Result {
	mp := r.ix.ModulePath()
	if mp == "" {
		return Result{Skipped: true}
	}
	ip := ref.Norm
	if ip != mp && !strings.HasPrefix(ip, mp+"/") {
		return Result{Skipped: true}
	}
	if d, ok := r.ix.GoPackageDir(ip); ok {
		return Result{OK: true, File: d}
	}
	dir := strings.Trim(strings.TrimPrefix(ip, mp), "/")
	if dir == "" || r.ix.DirExists(dir) {
		return Result{OK: true, File: dir}
	}
	var cands []string
	for _, s := range r.ix.SimilarPaths(dir, 3) {
		if r.ix.DirExists(s) {
			cands = append(cands, mp+"/"+s)
		}
	}
	msg := "import path `" + ip + "` does not match any package directory"
	return Result{Finding: r.finding(model.RuleMissingImport, model.SeverityFor(ref.Confidence), ref, msg, cands)}
}

// --- HTTP routes -----------------------------------------------------------

// SplitRoute splits a route reference's Norm ("GET /v1/items" or
// "/healthz") into method and path.
func SplitRoute(norm string) (method, p string) {
	if m, rest, ok := strings.Cut(norm, " "); ok {
		return m, rest
	}
	return "", norm
}

func (r *Resolver) resolveRoute(ref model.Reference) Result {
	if !r.ix.HasRoutes() {
		return Result{Skipped: true} // not a web service; "/x" is just a path
	}
	method, p := SplitRoute(ref.Norm)
	m := r.ix.MatchRoute(method, p)
	if m.OK {
		// no File: /healthz is registered in several places (tests, examples,
		// the service) and a churning handler says nothing about the claim
		return Result{OK: true}
	}
	if len(m.Methods) == 0 && (r.ix.HasLiteral(p) || r.ix.HasLiteral(p+"/") || r.ix.HasLiteral(strings.TrimPrefix(p, "/"))) {
		return Result{OK: true} // registered through a constant or a config default
	}
	sev := model.SeverityFor(ref.Confidence)
	var msg string
	if len(m.Methods) > 0 {
		msg = "`" + p + "` is registered for " + strings.Join(m.Methods, "/") + ", not " + method
	} else {
		msg = "route `" + ref.Text + "` is not registered by any handler"
	}
	var cands []string
	if len(m.Methods) == 0 {
		cands = r.ix.SimilarRoutes(p, 3)
	}
	return Result{Finding: r.finding(model.RuleMissingRoute, sev, ref, msg, cands)}
}

// --- URLs ------------------------------------------------------------------

func (r *Resolver) client() *http.Client {
	r.httpOnce.Do(func() {
		r.httpClient = &http.Client{
			Timeout: r.opts.NetTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				return nil
			},
		}
	})
	return r.httpClient
}

func skipHost(host string) bool {
	h := strings.ToLower(host)
	if hp, _, err := net.SplitHostPort(h); err == nil {
		h = hp // strips the port, including for [::1]:8080
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") ||
		strings.HasSuffix(h, ".example") || h == "example.com" || strings.HasSuffix(h, ".example.com") ||
		h == "example.org" || !strings.Contains(h, ".") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified()
	}
	return false
}

func (r *Resolver) resolveURL(ref model.Reference) Result {
	if !r.opts.Net {
		return Result{Skipped: true}
	}
	u, err := url.Parse(ref.Norm)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || skipHost(u.Host) {
		return Result{Skipped: true}
	}
	var problem string
	if v, ok := r.urlCache.Load(ref.Norm); ok {
		problem = v.(string)
	} else {
		problem = r.probe(ref.Norm)
		r.urlCache.Store(ref.Norm, problem)
	}
	if problem == "" {
		return Result{OK: true}
	}
	msg := "`" + ref.Text + "`: " + problem
	return Result{Finding: r.finding(model.RuleBrokenURL, model.SevWarning, ref, msg, nil)}
}

func (r *Resolver) probe(u string) string {
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.NetTimeout)
	defer cancel()
	try := func(method string) (int, error) {
		req, err := http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("User-Agent", "docrot/1 (+https://github.com/GMfatcat/docrot)")
		resp, err := r.client().Do(req)
		if err != nil {
			return 0, err
		}
		resp.Body.Close()
		return resp.StatusCode, nil
	}
	code, err := try(http.MethodHead)
	if err == nil && (code == http.StatusMethodNotAllowed || code == http.StatusForbidden || code == http.StatusNotImplemented) {
		code, err = try(http.MethodGet)
	}
	if err != nil {
		return "request failed: " + shortErr(err)
	}
	if code >= 400 {
		return "HTTP " + http.StatusText(code) + " (" + strconv.Itoa(code) + ")"
	}
	return ""
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}
