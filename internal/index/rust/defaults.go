package rust

import (
	"regexp"
	"strings"
)

var (
	// #[arg(long, short, env = "PORT", default_value = "8080")], #[clap(...)], #[structopt(...)]
	reArgAttr = regexp.MustCompile(`^#\[(?:arg|clap|structopt)\((.*)$`)
	reField   = regexp.MustCompile(`^(?:pub(?:\([^)]*\))?\s+)?([a-z_][a-z0-9_]*)\s*:`)
	reLongEq  = regexp.MustCompile(`\blong\s*=\s*"([^"]+)"`)
	reLong    = regexp.MustCompile(`(?:^|[\s(,])long(?:\s*[,)]|$)`)
	reEnvEq   = regexp.MustCompile(`\benv\s*=\s*"([^"]+)"`)
	reEnvBare = regexp.MustCompile(`(?:^|[\s(,])env(?:\s*[,)]|$)`)
	reDefault = regexp.MustCompile(`\bdefault_value(?:_t|_os)?\s*=\s*([^,)]+)`)
	// builder API: Arg::new("port").long("port").env("PORT").default_value("8080")
	reBuildLong = regexp.MustCompile(`\.long\(\s*"([^"]+)"\s*\)`)
	reBuildEnv  = regexp.MustCompile(`\.env\(\s*"([^"]+)"\s*\)`)
	reBuildDef  = regexp.MustCompile(`\.default_value\(\s*([^)]+)\)`)
	// std::env::var("X"), env::var_os("X"), env!("X"), option_env!("X")
	reEnvRead = regexp.MustCompile(`\b(?:env::var(?:_os)?|env!|option_env!)\(\s*"([A-Z_][A-Z0-9_]*)"\s*\)(.*)$`)
	reUnwrap  = regexp.MustCompile(`\.unwrap_or(?:_else\(\s*\|_\|)?\(?\s*("[^"]*"|-?[0-9][0-9._]*|true|false)`)
)

// parseDefaults scans one file for clap option declarations (derive
// attributes and the builder API) and environment reads. It returns the
// literal defaults as (kind, name, value) triples, the long flag names and
// the environment variable names.
func parseDefaults(lines []string) (defs [][3]string, flags, envs []string) {
	var attr string // accumulated #[arg(...)] text waiting for its field
	inAttr := false
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if inAttr {
			attr += " " + line
			if strings.Contains(line, ")]") {
				inAttr = false
			}
			continue
		}
		if m := reArgAttr.FindStringSubmatch(line); m != nil {
			attr += " " + m[1]
			inAttr = !strings.Contains(line, ")]")
			continue
		}
		if attr != "" {
			if strings.HasPrefix(line, "#[") || strings.HasPrefix(line, "///") || line == "" {
				continue
			}
			if m := reField.FindStringSubmatch(line); m != nil {
				field := m[1]
				flag := ""
				if lm := reLongEq.FindStringSubmatch(attr); lm != nil {
					flag = lm[1]
				} else if reLong.MatchString(attr) {
					flag = strings.ReplaceAll(field, "_", "-")
				}
				env := ""
				if em := reEnvEq.FindStringSubmatch(attr); em != nil {
					env = em[1]
				} else if reEnvBare.MatchString(attr) {
					env = strings.ToUpper(field)
				}
				if flag != "" {
					flags = append(flags, flag)
				}
				if env != "" {
					envs = append(envs, env)
				}
				if dm := reDefault.FindStringSubmatch(attr); dm != nil {
					if v, ok := literalValue(dm[1]); ok {
						if flag != "" {
							defs = append(defs, [3]string{"flag", flag, v})
						}
						if env != "" {
							defs = append(defs, [3]string{"env", env, v})
						}
					}
				}
			}
			attr = ""
		}
		// builder API on one line
		if lm := reBuildLong.FindStringSubmatch(line); lm != nil {
			flags = append(flags, lm[1])
			if dm := reBuildDef.FindStringSubmatch(line); dm != nil {
				if v, ok := literalValue(dm[1]); ok {
					defs = append(defs, [3]string{"flag", lm[1], v})
				}
			}
		}
		if em := reBuildEnv.FindStringSubmatch(line); em != nil {
			envs = append(envs, em[1])
		}
		if m := reEnvRead.FindStringSubmatch(line); m != nil {
			envs = append(envs, m[1])
			if um := reUnwrap.FindStringSubmatch(m[2]); um != nil {
				if v, ok := literalValue(um[1]); ok {
					defs = append(defs, [3]string{"env", m[1], v})
				}
			}
		}
	}
	return defs, flags, envs
}

// literalValue accepts numbers, quoted strings (a trailing .to_string(),
// .into() or .to_owned() is dropped), true/false and nothing else.
func literalValue(s string) (string, bool) {
	s = strings.TrimSpace(s)
	for _, suffix := range []string{".to_string()", ".into()", ".to_owned()", ".to_vec()"} {
		s = strings.TrimSuffix(s, suffix)
	}
	s = strings.TrimSpace(s)
	switch s {
	case "true", "false":
		return s, true
	case "", "None":
		return "", false
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1], true
	}
	for i, c := range s {
		if !(c >= '0' && c <= '9' || c == '.' || c == '-' && i == 0 || c == '_') {
			return "", false
		}
	}
	return strings.ReplaceAll(s, "_", ""), true
}
