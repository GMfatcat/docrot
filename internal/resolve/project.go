package resolve

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"docrot/internal/fuzzy"
	"docrot/internal/index/defaults"
	"docrot/internal/model"
)

// Claims about the project's identity: install lines, toolchain
// requirements, task-runner targets. Each is only checked when the
// repository declares the corresponding fact (a go.mod, a pyproject name,
// a Makefile…); otherwise the claim is skipped.

func (r *Resolver) resolveTarget(ref model.Reference) Result {
	tool, name, ok := strings.Cut(ref.Norm, ":")
	if !ok {
		return Result{Skipped: true}
	}
	proj := r.ix.Project()
	targets := proj.Targets[tool]
	if len(targets) == 0 || r.nestedRunner(ref.Loc.File, tool) {
		return Result{Skipped: true} // no Makefile/justfile/… at the root, or a nested one the doc refers to
	}
	if proj.HasTarget(tool, name) {
		return Result{OK: true} // Makefile churn does not date a "run make build" sentence
	}
	var cands []string
	for _, c := range fuzzy.Rank(name, targets, 3, maxDist(name)) {
		cands = append(cands, tool+" "+c.Text)
	}
	msg := "`" + ref.Text + "`: no target `" + name + "` in " + proj.TargetFiles[tool]
	return Result{Finding: r.finding(model.RuleMissingTarget, model.SeverityFor(ref.Confidence), ref, msg, cands)}
}

func (r *Resolver) resolveInstall(ref model.Reference) Result {
	kind, name, ok := strings.Cut(ref.Norm, ":")
	if !ok {
		return Result{Skipped: true}
	}
	proj := r.ix.Project()
	sev := model.SeverityFor(ref.Confidence)
	switch kind {
	case "go":
		mod := proj.GoModule
		if mod == "" {
			return Result{Skipped: true}
		}
		if name == mod || strings.HasPrefix(name, mod+"/") {
			sub := strings.TrimPrefix(strings.TrimPrefix(name, mod), "/")
			if sub == "" || r.ix.DirExists(sub) {
				return Result{OK: true}
			}
			if _, ok := r.ix.GoPackageDir(name); ok {
				return Result{OK: true}
			}
			msg := "`" + ref.Text + "`: no package directory `" + sub + "` in this module"
			var cands []string
			for _, s := range r.ix.SimilarPaths(sub, 3) {
				if r.ix.DirExists(s) {
					cands = append(cands, mod+"/"+s)
				}
			}
			return Result{Finding: r.finding(model.RuleInstallMismatch, sev, ref, msg, cands)}
		}
		if !strings.EqualFold(lastSegment(name), lastSegment(mod)) && !strings.EqualFold(name, mod) {
			return Result{Skipped: true} // a dependency, not this project
		}
		msg := "`" + ref.Text + "` installs `" + name + "` but go.mod declares module `" + mod + "`"
		return Result{Finding: r.finding(model.RuleInstallMismatch, sev, ref, msg, []string{mod})}
	case "pip":
		want := proj.PyName
		if want == "" {
			return Result{Skipped: true}
		}
		got := pep503(name)
		if got == pep503(want) {
			return Result{OK: true}
		}
		if len(got) < 4 || fuzzy.Distance(got, pep503(want)) > 2 {
			return Result{Skipped: true} // another package
		}
		msg := "`" + ref.Text + "` installs `" + name + "` but pyproject.toml names the package `" + want + "`"
		return Result{Finding: r.finding(model.RuleInstallMismatch, sev, ref, msg, []string{want})}
	case "cargo":
		want := proj.CargoName
		if want == "" {
			return Result{Skipped: true}
		}
		got, wantID := strings.ReplaceAll(name, "-", "_"), strings.ReplaceAll(want, "-", "_")
		if got == wantID {
			return Result{OK: true}
		}
		if len(got) < 4 || fuzzy.Distance(got, wantID) > 2 {
			return Result{Skipped: true} // a dependency
		}
		msg := "`" + ref.Text + "` installs `" + name + "` but Cargo.toml names the package `" + want + "`"
		return Result{Finding: r.finding(model.RuleInstallMismatch, sev, ref, msg, []string{want})}
	case "dotnet":
		if len(proj.DotnetPackages) == 0 {
			return Result{Skipped: true}
		}
		best, bestDist := "", 99
		for _, want := range proj.DotnetPackages {
			if strings.EqualFold(name, want) {
				return Result{OK: true}
			}
			if d := fuzzy.Distance(strings.ToLower(name), strings.ToLower(want)); d < bestDist {
				best, bestDist = want, d
			}
		}
		if len(name) < 4 || bestDist > 2 {
			return Result{Skipped: true} // a dependency
		}
		msg := "`" + ref.Text + "` installs `" + name + "` but the project files name the package `" + best + "`"
		return Result{Finding: r.finding(model.RuleInstallMismatch, sev, ref, msg, []string{best})}
	case "npm":
		want := proj.NPMName
		if want == "" {
			return Result{Skipped: true}
		}
		if name == want {
			return Result{OK: true}
		}
		if !strings.EqualFold(lastSegment(name), lastSegment(want)) && fuzzy.Distance(name, want) > 2 {
			return Result{Skipped: true}
		}
		msg := "`" + ref.Text + "` installs `" + name + "` but package.json names the package `" + want + "`"
		return Result{Finding: r.finding(model.RuleInstallMismatch, sev, ref, msg, []string{want})}
	}
	return Result{Skipped: true}
}

