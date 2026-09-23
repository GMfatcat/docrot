package py

import (
	"regexp"
	"strings"

	"docrot/internal/index/routes"
)

var (
	// @app.get("/items"), @router.post('/x'), @app.route("/x", methods=["POST"]),
	// @app.api_route(...), @app.websocket("/ws"), @bp.route("/x")
	reDecorator = regexp.MustCompile(`^@[\w.]+\.(get|post|put|patch|delete|head|options|route|api_route|websocket|websocket_route)\s*\(\s*(.*)$`)
	// app.add_api_route("/x", h, methods=[...]), app.add_url_rule("/x", ...),
	// app.add_route("/x", ...), router.add_websocket_route("/ws", ...)
	reAddRoute = regexp.MustCompile(`\b(add_api_route|add_url_rule|add_route|add_websocket_route|add_api_websocket_route)\s*\(\s*(.*)$`)
	// Starlette: Route("/x", endpoint), Mount("/static", app=...),
	// WebSocketRoute("/ws", ...); Django: path("items/", view), re_path is skipped
	reRouteCtor = regexp.MustCompile(`(?:^|[\s(\[,=])(Route|Mount|WebSocketRoute|Host|path|APIRoute)\s*\(\s*(.*)$`)
	// APIRouter(prefix="/api"), include_router(r, prefix="/v1"), Blueprint(..., url_prefix="/x")
	rePrefixKw = regexp.MustCompile(`\b(?:prefix|url_prefix)\s*=\s*(['"])(/[^'"]*)['"]`)
	reMethods  = regexp.MustCompile(`methods\s*=\s*[\[({]([^\])}]*)[\])}]`)
	reStrLit   = regexp.MustCompile(`^[rfbu]*(['"])([^'"]*)['"]`)
)

// parseRoutes scans one file for HTTP route registrations. The path may sit
// on the line after the opening parenthesis (black's formatting), so the
// scan looks ahead a few lines for the first string literal.
func parseRoutes(lines []string, rel string) []routes.Route {
	var out []routes.Route
	add := func(method, p string, i int, prefix bool) {
		if p == "" || strings.Contains(p, "://") {
			return
		}
		out = append(out, routes.Route{Method: strings.ToUpper(method), Path: p, File: rel, Line: i + 1, Prefix: prefix})
	}
	for i, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := rePrefixKw.FindStringSubmatch(line); m != nil {
			add("", m[2], i, true)
		}
		if m := reDecorator.FindStringSubmatch(line); m != nil {
			p, ok := firstString(lines, i, m[2])
			if !ok {
				continue
			}
			kind := m[1]
			switch kind {
			case "route", "api_route":
				for _, meth := range methodsOf(lines, i) {
					add(meth, p, i, false)
				}
			case "websocket", "websocket_route":
				add("", p, i, false)
			default:
				add(kind, p, i, false)
			}
			continue
		}
		if m := reAddRoute.FindStringSubmatch(line); m != nil {
			if p, ok := firstString(lines, i, m[2]); ok {
				for _, meth := range methodsOf(lines, i) {
					add(meth, p, i, false)
				}
			}
			continue
		}
		if m := reRouteCtor.FindStringSubmatch(line); m != nil {
			if p, ok := firstString(lines, i, m[2]); ok {
				if m[1] == "path" && strings.HasPrefix(p, "/") {
					continue // os.path(...)? Django paths never start with "/"
				}
				if m[1] == "path" && strings.ContainsAny(p, "\\^$") {
					continue // a regex, not a path
				}
				add("", p, i, m[1] == "Mount")
			}
		}
	}
	return out
}

// firstString returns the first string literal in rest, or in the next
// three lines when rest is empty (the argument list continues below).
func firstString(lines []string, i int, rest string) (string, bool) {
	rest = strings.TrimSpace(rest)
	for j := i; j < len(lines) && j <= i+3; j++ {
		if j > i {
			rest = strings.TrimSpace(strings.TrimRight(lines[j], "\r"))
		}
		if rest == "" || rest == ")" {
			continue
		}
		if m := reStrLit.FindStringSubmatch(rest); m != nil {
			return m[2], true
		}
		return "", false // the first argument is not a literal
	}
	return "", false
}

// methodsOf reads methods=[...] from the registration statement, which may
// run over several lines; without it the route accepts any method.
func methodsOf(lines []string, i int) []string {
	var stmt strings.Builder
	depth := 0
	for j := i; j < len(lines) && j <= i+12; j++ {
		l := strings.TrimRight(lines[j], "")
		stmt.WriteString(l)
		stmt.WriteByte(' ')
		depth += strings.Count(l, "(") - strings.Count(l, ")")
		if depth <= 0 {
			break
		}
	}
	if m := reMethods.FindStringSubmatch(stmt.String()); m != nil {
		var out []string
		for _, f := range strings.Split(m[1], ",") {
			f = strings.Trim(strings.TrimSpace(f), `"'`)
			if routes.Methods[strings.ToUpper(f)] {
				out = append(out, strings.ToUpper(f))
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{""}
}
