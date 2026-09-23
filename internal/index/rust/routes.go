package rust

import (
	"regexp"
	"strings"

	"docrot/internal/index/routes"
)

var (
	// axum: .route("/x", get(h).post(h2)), .route_service("/x", svc),
	// tide: app.at("/x").get(h)
	reAxumRoute = regexp.MustCompile(`\.(route|route_service|at)\(\s*"([^"]*)"(.*)$`)
	reAxumNest  = regexp.MustCompile(`\.(nest|nest_service)\(\s*"([^"]*)"`)
	reMethodFn  = regexp.MustCompile(`\b(get|post|put|patch|delete|head|options|trace|connect|any)\s*\(`)
	// actix-web and rocket attributes: #[get("/x")], #[post("/x", data = "<f>")],
	// #[route("/x", method = "GET")]
	reAttrRoute  = regexp.MustCompile(`^#\[(?:actix_web::|rocket::)?(get|post|put|patch|delete|head|options|route)\(\s*"([^"]*)"(.*)\]`)
	reAttrMethod = regexp.MustCompile(`method\s*=\s*"([A-Za-z]+)"`)
	// actix: web::resource("/x"), web::scope("/api"); rocket: .mount("/api", routes![...])
	reResource = regexp.MustCompile(`\b(?:web::)?resource\(\s*"([^"]*)"`)
	reScope    = regexp.MustCompile(`\b(?:web::)?scope\(\s*"([^"]*)"`)
	reMount    = regexp.MustCompile(`\.mount\(\s*"([^"]*)"`)
)

// parseRoutes scans one file for HTTP route registrations of axum,
// actix-web, rocket and tide. Only string literals count.
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
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if m := reAttrRoute.FindStringSubmatch(line); m != nil {
			method := m[1]
			if method == "route" {
				method = ""
				if mm := reAttrMethod.FindStringSubmatch(m[3]); mm != nil {
					method = mm[1]
				}
			}
			add(method, m[2], i, false)
			continue
		}
		for _, m := range reAxumNest.FindAllStringSubmatch(line, -1) {
			add("", m[2], i, true)
		}
		if m := reScope.FindStringSubmatch(line); m != nil {
			add("", m[1], i, true)
		}
		if m := reMount.FindStringSubmatch(line); m != nil {
			add("", m[1], i, true)
		}
		if m := reResource.FindStringSubmatch(line); m != nil {
			add("", m[1], i, false)
		}
		if m := reAxumRoute.FindStringSubmatch(line); m != nil {
			// the method helpers may continue on the next lines
			rest := m[3]
			for j := i + 1; j < len(lines) && j <= i+4 && !strings.Contains(rest, ")"); j++ {
				rest += " " + strings.TrimSpace(lines[j])
			}
			methods := reMethodFn.FindAllStringSubmatch(rest, -1)
			if len(methods) == 0 || m[1] != "route" {
				add("", m[2], i, false)
				continue
			}
			for _, mm := range methods {
				if mm[1] == "any" {
					add("", m[2], i, false)
				} else {
					add(mm[1], m[2], i, false)
				}
			}
		}
	}
	return out
}
