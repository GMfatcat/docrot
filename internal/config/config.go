// Package config loads and validates docrot's `.docrot.json` settings
// file. Loading always starts from Default() and overlays whatever the
// file sets, so a file only has to mention what it changes.
//
// Unknown fields are rejected — a typo in a config file is a bug the user
// wants to hear about — and JSON errors are reported with a line number,
// as `path:line: message`.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"docrot/internal/globx"
)

// FileNames are the config file names Find looks for, in order.
var FileNames = []string{".docrot.json", "docrot.json"}

// Pair explicitly ties a source document to its translation.
type Pair struct {
	Source      string `json:"source"`
	Translation string `json:"translation"`
}

// Stale configures the git-history based staleness scoring.
type Stale struct {
	Enabled  bool `json:"enabled"`
	MinChurn int  `json:"minChurn"`
	MinDays  int  `json:"minDays"`
	// Exclude are doc globs never analysed for staleness (changelogs,
	// dated specs and plans are historical records by nature).
	Exclude []string `json:"exclude"`
}

// Coverage configures the reverse check: code that no document mentions.
type Coverage struct {
	Report          bool `json:"report"`
	IncludeInternal bool `json:"includeInternal"`
}

// Config is the whole of `.docrot.json`.
type Config struct {
	// Docs are globs selecting the documents to check.
	Docs []string `json:"docs"`
	// Exclude are globs of paths never walked or indexed.
	Exclude []string `json:"exclude"`
	// Ignore are regular expressions applied to Reference.Text; a match
	// suppresses the reference.
	Ignore []string `json:"ignore"`
	// Siblings are other repositories (relative to root or absolute) in
	// which a path that is missing here may legitimately live, e.g. a
	// library this repo documents alongside its own code.
	Siblings []string `json:"siblings"`
	// Pairs are explicit source/translation document pairs.
	Pairs []Pair `json:"pairs"`
	// PairPatterns derive translations from a source name; "{stem}" is
	// replaced by the source file name without its extension.
	PairPatterns []string `json:"pairPatterns"`
	// ConfigSamples are globs of JSON files to mine for config keys.
	ConfigSamples []string `json:"configSamples"`
	Stale         Stale    `json:"stale"`
	Coverage      Coverage `json:"coverage"`
	// Severity overrides the default severity of a rule, by rule name.
	Severity map[string]string `json:"severity"`
	// Net enables external URL checking.
	Net bool `json:"net"`
	// FailOn is the lowest severity that makes `docrot check` exit 1:
	// "error", "warning", "info" or "none".
	FailOn string `json:"failOn"`
	// MinConfidence drops references below this confidence:
	// "low", "medium" or "high".
	MinConfidence string `json:"minConfidence"`
	// OutDir is the directory, relative to the repo root, that every
	// `docrot check` run rewrites with the report in every format.
	// Empty means "write nothing".
	OutDir string `json:"outDir"`
}

// DefaultOutDir is the directory `docrot check` rewrites unless the config
// or --out-dir says otherwise.
const DefaultOutDir = ".docrot"

// Default returns the settings docrot uses when there is no config file.
func Default() Config {
	return Config{
		Docs:     []string{"**/*.md", "llms.txt"},
		Exclude:  []string{"vendor/**", "node_modules/**", "third_party/**", "3rdparty/**", "external/**", "**/testdata/**", "dist/**", ".git/**", ".*/**"},
		Ignore:   []string{},
		Siblings: []string{},
		Pairs:    []Pair{},
		PairPatterns: []string{
			"{stem}-zh.md", "{stem}_zh.md", "{stem}.zh.md", "{stem}.zh-TW.md", "{stem}-zh-TW.md",
		},
		ConfigSamples: []string{
			"config.json", "config*.json", "*.example.json", "*.sample.json", "configs/**/*.json",
		},
		Stale: Stale{Enabled: true, MinChurn: 3, MinDays: 90, Exclude: []string{
			"CHANGELOG*.md", "CHANGES*.md", "HISTORY*.md", "**/superpowers/**", "**/specs/**", "**/plans/**", "**/*-report.md", "**/adr/**",
		}},
		Coverage: Coverage{Report: false, IncludeInternal: false},
		Severity: map[string]string{
			"stale-section": "warning",
			"pair-lag":      "warning",
			"pair-number":   "info",
		},
		Net:           false,
		FailOn:        "error",
		MinConfidence: "low",
		OutDir:        DefaultOutDir,
	}
}

