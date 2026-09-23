package extract

import (
	"regexp"
	"strings"

	"docrot/internal/model"
)

var shellLangs = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "shell": true, "console": true, "powershell": true,
	"ps1": true, "pwsh": true, "cmd": true, "bat": true, "batch": true, "text": true, "": true,
	"terminal": true, "fish": true, "dos": true,
}

var goSubcommands = map[string]bool{
	"run": true, "test": true, "build": true, "vet": true, "install": true, "generate": true,
	"fmt": true, "doc": true, "list": true,
}

var interpreters = map[string]bool{
	"python": true, "python3": true, "py": true, "node": true, "bash": true, "sh": true,
	"pwsh": true, "powershell": true, "source": true, "deno": true, "bun": true, "ruby": true, "perl": true,
}

var rePrompt = regexp.MustCompile(`^(?:\$|>|#|PS[^>]*>|[A-Za-z]:\\[^>]*>|\S+@\S+:\S+\$|\S+\s?%)\s+`)

// outputSinks are tokens after which the next token names an output, not
// an input that must exist.
var outputSinks = map[string]bool{
	">": true, ">>": true, "-o": true, "-out": true, "--out": true, "--output": true,
	"-output": true, "-coverprofile": true, "-cpuprofile": true, "-memprofile": true,
	"-f": true, "-F": true, "--file": true, "-out:": true, "-C": true, "--directory": true,
	"--config": true, "-config": true, "--baseline": true, "-baseline": true,
}

// plainLangs are block languages where only prompt-prefixed lines ("$ cmd")
// are commands; the rest is arbitrary text.
var plainLangs = map[string]bool{"": true, "text": true, "txt": true, "plaintext": true, "console": true, "terminal": true, "output": true}

// shellRefs extracts command/path references from one line of a shell block.
// When promptOnly is set, lines without a shell prompt are ignored.
func (x *extractor) shellRefs(line string, promptOnly bool) []model.Reference {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "REM ") {
		return nil
	}
	if strings.HasPrefix(line, "Write-Host") || strings.HasPrefix(line, "echo ") {
		return nil
	}
	hadPrompt := false
	if m := rePrompt.FindString(line); m != "" {
		line = line[len(m):]
		hadPrompt = true
	} else if strings.HasPrefix(line, "$ ") || strings.HasPrefix(line, "> ") {
		line = line[2:]
		hadPrompt = true
	}
	if promptOnly && !hadPrompt {
		return nil
	}
	var out []model.Reference
	for _, seg := range splitSegments(line) {
		toks := strings.Fields(seg)
		if len(toks) == 0 {
			continue
		}
		cmd := toks[0]
		out = append(out, projectRefs(toks, model.High)...) // go get, pip install, make target
		switch {
		case isExplicitPath(cmd):
			if r := x.pathRef(cmd, false); r != nil {
				r.Kind, r.Confidence = model.KindCommand, model.High
				out = append(out, *r)
			}
		case cmd == "go" && len(toks) >= 3 && goSubcommands[toks[1]]:
			for i := 2; i < len(toks); i++ {
				t := toks[i]
				if strings.HasPrefix(t, "-") || outputSinks[toks[i-1]] {
					continue
				}
				if isExplicitPath(t) || t == "." {
					if t == "." || t == "./..." {
						continue
					}
					if r := x.pathRef(t, false); r != nil {
						r.Kind, r.Confidence = model.KindCommand, model.High
						out = append(out, *r)
					}
				}
				break // only the first package/path argument
			}
		case cmd == "odin" && len(toks) >= 3 && (toks[1] == "build" || toks[1] == "run" || toks[1] == "test" || toks[1] == "check"):
			t := toks[2]
			if !strings.HasPrefix(t, "-") && t != "." {
				if r := x.pathRef(t, false); r != nil {
					r.Kind, r.Confidence = model.KindCommand, model.High
					out = append(out, *r)
				} else if isIdent(t) {
					out = append(out, model.Reference{Kind: model.KindCommand, Text: t, Norm: t, Confidence: model.High})
				}
			}
		case interpreters[cmd] && len(toks) >= 2:
			for _, t := range toks[1:] {
				if strings.HasPrefix(t, "-") && t != "-File" {
					continue
				}
				if t == "-File" {
					continue
				}
				if r := x.pathRef(t, false); r != nil {
					r.Kind, r.Confidence = model.KindCommand, model.High
					out = append(out, *r)
				}
				break
			}
		case (cmd == "cp" || cmd == "mv" || cmd == "rsync" || cmd == "robocopy" || cmd == "Copy-Item" || cmd == "Move-Item" || cmd == "xcopy") && len(toks) >= 3:
			// sources must exist; the destination is an output
			for _, t := range toks[1 : len(toks)-1] {
				if strings.HasPrefix(t, "-") || !isExplicitPath(t) {
					continue
				}
				if r := x.pathRef(t, false); r != nil {
					r.Kind, r.Confidence = model.KindCommand, model.Medium
					out = append(out, *r)
				}
			}
			continue
		case cmd == "cd" && len(toks) >= 2:
			if r := x.pathRef(toks[1], false); r != nil {
				r.Kind, r.Confidence = model.KindCommand, model.Medium
				out = append(out, *r)
			}
		}
		// any other ./x argument, unless it names an output
		for i := 1; i < len(toks); i++ {
			t := toks[i]
			if outputSinks[toks[i-1]] || !isDotPath(t) {
				continue
			}
			if i == 1 && (cmd == "cd" || interpreters[cmd] || cmd == "odin") {
				continue
			}
			if r := x.pathRef(t, false); r != nil {
				r.Kind, r.Confidence = model.KindCommand, model.Medium
				out = append(out, *r)
			}
		}
	}
	return out
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// isDotPath reports whether a token starts with ./, ../ or .\ .
func isDotPath(t string) bool {
	return strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../") || strings.HasPrefix(t, `.\`)
}

