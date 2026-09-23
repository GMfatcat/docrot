package py

import (
	"regexp"
	"strings"
)

var (
	// port: int = typer.Option(8080, "--port"), name: str = typer.Argument("World")
	reTyperParam = regexp.MustCompile(`^\s*(\w+)\s*(?::[^=]+)?=\s*typer\.(?:Option|Argument)\(\s*([^,)]+)`)
	// @click.option("--port", default=8080, …), parser.add_argument("--port", …, default=8080)
	reClickOpt  = regexp.MustCompile(`(?:click\.option|add_argument)\(\s*['"](--?[\w-]+)['"](.*)$`)
	reDefaultKw = regexp.MustCompile(`default\s*=\s*([^,)]+)`)
	// os.getenv("NAME", "x"), os.environ.get("NAME", "x")
	reEnvDefault = regexp.MustCompile(`os\.(?:getenv|environ\.get)\(\s*['"]([A-Z_][A-Z0-9_]*)['"]\s*,\s*([^)]+)\)`)
)

// parseDefaults scans one file for literal defaults of CLI options
// (typer, click, argparse) and environment variables. Values that are not
// literals (default_factory, ..., a call) are skipped.
func parseDefaults(lines []string) [][3]string {
	var out [][3]string
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if m := reTyperParam.FindStringSubmatch(line); m != nil {
			if v, ok := literalValue(m[2]); ok {
				out = append(out, [3]string{"flag", strings.ReplaceAll(m[1], "_", "-"), v})
			}
		}
		if m := reClickOpt.FindStringSubmatch(line); m != nil {
			if d := reDefaultKw.FindStringSubmatch(m[2]); d != nil {
				if v, ok := literalValue(d[1]); ok {
					out = append(out, [3]string{"flag", m[1], v})
				}
			}
		}
		for _, m := range reEnvDefault.FindAllStringSubmatch(line, -1) {
			if v, ok := literalValue(m[2]); ok {
				out = append(out, [3]string{"env", m[1], v})
			}
		}
	}
	return out
}

// literalValue accepts numbers, quoted strings, True/False/None and
// nothing else.
func literalValue(s string) (string, bool) {
	s = strings.TrimSpace(s)
	switch s {
	case "True":
		return "true", true
	case "False":
		return "false", true
	case "None":
		return "", true
	case "", "...":
		return "", false
	}
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1], true
	}
	for i, c := range s {
		if !(c >= '0' && c <= '9' || c == '.' || c == '-' && i == 0 || c == '_') {
			return "", false
		}
	}
	return s, true
}
