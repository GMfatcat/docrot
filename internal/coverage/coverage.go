// Package coverage answers the question the rest of docrot does not: which
// parts of the code surface no document mentions at all (design §13).
//
// It takes the exported items collected by the index and the set of
// references the documents actually resolved, and reports a documented/total
// ratio plus the missing entries per Go package, for command-line flags and
// for environment variables.
package coverage

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"docrot/internal/model"
)

// Group names used for the two non-package buckets.
const (
	FlagsGroup = "flags"
	EnvGroup   = "env"
)

// Mention key prefixes accepted in the mentioned set passed to [Compute].
const (
	symbolPrefix = "gosym|"
	flagPrefix   = "flag|"
	envPrefix    = "env|"
)

// Group is the coverage of one bucket: one Go package, all flags or all
// environment variables. Missing lists the undocumented entries, sorted.
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
// package name, plus the flag and environment groups.
type Result struct {
	Packages []Group `json:"packages"`
	Flags    Group   `json:"flags"`
	Envs     Group   `json:"envs"`
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
// Location is the zero value. The order is packages (in Result order), then
// flags, then environment variables.
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