// isExplicitPath reports whether a shell token is written as a path
// (./x, ../x, .\x, x/y) as opposed to a bare command name.
func isExplicitPath(t string) bool {
	if strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../") || strings.HasPrefix(t, `.\`) {
		return true
	}
	if strings.ContainsAny(t, "<>$%{}|\"'`") {
		return false
	}
	return strings.Contains(t, "/") && !strings.Contains(t, "://") && !strings.HasPrefix(t, "/")
}

// splitSegments splits a shell line on &&, ||, |, ; keeping order.
func splitSegments(line string) []string {
	repl := strings.NewReplacer("&&", "\x00", "||", "\x00", "|", "\x00", ";", "\x00")
	return strings.Split(repl.Replace(line), "\x00")
}

// goFenceRefs extracts import paths and package-qualified symbols from a Go
// code block.
func (x *extractor) goFenceRefs(lines []string) []model.Reference {
	var out []model.Reference
	seen := map[string]bool{}
	mp := x.hints.ModulePath()
	inImport := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "//") {
			continue
		}
		line = stripLineComment(line)
		if strings.HasPrefix(line, "import (") {
			inImport = true
			continue
		}
		if inImport && line == ")" {
			inImport = false
			continue
		}
		if inImport || strings.HasPrefix(line, "import ") {
			if m := reGoImport.FindStringSubmatch(line); m != nil {
				ip := m[1]
				if mp != "" && (ip == mp || strings.HasPrefix(ip, mp+"/")) && !seen["i|"+ip] {
					seen["i|"+ip] = true
					out = append(out, model.Reference{Kind: model.KindImport, Text: ip, Norm: ip, Confidence: model.High})
				}
			}
			continue
		}
		for _, m := range reGoSym.FindAllStringSubmatch(line, -1) {
			pkg, name := m[1], m[2]
			if !x.hints.IsGoPackage(pkg) {
				continue
			}
			q := pkg + "." + name
			if seen["s|"+q] {
				continue
			}
			seen["s|"+q] = true
			out = append(out, model.Reference{Kind: model.KindGoSymbol, Text: q, Norm: q, Confidence: model.High})
		}
	}
	return out
}

// stripLineComment removes a trailing // comment that is not inside a
// string literal.
func stripLineComment(line string) string {
	inStr := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inStr != 0:
			if c == '\\' {
				i++
			} else if c == inStr {
				inStr = 0
			}
		case c == '"' || c == '`' || c == '\'':
			inStr = c
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			return strings.TrimSpace(line[:i])
		}
	}
	return line
}
