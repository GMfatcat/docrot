package js

import (
	"regexp"
	"strings"

	"docrot/internal/index/routes"
)

const q = "['\"`]"

var (
	// app.get('/x', h), router.post("/x", ...), fastify.delete(`/x`, ...), hono.all('/x')
	reMethodCall = regexp.MustCompile(`\b([A-Za-z_$][A-Za-z0-9_$]*)\.(get|post|put|patch|delete|head|options|all)\(\s*` + q + `([^'"` + "`" + `]*)` + q)
	// app.use('/api', router) — a prefix only with a second argument
	reUse = regexp.MustCompile(`\b(?:app|router|server|express|koa)\.use\(\s*` + q + `(/[^'"` + "`" + `]*)` + q + `\s*,`)
	// fastify.register(plugin, { prefix: '/v1' }), hono .basePath('/api'), app.route('/x', sub)
	rePrefixKey = regexp.MustCompile(`\bprefix\s*:\s*` + q + `(/[^'"` + "`" + `]*)` + q)
	reBasePath  = regexp.MustCompile(`\.basePath\(\s*` + q + `(/[^'"` + "`" + `]*)` + q)
	reHonoRoute = regexp.MustCompile(`\b(?:app|router|hono|[A-Za-z_$]*[Aa]pp)\.route\(\s*` + q + `(/[^'"` + "`" + `]*)` + q + `\s*,`)
	// hono .on('GET', '/x', h) / .on(['GET', 'POST'], '/x', h)
	reOn = regexp.MustCompile(`\.on\(\s*(\[[^\]]*\]|` + q + `[A-Za-z]+` + q + `)\s*,\s*` + q + `(/[^'"` + "`" + `]*)` + q)
	// fastify.route({ method: 'GET', url: '/x' })
	reRouteObj  = regexp.MustCompile(`\.route\(\s*\{`)
	reMethodKey = regexp.MustCompile(`\bmethod\s*:\s*(\[[^\]]*\]|` + q + `[A-Za-z]+` + q + `)`)
	reURLKey    = regexp.MustCompile(`\b(?:url|path)\s*:\s*` + q + `(/[^'"` + "`" + `]*)` + q)
	// NestJS: @Controller('cats'), @Get(':id'), @Post()
	reController = regexp.MustCompile(`^@Controller\(\s*(?:` + q + `([^'"` + "`" + `]*)` + q + `)?`)
	reNestMethod = regexp.MustCompile(`^@(Get|Post|Put|Patch|Delete|Head|Options|All)\(\s*(?:` + q + `([^'"` + "`" + `]*)` + q + `)?`)
	reMethodWord = regexp.MustCompile(`[A-Za-z]+`)
)

// receivers that register routes; anything else (axios, fetch wrappers,
// redis clients) is a client call.
var routerReceiver = regexp.MustCompile(`^(?:app|application|router|routes|route|server|fastify|api|hono|r|express|koa|instance|this|[A-Za-z_$]*(?:Router|Server|App|Fastify|Hono)|[a-z]+(?:Router|Server|App))$`)

// parseRoutes scans one file for HTTP route registrations of express,
// koa-router, fastify, hono and NestJS. Only string literals count.
func parseRoutes(lines []string, rel string) []routes.Route {
	var out []routes.Route
	add := func(method, p string, i int, prefix bool) {
		if p == "" || strings.Contains(p, "://") {
			return
		}
		out = append(out, routes.Route{Method: strings.ToUpper(method), Path: p, File: rel, Line: i + 1, Prefix: prefix})
	}
	controller := ""
	for i, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "*") {
			continue
		}
		if m := reController.FindStringSubmatch(line); m != nil {
			controller = "/" + strings.Trim(m[1], "/")
			if controller == "/" {
				controller = ""
			}
			continue
		}
		if m := reNestMethod.FindStringSubmatch(line); m != nil {
			p := controller + "/" + strings.Trim(m[2], "/")
			p = strings.TrimSuffix(p, "/")
			if p == "" {
				p = "/"
			}
			method := m[1]
			if method == "All" {
				method = ""
			}
			add(method, p, i, false)
			continue
		}
		for _, m := range reMethodCall.FindAllStringSubmatch(line, -1) {
			if !routerReceiver.MatchString(m[1]) || !(strings.HasPrefix(m[3], "/") || m[3] == "*") {
				continue
			}
			method := m[2]
			if method == "all" {
				method = ""
			}
			add(method, m[3], i, false)
		}
		if m := reUse.FindStringSubmatch(line); m != nil {
			add("", m[1], i, true)
		}
		if m := rePrefixKey.FindStringSubmatch(line); m != nil {
			add("", m[1], i, true)
		}
		if m := reBasePath.FindStringSubmatch(line); m != nil {
			add("", m[1], i, true)
		}
		if m := reHonoRoute.FindStringSubmatch(line); m != nil {
			add("", m[1], i, true)
		}
		if m := reOn.FindStringSubmatch(line); m != nil {
			for _, meth := range reMethodWord.FindAllString(m[1], -1) {
				add(meth, m[2], i, false)
			}
		}
		if reRouteObj.MatchString(line) {
			// fastify.route({ method: 'GET', url: '/x', ... }): keys may follow on the next lines
			block := line
			for j := i + 1; j < len(lines) && j <= i+8 && !strings.Contains(block, "})"); j++ {
				block += " " + strings.TrimSpace(lines[j])
			}
			um := reURLKey.FindStringSubmatch(block)
			if um == nil {
				continue
			}
			if mm := reMethodKey.FindStringSubmatch(block); mm != nil {
				for _, meth := range reMethodWord.FindAllString(mm[1], -1) {
					add(meth, um[1], i, false)
				}
			} else {
				add("", um[1], i, false)
			}
		}
	}
	return out
}
