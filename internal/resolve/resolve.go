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
	switch ref.Kind {
	case model.KindPath, model.KindCommand:
		return r.resolvePath(ref)
	case model.KindGoSymbol:
		return r.resolveGoSymbol(ref)
	case model.KindOdinSym, model.KindPySym:
		return r.resolveOtherSymbol(ref)
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
	if docDir != "." && docDir != "" {
		cands = append(cands, path.Clean(docDir+"/"+norm))
	}
	if !strings.HasPrefix(norm, "../") {
		cands = append(cands, path.Clean(norm))
	}
	return cands
}

// existsOnDisk is the fallback for paths hidden from the index by exclude
// rules (vendor/, dist/, …).
func (r *Resolver) existsOnDisk(rel string) bool {
	if r.opts.Root == "" || strings.HasPrefix(rel, "..") {
		return false
	}
	_, err := os.Stat(filepath.Join(r.opts.Root, filepath.FromSlash(rel)))
	return err == nil
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
	rule := model.RuleMissingPath
	if ref.Kind == model.KindCommand {
		rule = model.RuleMissingCommand
	}
	sev := model.SeverityFor(ref.Confidence)
	var msg string
	var cands []string
	if isGlob {
		msg = "no file matches `" + ref.Text + "`"
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
		// A bare file name ("main.go", "config.json") is a weak claim: it
		// usually means "the main.go of whatever we are talking about".
		if !strings.Contains(ref.Norm, "/") && sev.Rank() > model.SevInfo.Rank() {
			sev = model.SevInfo
		}
	}
	return Result{Finding: r.finding(rule, sev, ref, msg, cands)}
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
	sev := model.SeverityFor(ref.Confidence)
	var msg string
	switch {
	case len(parts) == 1:
		msg = "function or type `" + ref.Text + "` not found in any package"
		if ref.Confidence < model.High {
			sev = model.SevInfo // a bare call is usually a method or a local helper
		}
	case r.ix.IsGoPackage(first):
		if strings.ToLower(last) == last && (len(parts) > 1 && (strings.Contains(last, "_") || r.ix.HasJSONKey(q) || r.ix.HasConfigKey(q))) {
			// kernel.max_concurrent: a config key that happens to start with a package name
			if r.ix.HasJSONKey(q) || r.ix.HasConfigKey(q) {
				return Result{OK: true}
			}
			return r.resolveConfigKey(model.Reference{Kind: model.KindConfigKey, Text: ref.Text, Norm: q, Confidence: model.Low, Loc: ref.Loc, Section: ref.Section, Context: ref.Context})
		}
		msg = "`" + q + "` not found in package " + first
		if len(parts) == 2 && r.ix.HasGoMember(last) && isReceiverish(first) {
			// `cfg.Addr` where cfg is both a package and a common variable name
			msg += " (if `" + first + "` is a variable, its type may have `" + last + "`)"
			sev = model.SevInfo
		}
	case r.ix.IsGoType(first):
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
	case "config", "logger", "client", "server", "store", "router", "handler", "worker", "service", "runner", "engine", "index", "opts", "options", "flags", "state", "cache", "queue", "pool", "conn", "resp", "req":
		return true
	}
	return false
}

// --- Odin / Python ---------------------------------------------------------

func (r *Resolver) resolveOtherSymbol(ref model.Reference) Result {
	var has func(string) bool
	var sim func(string, int) []string
	var lang string
	if ref.Kind == model.KindOdinSym {
		if !r.ix.HasOdin() {
			return Result{Skipped: true}
		}
		has, sim, lang = r.ix.HasOdinSymbol, r.ix.SimilarOdinSymbols, "Odin"
	} else {
		if !r.ix.HasPython() {
			return Result{Skipped: true}
		}
		has, sim, lang = r.ix.HasPySymbol, r.ix.SimilarPySymbols, "Python"
	}
	if has(ref.Norm) {
		return Result{OK: true}
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
	msg := lang + " symbol `" + ref.Text + "` not found"
	return Result{Finding: r.finding(model.RuleMissingSymbol, model.SeverityFor(ref.Confidence), ref, msg, sim(ref.Norm, 3))}
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
	if r.ix.HasEnv(ref.Norm) {
		return Result{OK: true}
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
	if r.ix.HasJSONKey(ref.Norm) || r.ix.HasConfigKey(ref.Norm) {
		return Result{OK: true}
	}
	// only report when the top-level segment is a real config section;
	// otherwise "rec.status" is just a variable in prose
	top, _, _ := strings.Cut(ref.Norm, ".")
	if !r.ix.HasJSONKey(top) && !r.ix.HasConfigKey(top) {
		return Result{Skipped: true}
	}
	pool := append(append([]string{}, r.ix.JSONKeys()...), r.ix.ConfigKeys()...)
	var cands []string
	for _, c := range fuzzy.Rank(ref.Norm, pool, 3, maxDist(ref.Norm)) {
		cands = append(cands, c.Text)
	}
	msg := "config key `" + ref.Text + "` not found in any config struct tag or sample file"
	return Result{Finding: r.finding(model.RuleUnknownConfigKey, model.SevInfo, ref, msg, cands)}
}

// --- anchors ---------------------------------------------------------------

func (r *Resolver) resolveAnchor(ref model.Reference) Result {
	file, slug, _ := strings.Cut(ref.Norm, "#")
	slug = strings.TrimPrefix(slug, "user-content-")
	target := file
	if target == "" {
		target = ref.Loc.File
	}
	if !r.ix.FileExists(target) {
		// the path reference already reports the missing file
		return Result{Skipped: true}
	}
	if slug == "" || r.ix.HasAnchor(target, slug) {
		return Result{OK: true, File: target}
	}
	var cands []string
	for _, c := range fuzzy.Rank(slug, r.ix.Anchors(target), 3, maxDist(slug)) {
		cands = append(cands, "#"+c.Text)
	}
	msg := "no heading `#" + slug + "` in " + target
	return Result{Finding: r.finding(model.RuleBrokenAnchor, model.SeverityFor(ref.Confidence), ref, msg, cands)}
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
	dir := strings.TrimPrefix(strings.TrimPrefix(ip, mp), "/")
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
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h, "]") {
		h = h[:i]
	}
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
