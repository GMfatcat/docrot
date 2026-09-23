package extract

import (
	"regexp"
	"strings"

	"docrot/internal/model"
)

// Install lines, toolchain requirements and task-runner targets: claims
// about the project's identity rather than about its code.

var (
	// "requires Go 1.21+", "Go 1.22 or later", "Python >= 3.9", "需要 Go 1.21 以上"
	reToolchain = regexp.MustCompile(`(?i)\b(go|golang|python|rust|rustc|node\.js|nodejs|node)\s*(?:>=|≥|version\s+)?\s*v?(\d+(?:\.\d+)?)(?:\.\d+)?\s*(\+|or\s+(?:later|newer|higher|above)|and\s+(?:later|newer|above)|以上|或更新|或以上)?`)
	// a negated sentence ("not supported on Python 3.14", "dropped Python 3.8")
	// is not a requirement of this project
	reNegated = regexp.MustCompile(`(?i)\b(not|no longer|n't|dropp?e?d?|removed?|unsupported|without|deprecated)\b|不支援|不再|移除|已停止`)
	// words that make a version mention a requirement rather than a remark
	reRequirement = regexp.MustCompile(`(?i)\b(requires?|required|requirements?|needs?|minimum|at least|or later|or newer|or higher|or above|supports?|supported)\b|需要|至少|以上|最低|須|支援|支持`)
	// pip install httpx[cli]==0.27 → httpx
	rePipName = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)`)
)

// installers maps a command line to the install kind and the position of
// the first package argument: "pip install X" → ("pip", after "install").
type installer struct {
	kind string
	sub  string // sub-command that must follow, "" for none
}

var installers = map[string]installer{
	"pip": {"pip", "install"}, "pip3": {"pip", "install"}, "pipx": {"pip", "install"},
	"uv": {"pip", ""}, "poetry": {"pip", "add"}, "conda": {"pip", "install"},
	"npm": {"npm", "install"}, "yarn": {"npm", "add"}, "pnpm": {"npm", "add"},
	"cargo": {"cargo", ""},
}

// runners maps a task runner to the sub-command that precedes the target.
var runners = map[string]string{"make": "", "just": "", "task": "", "npm": "run", "yarn": "run", "pnpm": "run"}

// projectRefs extracts install and target claims from one shell command
// line (already split into tokens of one segment).
func projectRefs(toks []string, conf model.Confidence) []model.Reference {
	if len(toks) < 2 {
		return nil
	}
	cmd := toks[0]
	var out []model.Reference
	add := func(kind model.Kind, text, norm string) {
		out = append(out, model.Reference{Kind: kind, Text: text, Norm: norm, Confidence: conf})
	}
	// go get / go install
	if cmd == "go" && (toks[1] == "get" || toks[1] == "install") {
		for _, t := range toks[2:] {
			if strings.HasPrefix(t, "-") || t == "." || strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../") {
				continue
			}
			p := strings.TrimSuffix(strings.SplitN(t, "@", 2)[0], "/...")
			if strings.Contains(p, ".") && strings.Contains(p, "/") {
				add(model.KindInstall, t, "go:"+p)
			}
		}
		return out
	}
	// task runners: make X, npm run X, just X, task X
	if sub, ok := runners[cmd]; ok {
		args := toks[1:]
		if sub != "" {
			if len(args) < 2 || args[0] != sub {
				args = nil
			} else {
				args = args[1:]
			}
		}
		for _, t := range args {
			if t == "--" || strings.HasPrefix(t, "#") {
				break // end of options, or a trailing comment
			}
			if strings.HasPrefix(t, "-") || strings.Contains(t, "=") || strings.ContainsAny(t, "$<>|&;") {
				continue
			}
			tool := cmd
			if cmd == "yarn" || cmd == "pnpm" {
				tool = "npm"
			}
			add(model.KindTarget, cmd+" "+t, tool+":"+t)
			if cmd != "make" {
				break // one target per invocation; make takes several
			}
		}
		if _, alsoInstalls := installers[cmd]; !alsoInstalls || len(out) > 0 {
			return out
		}
	}
	// package installers
	ins, ok := installers[cmd]
	if !ok {
		return out
	}
	args := toks[1:]
	if cmd == "uv" {
		// uv pip install X / uv add X
		switch {
		case len(args) >= 3 && args[0] == "pip" && args[1] == "install":
			args = args[2:]
		case len(args) >= 2 && args[0] == "add":
			args = args[1:]
		default:
			return out
		}
	} else if cmd == "npm" && len(args) >= 2 && (args[0] == "i" || args[0] == "install" || args[0] == "add") {
		args = args[1:]
	} else if cmd == "cargo" {
		// cargo add X / cargo install X; "cargo build" and friends are not installs
		if len(args) < 2 || args[0] != "add" && args[0] != "install" {
			return out
		}
		args = args[1:]
	} else if ins.sub != "" {
		if len(args) < 2 || args[0] != ins.sub {
			return out
		}
		args = args[1:]
	}
	skipNext := false
	for _, t := range args {
		if skipNext {
			skipNext = false
			continue
		}
		scoped := ins.kind == "npm" && strings.HasPrefix(t, "@") && strings.Count(t, "/") == 1
		switch {
		case t == "-r" || t == "--requirement" || t == "-e" || t == "--editable" || t == "-c" || t == "--constraint" || t == "-i" || t == "--index-url" || t == "-t" || t == "--target":
			skipNext = true
			continue
		case strings.HasPrefix(t, "-"), t == ".", strings.Contains(t, "://"), strings.HasPrefix(t, "git+"), strings.Contains(t, "/") && !scoped:
			continue
		}
		name := strings.Trim(t, `"'`)
		if ins.kind == "cargo" {
			// cargo add serde@1: the name is the first token; the rest are dependencies
			if i := strings.IndexAny(name, "@="); i > 0 {
				name = name[:i]
			}
			if m := rePipName.FindStringSubmatch(name); m != nil {
				add(model.KindInstall, t, "cargo:"+m[1])
			}
			break
		}
		if ins.kind == "npm" {
			if i := strings.LastIndex(name, "@"); i > 0 {
				name = name[:i] // version suffix, keeps @scope/
			}
			if name != "" {
				add(model.KindInstall, t, "npm:"+name)
			}
			continue
		}
		if m := rePipName.FindStringSubmatch(name); m != nil {
			add(model.KindInstall, t, "pip:"+m[1])
		}
	}
	return out
}

