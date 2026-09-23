// Package coverage answers the question the rest of docrot does not: which
// parts of the code surface no document mentions at all (design §13).
//
// It takes the exported items collected by the index and the set of
// references the documents actually resolved, and reports a documented/total
// ratio plus the missing entries per Go package, for command-line flags,
// for environment variables, for registered HTTP routes and for the keys
// of the configuration samples.
package coverage

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"docrot/internal/model"
)

// Group names used for the non-package buckets.
const (
	FlagsGroup  = "flags"
	EnvGroup    = "env"
	RoutesGroup = "routes"
	ConfigGroup = "config"
)

// Mention key prefixes accepted in the mentioned set passed to [Compute].
const (
	symbolPrefix = "gosym|"
	flagPrefix   = "flag|"
	envPrefix    = "env|"
)

// Group is the coverage of one bucket: one Go package, all flags, all
// environment variables, all routes or all config keys. Missing lists the
// undocumented entries, sorted.
type Group struct {
	Name       string   `json:"name"`
	Total      int      `json:"total"`
	Documented int      `json:"documented"`
	Missing    []string `json:"missing,omitempty"`
}

// Percent returns the documented share of the group, rounded to the nearest
// whole percent. An empty group counts as fully documented (100).
func (g Group) Percent() int {
	if g.Total <= 0 {
		return 100
	}
	return int(math.Round(float64(g.Documented) / float64(g.Total) * 100))
}

// Result is the whole coverage report: one Group per Go package, sorted by
// package name, plus the flag, environment, route and config groups.
// Compute fills the first three; Routes and Configs fill the rest.
// Locations optionally maps a missing route ("GET /items/{}") or config
// key ("config|server.addr") to where the code declares it, for Findings.
type Result struct {
	Packages  []Group                   `json:"packages"`
	Flags     Group                     `json:"flags"`
	Envs      Group                     `json:"envs"`
	Routes    Group                     `json:"routes"`
	Configs   Group                     `json:"config"`
	Locations map[string]model.Location `json:"-"`
}

// Compute matches every exported item against the set of mentioned
// references.
//
// mentioned holds keys of the form "gosym|<norm>", "flag|<norm>" and
// "env|<norm>", where the norms are whatever the documents wrote after
// normalisation ("pkg.Name", "pkg.Type.Method", "Type.Method", "Name").
// A Go symbol counts as documented when its qualified name is mentioned, or
// when the package-less form of it is ("Type.Method" or "Name"), because
// documents rarely repeat the package name. A flag matches case-insensitively
// with '_' treated as '-', an environment variable matches exactly.
//
// Items whose Kind is neither [model.KindFlag] nor [model.KindEnv] are
// treated as Go symbols.
func Compute(exported []model.Exported, mentioned map[string]bool) Result {
	flags := map[string]bool{}
	for key := range mentioned {
		if rest, ok := strings.CutPrefix(key, flagPrefix); ok {
			flags[normFlag(rest)] = true
		}
	}

	res := Result{
		Flags: Group{Name: FlagsGroup},
		Envs:  Group{Name: EnvGroup},
	}
	pkgs := map[string]*Group{}
	var order []string

	for _, e := range exported {
		switch e.Kind {
		case model.KindFlag:
			record(&res.Flags, e.Qualified, flags[normFlag(e.Qualified)])
		case model.KindEnv:
			record(&res.Envs, e.Qualified, mentioned[envPrefix+e.Qualified])
		default:
			g := pkgs[e.Package]
			if g == nil {
				g = &Group{Name: e.Package}
				pkgs[e.Package] = g
				order = append(order, e.Package)
			}
			record(g, e.Qualified, symbolMentioned(e, mentioned))
		}
	}

	sort.Strings(order)
	for _, name := range order {
		g := pkgs[name]
		sort.Strings(g.Missing)
		res.Packages = append(res.Packages, *g)
	}
	sort.Strings(res.Flags.Missing)
	sort.Strings(res.Envs.Missing)
	return res
}

// Routes computes the route group. listed holds every registered route as
// the index lists it: "GET /items/{}" (parameters normalised to "{}"),
// "/healthz" for a route of any method, "/api/**" for a mounted prefix.
// documented holds every route a document mentioned in the same shape:
// "GET /items/{}" when the document gave a method, "/items/{}" when it did
// not. A listed route with a method is documented when it was mentioned
// with that method or with none; one without a method when its path was
// mentioned with any method; a prefix when anything under it was. A
// mention whose path ends with the listed path ("/py/items/{}" for a
// router's "/items/{}") counts too, as it does when the route is resolved.
func Routes(listed []string, documented map[string]bool) Group {
	g := Group{Name: RoutesGroup}
	type mention struct{ method, path string }
	var mentions []mention
	for d := range documented {
		m, p, hasMethod := strings.Cut(d, " ")
		if !hasMethod {
			m, p = "", d
		}
		mentions = append(mentions, mention{m, p})
	}
	for _, r := range listed {
		method, p, hasMethod := strings.Cut(r, " ")
		if !hasMethod {
			method, p = "", r
		}
		ok := false
		for _, m := range mentions {
			if hasMethod && m.method != "" && m.method != method {
				continue
			}
			if strings.HasSuffix(p, "/**") {
				base := strings.TrimSuffix(p, "**")
				if strings.HasPrefix(m.path, base) || m.path+"/" == base {
					ok = true
					break
				}
				continue
			}
			if m.path == p || (p != "/" && strings.HasSuffix(m.path, p)) {
				ok = true
				break
			}
		}
		record(&g, r, ok)
	}
	sort.Strings(g.Missing)
	return g
}

