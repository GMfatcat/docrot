package js

import (
	"regexp"
	"strings"
)

var (
	// commander: .option('-p, --port <number>', 'description', 8080)
	reCommanderOpt = regexp.MustCompile(`\.(?:option|requiredOption)\(\s*(['"])([^'"]*)['"]\s*(?:,\s*(['"])(?:[^'"]|\\['"])*['"])?\s*(?:,\s*([^,)]+))?`)
	reLongFlag     = regexp.MustCompile(`--([A-Za-z][A-Za-z0-9-]*)`)
	// yargs: .option('port', { default: 8080 }) / .option("port", {
	reYargsOpt   = regexp.MustCompile(`\.option\(\s*(['"])([A-Za-z][A-Za-z0-9-]*)['"]\s*,\s*\{(.*)$`)
	reDefaultKey = regexp.MustCompile(`\bdefault\s*:\s*([^,}]+)`)
	// process.env.X, process.env['X'], import.meta.env.X, Deno.env.get('X'), Bun.env.X
	reEnvRead  = regexp.MustCompile(`\b(?:process\.env\.|process\.env\[['"]|import\.meta\.env\.|Deno\.env\.get\(\s*['"]|Bun\.env\.)([A-Z_][A-Z0-9_]*)(?:['"]\s*\)?|\])?(.*)$`)
	reFallback = regexp.MustCompile(`^\s*(?:\|\||\?\?)\s*(['"][^'"]*['"]|-?\d+(?:\.\d+)?|true|false)`)
)

// parseDefaults scans one file for commander/yargs option declarations
// and environment reads. It returns literal defaults as (kind, name,
// value) triples, the long flag names and the environment variable names.
func parseDefaults(lines []string) (defs [][3]string, flags, envs []string) {
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if m := reCommanderOpt.FindStringSubmatch(line); m != nil {
			for _, f := range reLongFlag.FindAllStringSubmatch(m[2], -1) {
				flags = append(flags, f[1])
				if v, ok := literalValue(m[4]); ok {
					defs = append(defs, [3]string{"flag", f[1], v})
				}
			}
		}
		if m := reYargsOpt.FindStringSubmatch(line); m != nil {
			flags = append(flags, m[2])
			block := m[3]
			for j := i + 1; j < len(lines) && j <= i+6 && !strings.Contains(block, "}"); j++ {
				block += " " + strings.TrimSpace(lines[j])
			}
			if d := reDefaultKey.FindStringSubmatch(block); d != nil {
				if v, ok := literalValue(d[1]); ok {
					defs = append(defs, [3]string{"flag", m[2], v})
				}
			}
		}
		for _, m := range reEnvRead.FindAllStringSubmatch(line, -1) {
			envs = append(envs, m[1])
			if f := reFallback.FindStringSubmatch(m[2]); f != nil {
				if v, ok := literalValue(f[1]); ok {
					defs = append(defs, [3]string{"env", m[1], v})
				}
			}
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
	case "", "undefined", "null":
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
	return strings.ReplaceAll(s, "_", ""), true
}