// toolchainRefs scans prose lines for version requirements.
func (x *extractor) toolchainRefs() {
	inFence := make([]bool, len(x.doc.Lines)+1)
	for _, f := range x.doc.Fences {
		for i := f.StartLine; i <= f.EndLine && i < len(inFence); i++ {
			inFence[i] = true
		}
	}
	for i, line := range x.doc.Lines {
		ln := i + 1
		if inFence[ln] || x.ignored(ln) || !reRequirement.MatchString(line) {
			continue
		}
		// judge each sentence on its own: "requires Go 1.21; uses Go 1.22
		// wildcards" claims 1.21 only
		for _, sentence := range sentences(line) {
			if !reRequirement.MatchString(sentence) || reNegated.MatchString(sentence) {
				continue
			}
			for _, m := range reToolchain.FindAllStringSubmatch(sentence, -1) {
				tool := strings.ToLower(m[1])
				switch tool {
				case "golang":
					tool = "go"
				case "rustc":
					tool = "rust"
				case "node.js", "nodejs":
					tool = "node"
				}
				r := model.Reference{Kind: model.KindToolchain, Text: strings.TrimSpace(m[0]), Norm: tool + ":" + m[2], Confidence: model.Medium}
				x.emit(r, ln, 0, x.doc.SectionAt(ln), "")
			}
		}
	}
}

// sentences splits a line at sentence punctuation: ";", "!", "?", the CJK
// full stops, and a "." that is followed by a space or ends the line (so
// "1.21" survives).
func sentences(line string) []string {
	var out []string
	start := 0
	for i, r := range line {
		switch r {
		case ';', '!', '?', '。', '；', '！', '？':
			out = append(out, line[start:i])
			start = i + len(string(r))
		case '.':
			if i+1 == len(line) || line[i+1] == ' ' {
				out = append(out, line[start:i])
				start = i + 1
			}
		}
	}
	if start < len(line) {
		out = append(out, line[start:])
	}
	return out
}