// Configs computes the config-key group from the dotted keys of the
// configuration samples. Only leaf keys count — "server.addr", never
// "server" — because a document names settings, not the sections they sit
// in. mentioned holds the lower-cased dotted keys the documents named.
func Configs(keys []string, mentioned map[string]bool) Group {
	g := Group{Name: ConfigGroup}
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	for _, k := range keys {
		if hasChild(k, set) {
			continue
		}
		record(&g, k, mentioned[strings.ToLower(k)])
	}
	sort.Strings(g.Missing)
	return g
}

// hasChild reports whether some key extends k by a dotted segment.
func hasChild(k string, set map[string]bool) bool {
	prefix := k + "."
	for other := range set {
		if strings.HasPrefix(other, prefix) {
			return true
		}
	}
	return false
}

// record counts one item into g, appending it to Missing when undocumented.
func record(g *Group, qualified string, documented bool) {
	g.Total++
	if documented {
		g.Documented++
		return
	}
	g.Missing = append(g.Missing, qualified)
}

// symbolMentioned reports whether a Go symbol is mentioned either by its
// qualified name or by its package-less form.
func symbolMentioned(e model.Exported, mentioned map[string]bool) bool {
	if mentioned[symbolPrefix+e.Qualified] {
		return true
	}
	short := unqualify(e.Package, e.Qualified)
	return short != "" && mentioned[symbolPrefix+short]
}

// unqualify strips the leading package component from a qualified name:
// "pkg.Type.Method" becomes "Type.Method" and "pkg.Name" becomes "Name".
// It returns "" when there is nothing to strip.
func unqualify(pkg, qualified string) string {
	if pkg != "" {
		if rest, ok := strings.CutPrefix(qualified, pkg+"."); ok {
			return rest
		}
		return ""
	}
	if i := strings.IndexByte(qualified, '.'); i >= 0 {
		return qualified[i+1:]
	}
	return ""
}

// normFlag normalises a flag name for comparison: leading dashes removed,
// lower-cased, underscores folded to hyphens.
func normFlag(name string) string {
	s := strings.TrimLeft(strings.TrimSpace(name), "-")
	s = strings.ToLower(s)
	return strings.ReplaceAll(s, "_", "-")
}

// Findings turns the missing entries of r into `undocumented` findings with
// the given severity.
//
// Go symbols are located at their defining file and line, taken from
// exported; flags and environment variables have no location, so their
// Location is the zero value; routes and config keys use r.Locations when
// the engine filled it. The order is packages (in Result order), then
// flags, environment variables, routes and config keys.
func Findings(r Result, exported []model.Exported, sev model.Severity) []model.Finding {
	locs := make(map[string]model.Location, len(exported))
	for _, e := range exported {
		if e.Kind == model.KindFlag || e.Kind == model.KindEnv {
			continue
		}
		if _, ok := locs[e.Qualified]; !ok {
			locs[e.Qualified] = model.Location{File: e.File, Line: e.Line}
		}
	}

	var out []model.Finding
	for _, g := range r.Packages {
		for _, q := range g.Missing {
			out = append(out, finding(model.KindGoSymbol, q, locs[q], sev,
				fmt.Sprintf("exported symbol %s is not mentioned in any document", q)))
		}
	}
	for _, q := range r.Flags.Missing {
		out = append(out, finding(model.KindFlag, q, model.Location{}, sev,
			fmt.Sprintf("flag %s is not mentioned in any document", q)))
	}
	for _, q := range r.Envs.Missing {
		out = append(out, finding(model.KindEnv, q, model.Location{}, sev,
			fmt.Sprintf("environment variable %s is not mentioned in any document", q)))
	}
	for _, q := range r.Routes.Missing {
		out = append(out, finding(model.KindRoute, q, r.Locations[q], sev,
			fmt.Sprintf("route %s is not mentioned in any document", q)))
	}
	for _, q := range r.Configs.Missing {
		out = append(out, finding(model.KindConfigKey, q, r.Locations["config|"+q], sev,
			fmt.Sprintf("config key %s is not mentioned in any document", q)))
	}
	return out
}

// finding builds one undocumented finding.
func finding(kind model.Kind, qualified string, loc model.Location, sev model.Severity, msg string) model.Finding {
	return model.Finding{
		Rule:        model.RuleUndocumented,
		Severity:    sev,
		Message:     msg,
		Loc:         loc,
		Fingerprint: model.Fingerprint(model.RuleUndocumented, string(kind), qualified),
	}
}
