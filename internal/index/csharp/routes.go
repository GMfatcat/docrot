package csharp

import (
	"regexp"
	"strings"

	"docrot/internal/index/routes"
)

var (
	// app.MapGet("/x", …), group.MapPost("/", …), app.Map("/", …), app.MapMethods("/x", new[] { "GET" }, …)
	reMapCall = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\.Map(Get|Post|Put|Patch|Delete|Methods|)\(\s*"([^"]*)"`)
	// var group = app.MapGroup("/todos");  routes.MapGroup("/todos")
	reMapGroup  = regexp.MustCompile(`(?:(?:var|\w+)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*)?[A-Za-z_][A-Za-z0-9_]*\.MapGroup\(\s*"([^"]*)"`)
	reMethodArr = regexp.MustCompile(`"([A-Za-z]+)"`)
	// [Route("api/[controller]")], [HttpGet("{id}")], [HttpPost], [HttpDelete("{id:int}")]
	reRouteAttr = regexp.MustCompile(`^\[Route\(\s*"([^"]*)"`)
	reHTTPAttr  = regexp.MustCompile(`^\[Http(Get|Post|Put|Patch|Delete|Head|Options)(?:\(\s*"([^"]*)"\s*\))?`)
	reCtrlClass = regexp.MustCompile(`\bclass\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

// parseRoutes scans one file for ASP.NET Core route registrations: minimal
// API Map* calls (with MapGroup prefixes tracked by variable name) and
// attribute-routed controllers.
func parseRoutes(lines []string, rel string) []routes.Route {
	var out []routes.Route
	add := func(method, p string, i int, prefix bool) {
		if p == "" || strings.Contains(p, "://") {
			return
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		out = append(out, routes.Route{Method: strings.ToUpper(method), Path: p, File: rel, Line: i + 1, Prefix: prefix})
	}
	groups := map[string]string{} // variable → prefix
	ctrlPrefix, pendingRoute := "", ""
	pendingHTTP := [][2]string{} // attributes waiting for the method below
	for i, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if m := reRouteAttr.FindStringSubmatch(line); m != nil {
			pendingRoute = strings.Trim(m[1], "/")
		}
		if m := reHTTPAttr.FindStringSubmatch(line); m != nil {
			pendingHTTP = append(pendingHTTP, [2]string{m[1], strings.Trim(m[2], "/")})
			continue
		}
		if m := reCtrlClass.FindStringSubmatch(line); m != nil && pendingRoute != "" {
			name := strings.ToLower(strings.TrimSuffix(m[1], "Controller"))
			ctrlPrefix = "/" + strings.ReplaceAll(pendingRoute, "[controller]", name)
			pendingRoute = ""
			continue
		}
		if len(pendingHTTP) > 0 && (strings.Contains(line, "(") || strings.HasPrefix(line, "public ")) {
			for _, h := range pendingHTTP {
				p := ctrlPrefix
				if h[1] != "" {
					p += "/" + h[1]
				}
				if p == "" {
					p = "/"
				}
				add(h[0], p, i, false)
			}
			pendingHTTP = nil
		}
		for _, m := range reMapGroup.FindAllStringSubmatch(line, -1) {
			prefix := groups[""] + m[2]
			if m[1] != "" {
				groups[m[1]] = prefix
			}
			add("", prefix, i, true)
		}
		for _, m := range reMapCall.FindAllStringSubmatch(line, -1) {
			p := groups[m[1]] + m[3]
			switch m[2] {
			case "":
				add("", p, i, false)
			case "Methods":
				rest := line[strings.Index(line, m[0])+len(m[0]):]
				if k := strings.Index(rest, "}"); k >= 0 {
					rest = rest[:k]
				}
				for _, mm := range reMethodArr.FindAllStringSubmatch(rest, -1) {
					add(mm[1], p, i, false)
				}
			default:
				add(m[2], p, i, false)
			}
		}
	}
	return out
}
