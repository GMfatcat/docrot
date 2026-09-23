package c

import (
	"regexp"
	"strings"
)

var (
	// getopt_long / curl-style option tables: {"verbose", no_argument, 0, 'v'}, {"verbose", ARG_BOOL, 'v', C_VERBOSE}
	reOptTable = regexp.MustCompile(`\{\s*"([a-z][a-z0-9-]*)"\s*,\s*(?:no_argument|required_argument|optional_argument|ARG_[A-Z_]+|[012])\b`)
	// CLI11: app.add_option("-f,--file", …), add_flag("--verbose,-v"); ->default_val(3); ->envname("X")
	reCLI11    = regexp.MustCompile(`\badd_(?:option|flag|option_function|flag_function|option_group)\(\s*"([^"]*)"(.*)$`)
	reLongFlag = regexp.MustCompile(`--([A-Za-z][A-Za-z0-9-]*)`)
	reDefVal   = regexp.MustCompile(`->default_val\(\s*([^)]+)\)`)
	reEnvName  = regexp.MustCompile(`->envname\(\s*"([A-Z_][A-Z0-9_]*)"\s*\)`)
	// cxxopts: ("v,verbose", "desc", cxxopts::value<bool>()->default_value("false"))
	reCxxopts = regexp.MustCompile(`\(\s*"(?:[A-Za-z],)?([a-z][a-z0-9-]+)"\s*,\s*"[^"]*"(.*)$`)
	reCxxDef  = regexp.MustCompile(`->default_value\(\s*"([^"]*)"\s*\)`)
	// getenv("X"), std::getenv("X"), curl_getenv("X"), secure_getenv("X")
	reGetenv = regexp.MustCompile(`\b(?:std::)?(?:secure_|curl_|_w?)?getenv\(\s*"([A-Z_][A-Z0-9_]*)"\s*\)`)
)

// parseDefaults scans one file for option tables, CLI11 and cxxopts
// declarations and getenv reads. It returns literal defaults as (kind,
// name, value) triples, the long flag names and the environment variable
// names.
func parseDefaults(lines []string) (defs [][3]string, flags, envs []string) {
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		for _, m := range reOptTable.FindAllStringSubmatch(line, -1) {
			flags = append(flags, m[1])
		}
		if m := reCLI11.FindStringSubmatch(line); m != nil {
			block := m[2]
			for j := i + 1; j < len(lines) && j <= i+4 && !strings.Contains(block, ";"); j++ {
				block += " " + strings.TrimSpace(lines[j])
			}
			for _, f := range reLongFlag.FindAllStringSubmatch(m[1], -1) {
				flags = append(flags, f[1])
				if d := reDefVal.FindStringSubmatch(block); d != nil {
					if v, ok := literalValue(d[1]); ok {
						defs = append(defs, [3]string{"flag", f[1], v})
					}
				}
			}
			if e := reEnvName.FindStringSubmatch(block); e != nil {
				envs = append(envs, e[1])
			}
		} else if m := reCxxopts.FindStringSubmatch(line); m != nil && strings.Contains(line, "cxxopts") || m != nil && strings.HasPrefix(strings.TrimSpace(line), "(\"") {
			flags = append(flags, m[1])
			if d := reCxxDef.FindStringSubmatch(m[2]); d != nil {
				defs = append(defs, [3]string{"flag", m[1], d[1]})
			}
		}
		for _, m := range reGetenv.FindAllStringSubmatch(line, -1) {
			envs = append(envs, m[1])
		}
	}
	return defs, flags, envs
}

// literalValue accepts numbers, quoted strings, true/false and nothing
// else.
func literalValue(s string) (string, bool) {
	s = strings.TrimSpace(s)
	switch s {
	case "true", "false":
		return s, true
	case "", "nullptr", "NULL":
		return "", false
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1], true
	}
	s = strings.TrimRight(s, "fFuUlL")
	for i, c := range s {
		if !(c >= '0' && c <= '9' || c == '.' || c == '-' && i == 0 || c == '\'') {
			return "", false
		}
	}
	return strings.ReplaceAll(s, "'", ""), true
}
