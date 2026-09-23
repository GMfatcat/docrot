package engine

import (
	"strings"

	"docrot/internal/coverage"
	"docrot/internal/index"
	"docrot/internal/index/routes"
	"docrot/internal/model"
)

// documentedRoutes turns the route references the documents resolved
// ("route|GET /v1/items/{id}", "route|/healthz") into the shapes the route
// index lists: the path normalised ("/v1/items/{}"), once with its method
// and once without.
func documentedRoutes(mentioned map[string]bool) map[string]bool {
	out := map[string]bool{}
	for key := range mentioned {
		norm, ok := strings.CutPrefix(key, string(model.KindRoute)+"|")
		if !ok {
			continue
		}
		method, p, hasMethod := strings.Cut(norm, " ")
		if !hasMethod {
			p = norm
		}
		disp := routes.Display(routes.Normalize(p))
		if hasMethod {
			out[strings.ToUpper(method)+" "+disp] = true
		} else {
			out[disp] = true
		}
	}
	return out
}

// documentedConfigs collects the lower-cased dotted keys the documents
// named, either as a config-key reference ("configkey|server.addr") or as
// the subject of a documented default ("default|key:log.level|info").
func documentedConfigs(mentioned map[string]bool) map[string]bool {
	out := map[string]bool{}
	for key := range mentioned {
		if rest, ok := strings.CutPrefix(key, string(model.KindConfigKey)+"|"); ok {
			out[strings.ToLower(rest)] = true
			continue
		}
		if rest, ok := strings.CutPrefix(key, string(model.KindDefault)+"|key:"); ok {
			if subject, _, found := strings.Cut(rest, "|"); found {
				out[strings.ToLower(subject)] = true
			}
		}
	}
	return out
}

// coverageLocations finds where each undocumented route is registered and
// which sample file holds the undocumented config keys, for the
// `undocumented` findings.
func coverageLocations(ix *index.Index, res coverage.Result) map[string]model.Location {
	out := map[string]model.Location{}
	for _, r := range res.Routes.Missing {
		method, p, hasMethod := strings.Cut(r, " ")
		if !hasMethod {
			method, p = "", r
		}
		p = strings.TrimSuffix(p, "/**")
		if m := ix.MatchRoute(method, p); m.OK && m.File != "" {
			out[r] = model.Location{File: m.File, Line: m.Line}
		}
	}
	if files := ix.ConfigFiles(); len(files) > 0 {
		for _, k := range res.Configs.Missing {
			out["config|"+k] = model.Location{File: files[0]}
		}
	}
	return out
}