// Find looks for a config file directly inside root and reports the path
// of the first one that exists.
func Find(root string) (string, bool) {
	if root == "" {
		root = "."
	}
	for _, name := range FileNames {
		p := filepath.Join(root, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true
		}
	}
	return "", false
}

// Load reads path and overlays it on Default(). The returned Config is
// not validated; call Validate separately so callers can apply CLI
// overrides first.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Default(), err
	}
	return Parse(path, data)
}

// LoadOrDefault returns Load(path) when a config file exists in root, and
// Default() (with ok=false) when none does.
func LoadOrDefault(root string) (cfg Config, path string, err error) {
	p, ok := Find(root)
	if !ok {
		return Default(), "", nil
	}
	cfg, err = Load(p)
	return cfg, p, err
}

// Parse overlays JSON data on Default(). name is only used in error
// messages; it is normally the file the data came from.
func Parse(name string, data []byte) (Config, error) {
	cfg := Default()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Default(), decodeError(name, data, dec.InputOffset(), err)
	}
	// Reject trailing content after the object.
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Default(), fmt.Errorf("%s:%d: unexpected trailing data after the config object",
			name, lineOf(data, dec.InputOffset()))
	}
	return cfg, nil
}

// decodeError turns a json error into `name:line: message`.
func decodeError(name string, data []byte, offset int64, err error) error {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		return fmt.Errorf("%s:%d: %s", name, lineOf(data, syn.Offset), syn.Error())
	case errors.As(err, &typ):
		field := typ.Field
		if field == "" {
			field = "value"
		}
		return fmt.Errorf("%s:%d: field %q must be %s, not %s",
			name, lineOf(data, typ.Offset), field, typ.Type, typ.Value)
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return fmt.Errorf("%s: unexpected end of file: the config is not a complete JSON object", name)
	}
	msg := err.Error()
	if f, ok := unknownField(msg); ok {
		// The decoder has already consumed the value, so its offset points
		// past the key; look the key up instead for a useful line number.
		at := keyOffset(data, f)
		if at < 0 {
			at = offset
		}
		return fmt.Errorf("%s:%d: unknown field %q (see docs for the accepted settings)",
			name, lineOf(data, at), f)
	}
	return fmt.Errorf("%s:%d: %s", name, lineOf(data, offset), msg)
}

var unknownFieldRe = regexp.MustCompile(`unknown field "([^"]*)"`)

