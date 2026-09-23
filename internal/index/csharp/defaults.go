package csharp

import (
	"regexp"
	"strings"
)

var (
	// new Option<int>("--port", …), new Option<string>(name: "--port"), new Option<int>(["--port", "-p"]), new Option<int>(new[] { "--port" })
	reOption  = regexp.MustCompile(`new\s+Option<[^>]*>\s*\(\s*(?:name:\s*)?(?:\[|new\s*(?:string)?\[\]\s*\{)?\s*"(--?[A-Za-z][A-Za-z0-9-]*)"(.*)$`)
	reDefault = regexp.MustCompile(`(?:getDefaultValue:\s*\(\)\s*=>|DefaultValueFactory\s*=\s*_?\w*\s*=>)\s*([^,)}]+)`)
	// Environment.GetEnvironmentVariable("X"), Configuration["X"] (an UPPER_SNAKE key), Environment.GetEnvironmentVariable("X") ?? "v"
	reEnvRead  = regexp.MustCompile(`(?:Environment\.GetEnvironmentVariable\(\s*"([A-Z_][A-Z0-9_]*)"\s*\)|Configuration\[\s*"([A-Z_][A-Z0-9_]*)"\s*\])(.*)$`)
	reCoalesce = regexp.MustCompile(`^\s*\?\?\s*("[^"]*"|-?\d+(?:\.\d+)?|true|false)`)
)

// parseDefaults scans one file for System.CommandLine option declarations
// and environment reads. It returns literal defaults as (kind, name,
// value) triples, the long flag names and the environment variable names.
func parseDefaults(lines []string) (defs [][3]string, flags, envs []string) {
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if m := reOption.FindStringSubmatch(line); m != nil {
			name := strings.TrimLeft(m[1], "-")
			if strings.HasPrefix(m[1], "--") {
				flags = append(flags, name)
			}
			block := m[2]
			for j := i + 1; j < len(lines) && j <= i+4 && !strings.Contains(block, ";"); j++ {
				block += " " + strings.TrimSpace(lines[j])
			}
			if d := reDefault.FindStringSubmatch(block); d != nil && strings.HasPrefix(m[1], "--") {
				if v, ok := literalValue(d[1]); ok {
					defs = append(defs, [3]string{"flag", name, v})
				}
			}
		}
		for _, m := range reEnvRead.FindAllStringSubmatch(line, -1) {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			envs = append(envs, name)
			if c := reCoalesce.FindStringSubmatch(m[3]); c != nil {
				if v, ok := literalValue(c[1]); ok {
					defs = append(defs, [3]string{"env", name, v})
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
	case "", "null", "default":
		return "", false
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1], true
	}
	s = strings.TrimRight(s, "fFdDmMlLuU")
	for i, c := range s {
		if !(c >= '0' && c <= '9' || c == '.' || c == '-' && i == 0 || c == '_') {
			return "", false
		}
	}
	return strings.ReplaceAll(s, "_", ""), true
}
