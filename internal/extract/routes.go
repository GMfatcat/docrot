package extract

import (
	"regexp"
	"strings"

	"docrot/internal/model"
)

var (
	// `GET /v1/items`, `POST /items/{id}`: a whole code span
	reRouteSpan = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(/[^\s]*)$`)
	// `/healthz`, `/items/{item_id}`: a bare path span
	rePathSpan = regexp.MustCompile(`^/[A-Za-z0-9_\-.{}:<>*~%+@/]*$`)
	// "GET /v1/items" in prose or a table row, with or without backticks;
	// the method must stand alone (not "TARGET /x") and the path ends at
	// whitespace or punctuation that closes a cell, a span or a sentence.
	reRouteProse = regexp.MustCompile("(?:^|[\\s|(`\"'])(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)[\\s|]+`?(/[A-Za-z0-9_\\-.{}:<>*~%+@/]*)")
	// GET /v1/items HTTP/1.1 in an http fence
	reRouteHTTP = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(/\S*)(?:\s+HTTP/[0-9.]+)?\s*$`)
	// curl http://localhost:8080/v1/items, curl -X POST 127.0.0.1:3000/x,
	// $BASE/x and {{host}}/x: a local or placeholder host followed by a path
	reLocalURL   = regexp.MustCompile(`(?:https?://)?(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\]|\$\{?[A-Z_]+\}?|\{\{[^}]*\}\}|<[a-z-]+>|host|HOST|example\.com|api\.example\.com|your-host)(?::\d+|:\$?\{?[A-Z_]+\}?)?(/[A-Za-z0-9_\-.{}:<>*~%+@/]*)`)
	reCurlMethod = regexp.MustCompile(`(?:-X|--request)\s+([A-Za-z]+)`)
)

// osDirs are the first segments of absolute file-system paths; "/usr/bin"
// or "/etc/hosts" in a document is never a route.
var osDirs = map[string]bool{
	"usr": true, "etc": true, "var": true, "tmp": true, "home": true, "opt": true, "dev": true,
	"proc": true, "bin": true, "sbin": true, "lib": true, "lib64": true, "mnt": true, "media": true,
	"root": true, "srv": true, "boot": true, "sys": true, "run": true, "Users": true, "Applications": true,
	"Library": true, "System": true, "Volumes": true, "c": true, "d": true, "e": true, "C": true, "D": true,
	"cygdrive": true, "path": true, "your": true, "some": true, "mnt-c": true, "workspace": true,
}

// fileOnlyExt are extensions a URL path would not end with; "/main.go" or
// "/x.exe" is a file path written with a leading slash.
var fileOnlyExt = map[string]bool{
	"go": true, "py": true, "odin": true, "exe": true, "dll": true, "so": true, "dylib": true,
	"md": true, "txt": true, "yaml": true, "yml": true, "toml": true, "sh": true, "ps1": true,
	"zip": true, "tar": true, "gz": true, "log": true, "sock": true, "pid": true, "conf": true,
	"ini": true, "cfg": true, "pem": true, "key": true, "crt": true, "mod": true, "sum": true,
	"c": true, "h": true, "rs": true, "java": true, "ts": true, "tsx": true, "jsx": true,
}

// routeRef builds a route reference for a documented path, or nil when the
// path is not plausibly an HTTP route.
func (x *extractor) routeRef(method, p string, conf model.Confidence, text string) *model.Reference {
	p = strings.TrimSpace(p)
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if p == "" || p == "/" && method == "" || strings.Contains(p, "..") || strings.Contains(p, "//") {
		return nil
	}
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if osDirs[segs[0]] {
		return nil
	}
	last := segs[len(segs)-1]
	if i := strings.LastIndex(last, "."); i > 0 && fileOnlyExt[strings.ToLower(last[i+1:])] {
		return nil
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	norm := p
	if method != "" {
		norm = method + " " + p
	}
	if text == "" {
		text = norm
	}
	return &model.Reference{Kind: model.KindRoute, Text: text, Norm: norm, Confidence: conf}
}

// routeSpan classifies a whole code span as a route claim. Repositories
// that register no routes get none: there "/x" is just an absolute path.
func (x *extractor) routeSpan(s string) *model.Reference {
	if !x.hints.HasRoutes() {
		return nil
	}
	if m := reRouteSpan.FindStringSubmatch(s); m != nil {
		return x.routeRef(m[1], m[2], model.High, s)
	}
	if rePathSpan.MatchString(s) && len(s) > 1 {
		// a bare path is often a mention ("mount it at /metrics"), not a
		// claim that the handler exists: info unless a method is named
		return x.routeRef("", s, model.Low, s)
	}
	return nil
}

// routes scans prose lines, table rows, http fences and shell examples for
// route claims that are not whole code spans. Route claims are only worth
// extracting when the code registers routes, so the pass is skipped for
// repositories without any.
func (x *extractor) routes() {
	if !x.hints.HasRoutes() {
		return
	}
	inFence := make([]bool, len(x.doc.Lines)+1)
	for _, f := range x.doc.Fences {
		for i := f.StartLine; i <= f.EndLine && i < len(inFence); i++ {
			inFence[i] = true
		}
	}
	for i, line := range x.doc.Lines {
		ln := i + 1
		if inFence[ln] || x.ignored(ln) {
			continue
		}
		for _, m := range reRouteProse.FindAllStringSubmatch(line, -1) {
			conf := model.Medium
			if strings.Contains(line, "|") {
				conf = model.High // a table row: METHOD | /path
			}
			if r := x.routeRef(m[1], m[2], conf, m[1]+" "+m[2]); r != nil {
				x.emit(*r, ln, 0, x.doc.SectionAt(ln), "")
			}
		}
	}
	for _, f := range x.doc.Fences {
		if x.ignored(f.StartLine) {
			continue
		}
		section := x.doc.SectionAt(f.StartLine)
		lang := strings.ToLower(f.Lang)
		for i, raw := range f.Content {
			ln := f.StartLine + 1 + i
			line := strings.TrimSpace(raw)
			switch {
			case lang == "http" || lang == "" || lang == "text" || lang == "console" || lang == "plaintext":
				if m := reRouteHTTP.FindStringSubmatch(line); m != nil {
					conf := model.Medium // a plain-text listing may be a plan
					if lang == "http" {
						conf = model.High // an HTTP request example
					}
					if r := x.routeRef(m[1], m[2], conf, m[1]+" "+m[2]); r != nil {
						x.emit(*r, ln, 0, section, lang)
					}
				}
				if lang == "http" {
					continue
				}
				fallthrough
			case shellLangs[lang]:
				if !strings.Contains(line, "curl") && !strings.Contains(line, "http") && !strings.Contains(line, "localhost") {
					continue
				}
				method := ""
				if m := reCurlMethod.FindStringSubmatch(line); m != nil {
					method = strings.ToUpper(m[1])
				}
				for _, m := range reLocalURL.FindAllStringSubmatch(line, -1) {
					if r := x.routeRef(method, m[1], model.Medium, ""); r != nil {
						x.emit(*r, ln, 0, section, lang)
					}
				}
			}
		}
	}
}