func unknownField(msg string) (string, bool) {
	m := unknownFieldRe.FindStringSubmatch(msg)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// keyOffset finds the byte offset of the JSON object key `"field"` in
// data, or -1 when it is not there.
func keyOffset(data []byte, field string) int64 {
	quoted := []byte(`"` + field + `"`)
	from := 0
	for {
		i := bytes.Index(data[from:], quoted)
		if i < 0 {
			return -1
		}
		i += from
		rest := bytes.TrimLeft(data[i+len(quoted):], " \t\r\n")
		if len(rest) > 0 && rest[0] == ':' {
			return int64(i)
		}
		from = i + len(quoted)
	}
}

// lineOf converts a byte offset into a 1-based line number.
func lineOf(data []byte, offset int64) int {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	line := 1
	for _, b := range data[:offset] {
		if b == '\n' {
			line++
		}
	}
	return line
}

// Severity and confidence values accepted by Validate.
var (
	validSeverity      = []string{"error", "warning", "info"}
	validFailOn        = []string{"error", "warning", "info", "none"}
	validMinConfidence = []string{"low", "medium", "high"}
)

// Validate reports the first problem it finds in c. Empty failOn means
// "none" and empty minConfidence means "low".
func (c Config) Validate() error {
	globFields := []struct {
		name string
		pats []string
	}{
		{"docs", c.Docs},
		{"exclude", c.Exclude},
		{"configSamples", c.ConfigSamples},
	}
	for _, f := range globFields {
		for _, pat := range f.pats {
			if strings.TrimSpace(pat) == "" {
				return fmt.Errorf("%s: empty pattern", f.name)
			}
			if _, err := globx.Compile(pat); err != nil {
				return fmt.Errorf("%s: %v", f.name, err)
			}
		}
	}
	for _, p := range c.PairPatterns {
		if strings.TrimSpace(p) == "" {
			return errors.New("pairPatterns: empty pattern")
		}
		if !strings.Contains(p, "{stem}") {
			return fmt.Errorf("pairPatterns: %q must contain {stem}", p)
		}
	}
	for i, p := range c.Pairs {
		if strings.TrimSpace(p.Source) == "" || strings.TrimSpace(p.Translation) == "" {
			return fmt.Errorf("pairs[%d]: both source and translation are required", i)
		}
	}
	if _, err := c.IgnoreRegexps(); err != nil {
		return err
	}
	for rule, sev := range c.Severity {
		if strings.TrimSpace(rule) == "" {
			return errors.New("severity: empty rule name")
		}
		if !oneOf(sev, validSeverity) {
			return fmt.Errorf("severity[%q]: %q is not one of %s", rule, sev, strings.Join(validSeverity, ", "))
		}
	}
	if c.FailOn != "" && !oneOf(c.FailOn, validFailOn) {
		return fmt.Errorf("failOn: %q is not one of %s", c.FailOn, strings.Join(validFailOn, ", "))
	}
	if c.MinConfidence != "" && !oneOf(c.MinConfidence, validMinConfidence) {
		return fmt.Errorf("minConfidence: %q is not one of %s", c.MinConfidence, strings.Join(validMinConfidence, ", "))
	}
	if c.Stale.MinChurn < 0 {
		return fmt.Errorf("stale.minChurn: %d must not be negative", c.Stale.MinChurn)
	}
	if c.Stale.MinDays < 0 {
		return fmt.Errorf("stale.minDays: %d must not be negative", c.Stale.MinDays)
	}
	return nil
}

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// IgnoreRegexps compiles the ignore patterns, reporting the first that
// does not compile.
func (c Config) IgnoreRegexps() ([]*regexp.Regexp, error) {
	if len(c.Ignore) == 0 {
		return nil, nil
	}
	out := make([]*regexp.Regexp, 0, len(c.Ignore))
	for i, pat := range c.Ignore {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("ignore[%d]: %v", i, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// ExcludePatterns compiles the exclude globs for reuse by the indexers.
func (c Config) ExcludePatterns() ([]*globx.Pattern, error) {
	ps, err := globx.CompileAll(c.Exclude)
	if err != nil {
		return nil, fmt.Errorf("exclude: %v", err)
	}
	return ps, nil
}

// SeverityFor returns the configured severity override for a rule and
// whether one was set.
func (c Config) SeverityFor(rule string) (string, bool) {
	s, ok := c.Severity[rule]
	return s, ok
}

// Marshal renders c as the pretty-printed JSON docrot writes to disk.
func (c Config) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// WriteDefault writes a pretty-printed default config to path. It refuses
// to overwrite an existing file.
func WriteDefault(path string) error {
	return Write(path, Default())
}

// Write writes cfg to path, refusing to overwrite an existing file.
func Write(path string, cfg Config) error {
	b, err := cfg.Marshal()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists: refusing to overwrite it", path)
		}
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