// resolveDefault compares a documented default ("flag:port|8080") with
// the one the code declares. Names the code declares no default for are
// skipped.
func (r *Resolver) resolveDefault(ref model.Reference) Result {
	kindName, value, ok := strings.Cut(ref.Norm, "|")
	if !ok {
		return Result{Skipped: true}
	}
	kind, name, ok := strings.Cut(kindName, ":")
	if !ok {
		return Result{Skipped: true}
	}
	code, ok := r.ix.Default(kind, name)
	if !ok {
		return Result{Skipped: true}
	}
	if defaults.Same(value, code) {
		return Result{OK: true}
	}
	label := map[string]string{"flag": "flag `--" + name + "`", "key": "config key `" + name + "`", "env": "`" + name + "`"}[kind]
	shown := code
	if shown == "" {
		shown = "(empty)"
	}
	msg := "the document says " + label + " defaults to `" + value + "`, the code says `" + shown + "`"
	return Result{Finding: r.finding(model.RuleDefaultMismatch, model.SevWarning, ref, msg, []string{code})}
}

var reLowerBound = regexp.MustCompile(`(?:>=|\^|~=|~|==|^)\s*v?(\d+)(?:\.(\d+))?`)

func (r *Resolver) resolveToolchain(ref model.Reference) Result {
	tool, ver, ok := strings.Cut(ref.Norm, ":")
	if !ok {
		return Result{Skipped: true}
	}
	proj := r.ix.Project()
	var declared, source string
	switch tool {
	case "go":
		declared, source = proj.GoVersion, "go.mod"
	case "python":
		declared, source = proj.PyRequires, "pyproject.toml"
	case "rust":
		declared, source = proj.RustVersion, "Cargo.toml"
	case "node":
		declared, source = proj.NodeVersion, "package.json"
	case "dotnet":
		declared, source = proj.DotnetVersion, "the project files' TargetFramework"
	case "cmake":
		declared, source = proj.CMakeVersion, "CMakeLists.txt"
	}
	if declared == "" {
		return Result{Skipped: true}
	}
	dMaj, dMin, ok1 := parseMajorMinor(declared)
	cMaj, cMin, ok2 := parseMajorMinor(ver)
	if !ok1 || !ok2 {
		return Result{Skipped: true}
	}
	if dMaj == cMaj && dMin == cMin {
		return Result{OK: true} // go.mod churns with every dependency bump; not a staleness signal
	}
	label := map[string]string{"go": "Go", "python": "Python", "rust": "Rust", "node": "Node", "dotnet": ".NET", "cmake": "CMake"}[tool]
	need := strconv.Itoa(dMaj) + "." + strconv.Itoa(dMin)
	if !strings.Contains(declared, ".") || tool == "dotnet" && dMin == 0 {
		need = strconv.Itoa(dMaj) // engines.node ">=18", net8.0
	}
	var msg string
	sev := model.SevWarning
	if cMaj < dMaj || cMaj == dMaj && cMin < dMin {
		msg = "the document says " + label + " " + ver + " is enough, but " + source + " requires " + need
	} else {
		msg = "the document requires " + label + " " + ver + ", but " + source + " only asks for " + need
		sev = model.SevInfo
	}
	return Result{Finding: r.finding(model.RuleToolchain, sev, ref, msg, []string{need})}
}

// parseMajorMinor reads the lower bound of a version spec: "1.22", ">=3.10",
// "^3.9", "~=3.8", ">=3.9,<4".
func parseMajorMinor(spec string) (int, int, bool) {
	m := reLowerBound.FindStringSubmatch(strings.TrimSpace(spec))
	if m == nil {
		return 0, 0, false
	}
	maj, err1 := strconv.Atoi(m[1])
	if m[2] == "" {
		return maj, 0, err1 == nil // ">=18": a major alone
	}
	min, err2 := strconv.Atoi(m[2])
	return maj, min, err1 == nil && err2 == nil
}

// pep503 normalises a Python distribution name: lower-case, runs of
// "-", "_" and "." collapse to "-".
func pep503(name string) string {
	var b strings.Builder
	prevSep := false
	for _, c := range strings.ToLower(name) {
		if c == '-' || c == '_' || c == '.' {
			if !prevSep {
				b.WriteByte('-')
			}
			prevSep = true
			continue
		}
		prevSep = false
		b.WriteRune(c)
	}
	return b.String()
}

func lastSegment(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// runnerFiles are the definition files of each task runner.
var runnerFiles = map[string][]string{
	"make": {"Makefile", "GNUmakefile", "makefile"},
	"just": {"justfile", "Justfile", ".justfile"},
	"task": {"Taskfile.yml", "Taskfile.yaml", "taskfile.yml", "taskfile.yaml"},
	"npm":  {"package.json"},
}

// nestedRunner reports whether the document's own directory or an ancestor
// below the root has a definition file for tool: a monorepo package whose
// README means its own Makefile, which the project index does not read.
func (r *Resolver) nestedRunner(doc, tool string) bool {
	dir := path.Dir(doc)
	for dir != "." && dir != "/" && dir != "" {
		for _, f := range runnerFiles[tool] {
			if r.ix.FileExists(dir + "/" + f) {
				return true
			}
		}
		dir = path.Dir(dir)
	}
	return false
}
