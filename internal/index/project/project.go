// Package project reads a repository's identity files — go.mod,
// pyproject.toml, setup.cfg, package.json — and its task runners'
// definitions (Makefile, justfile, Taskfile.yml, package.json scripts), so
// that a document's install line, toolchain requirement and `make target`
// can be checked against what the repository actually declares.
//
// Everything here is line-oriented and forgiving: a Makefile that uses
// every GNU extension still yields its plain targets, and a pyproject.toml
// only needs [project] name / requires-python (or the Poetry equivalents).
package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"docrot/internal/model"
)

var (
	reMakeTarget = regexp.MustCompile(`^([A-Za-z0-9_./$()%-][^:=#]*?)\s*:(?:[^=]|$)`)
	reJustRecipe = regexp.MustCompile(`^@?([A-Za-z_][A-Za-z0-9_-]*)(?:\s+[^\s:]+)*\s*:(?:[^=]|$)`)
	reTaskKey    = regexp.MustCompile(`^  ([A-Za-z0-9_:.-]+):\s*(?:#.*)?$`)
	reTOMLSect   = regexp.MustCompile(`^\[([^\]]+)\]`)
	reTOMLKey    = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*=\s*"([^"]*)"`)
	reTOMLKeyS   = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*=\s*'([^']*)'`)
	reGoDirect   = regexp.MustCompile(`^go\s+(\d+\.\d+)`)
	reModule     = regexp.MustCompile(`^module\s+"?([^\s"]+)"?`)
	reCfgKey     = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*=\s*(.+?)\s*$`)
)

// Build reads the identity and task-runner files directly under root.
// Missing files are simply absent from the result; nothing here fails.
func Build(root string) model.Project {
	p := model.Project{Targets: map[string][]string{}, TargetFiles: map[string]string{}}
	if lines, ok := read(root, "go.mod"); ok {
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if m := reModule.FindStringSubmatch(l); m != nil && p.GoModule == "" {
				p.GoModule = m[1]
			} else if m := reGoDirect.FindStringSubmatch(l); m != nil && p.GoVersion == "" {
				p.GoVersion = m[1]
			}
		}
	}
	if lines, ok := read(root, "pyproject.toml"); ok {
		p.PyName, p.PyRequires = pyproject(lines)
	}
	if p.PyName == "" {
		if lines, ok := read(root, "setup.cfg"); ok {
			p.PyName = setupCfgName(lines)
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		var pkg struct {
			Name    string            `json:"name"`
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(b, &pkg) == nil {
			p.NPMName = pkg.Name
			if len(pkg.Scripts) > 0 {
				p.Targets["npm"] = sortedKeys(pkg.Scripts)
				p.TargetFiles["npm"] = "package.json"
			}
		}
	}
	for _, name := range []string{"Makefile", "GNUmakefile", "makefile"} {
		if lines, ok := read(root, name); ok {
			p.Targets["make"] = makeTargets(lines)
			p.TargetFiles["make"] = name
			break
		}
	}
	for _, name := range []string{"justfile", "Justfile", ".justfile"} {
		if lines, ok := read(root, name); ok {
			p.Targets["just"] = justRecipes(lines)
			p.TargetFiles["just"] = name
			break
		}
	}
	for _, name := range []string{"Taskfile.yml", "Taskfile.yaml", "taskfile.yml", "taskfile.yaml"} {
		if lines, ok := read(root, name); ok {
			p.Targets["task"] = taskfileTasks(lines)
			p.TargetFiles["task"] = name
			break
		}
	}
	for tool, list := range p.Targets {
		if len(list) == 0 {
			delete(p.Targets, tool)
			delete(p.TargetFiles, tool)
		}
	}
	for _, conf := range []string{"docs/conf.py", "doc/conf.py", "docs/source/conf.py", "doc/source/conf.py", "conf.py", "source/conf.py"} {
		if lines, ok := read(root, conf); ok {
			for _, l := range lines {
				if strings.Contains(l, "intersphinx_mapping") {
					p.Intersphinx = true
					break
				}
			}
			break
		}
	}
	return p
}

func read(root, name string) ([]string, bool) {
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return nil, false
	}
	return strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), true
}

// makeTargets returns the explicit targets of a Makefile: rule names
// before ":" (several per line allowed, pattern rules with "%" skipped) and
// the names listed after .PHONY:.
func makeTargets(lines []string) []string {
	set := map[string]bool{}
	for _, l := range lines {
		if l == "" || l[0] == '\t' || l[0] == '#' {
			continue
		}
		if strings.HasPrefix(l, ".PHONY:") {
			for _, t := range strings.Fields(strings.TrimPrefix(l, ".PHONY:")) {
				if t == "##" || strings.HasPrefix(t, "#") {
					break
				}
				set[t] = true
			}
			continue
		}
		m := reMakeTarget.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		for _, t := range strings.Fields(m[1]) {
			if strings.ContainsAny(t, "%$(") || strings.HasPrefix(t, ".") && strings.ToUpper(t) == t {
				continue // pattern rule, variable, .SUFFIXES-style special target
			}
			set[t] = true
		}
	}
	return sortedSet(set)
}

// justRecipes returns the recipe names of a justfile.
func justRecipes(lines []string) []string {
	set := map[string]bool{}
	for _, l := range lines {
		if l == "" || l[0] == ' ' || l[0] == '\t' || l[0] == '#' || l[0] == '[' {
			continue
		}
		if m := reJustRecipe.FindStringSubmatch(l); m != nil {
			set[m[1]] = true
		}
	}
	return sortedSet(set)
}

// taskfileTasks returns the keys under "tasks:" in a Taskfile.
func taskfileTasks(lines []string) []string {
	set := map[string]bool{}
	in := false
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "tasks:"):
			in = true
			continue
		case in && l != "" && l[0] != ' ' && l[0] != '#':
			in = false
		}
		if !in {
			continue
		}
		if m := reTaskKey.FindStringSubmatch(l); m != nil {
			set[m[1]] = true
		}
	}
	return sortedSet(set)
}

// pyproject returns the distribution name and the Python requirement from
// [project] (PEP 621) or [tool.poetry] / [tool.poetry.dependencies].
func pyproject(lines []string) (name, requires string) {
	sect := ""
	for _, raw := range lines {
		l := strings.TrimSpace(raw)
		if m := reTOMLSect.FindStringSubmatch(l); m != nil {
			sect = strings.TrimSpace(m[1])
			continue
		}
		k, v := tomlKV(l)
		if k == "" {
			continue
		}
		switch {
		case sect == "project" && k == "name" && name == "":
			name = v
		case sect == "project" && k == "requires-python" && requires == "":
			requires = v
		case sect == "tool.poetry" && k == "name" && name == "":
			name = v
		case sect == "tool.poetry.dependencies" && k == "python" && requires == "":
			requires = v
		}
	}
	return name, requires
}

func tomlKV(l string) (string, string) {
	if m := reTOMLKey.FindStringSubmatch(l); m != nil {
		return m[1], m[2]
	}
	if m := reTOMLKeyS.FindStringSubmatch(l); m != nil {
		return m[1], m[2]
	}
	return "", ""
}

// setupCfgName returns "name" from the [metadata] section of setup.cfg.
func setupCfgName(lines []string) string {
	sect := ""
	for _, raw := range lines {
		l := strings.TrimSpace(raw)
		if m := reTOMLSect.FindStringSubmatch(l); m != nil {
			sect = m[1]
			continue
		}
		if sect != "metadata" {
			continue
		}
		if m := reCfgKey.FindStringSubmatch(l); m != nil && m[1] == "name" {
			return strings.Trim(m[2], `"'`)
		}
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
