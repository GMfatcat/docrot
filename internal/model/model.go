// Package model holds the shared contract between every docrot stage:
// references extracted from documents, findings produced by resolvers,
// and the Index interface that resolvers query.
//
// Nothing in this package depends on any other docrot package.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Kind classifies what a Reference points at.
type Kind string

const (
	KindPath      Kind = "path"      // file or directory path (may contain glob)
	KindGoSymbol  Kind = "gosym"     // pkg.Name / pkg.Type.Method / Type.Method / Name()
	KindOdinSym   Kind = "odinsym"   // Odin package.proc / proc
	KindPySym     Kind = "pysym"     // Python module.func / Class.method / name
	KindFlag      Kind = "flag"      // --name / -name
	KindEnv       Kind = "env"       // UPPER_SNAKE
	KindConfigKey Kind = "configkey" // dotted lower-case key path
	KindAnchor    Kind = "anchor"    // file.md#slug or #slug
	KindURL       Kind = "url"       // http(s)://
	KindCommand   Kind = "command"   // executable path in a shell block
	KindImport    Kind = "import"    // Go import path in a go block
	KindRoute     Kind = "route"     // HTTP route: "GET /v1/items" or "/healthz"
	KindInstall   Kind = "install"   // install line: "go:example.com/x", "pip:httpx", "npm:@acme/x"
	KindToolchain Kind = "toolchain" // version requirement in prose: "go:1.21", "python:3.9"
	KindTarget    Kind = "target"    // task-runner target: "make:build", "npm:lint", "just:x", "task:x"
)

// AllKinds lists every Kind in a stable order.
var AllKinds = []Kind{
	KindPath, KindGoSymbol, KindOdinSym, KindPySym, KindFlag, KindEnv,
	KindConfigKey, KindAnchor, KindURL, KindCommand, KindImport, KindRoute,
	KindInstall, KindToolchain, KindTarget,
}

// Confidence expresses how sure the extractor is that a piece of text
// really is a reference of the assigned Kind.
type Confidence int

const (
	Low    Confidence = 1
	Medium Confidence = 2
	High   Confidence = 3
)

func (c Confidence) String() string {
	switch c {
	case Low:
		return "low"
	case Medium:
		return "medium"
	case High:
		return "high"
	}
	return fmt.Sprintf("confidence(%d)", int(c))
}

// MarshalText makes Confidence serialise as "low"/"medium"/"high".
func (c Confidence) MarshalText() ([]byte, error) { return []byte(c.String()), nil }

// UnmarshalText accepts "low"/"medium"/"high" (case-insensitive).
func (c *Confidence) UnmarshalText(b []byte) error {
	v, ok := ParseConfidence(string(b))
	if !ok {
		return fmt.Errorf("invalid confidence %q", string(b))
	}
	*c = v
	return nil
}

// ParseConfidence parses "low" | "medium" | "high".
func ParseConfidence(s string) (Confidence, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return Low, true
	case "medium", "med":
		return Medium, true
	case "high":
		return High, true
	}
	return 0, false
}

// Severity of a Finding.
type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
	SevInfo    Severity = "info"
)

// Rank orders severities: error > warning > info. Unknown/empty → 0.
func (s Severity) Rank() int {
	switch s {
	case SevError:
		return 3
	case SevWarning:
		return 2
	case SevInfo:
		return 1
	}
	return 0
}

// ParseSeverity parses "error" | "warning" | "info" | "none".
// "none" (or empty) returns ("", true), meaning "never fail".
func ParseSeverity(s string) (Severity, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error":
		return SevError, true
	case "warning", "warn":
		return SevWarning, true
	case "info":
		return SevInfo, true
	case "none", "":
		return "", true
	}
	return "", false
}

// SeverityFor maps confidence to the default severity of a missing-X rule.
func SeverityFor(c Confidence) Severity {
	switch c {
	case High:
		return SevError
	case Medium:
		return SevWarning
	}
	return SevInfo
}

// Location is a position inside a document. File is relative to the repo
// root and always uses forward slashes. Line and Col are 1-based; Col is a
// byte offset. Line==0 means "whole file".
type Location struct {
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
}

func (l Location) String() string {
	switch {
	case l.Line == 0:
		return l.File
	case l.Col == 0:
		return fmt.Sprintf("%s:%d", l.File, l.Line)
	}
	return fmt.Sprintf("%s:%d:%d", l.File, l.Line, l.Col)
}

// Reference is one thing a document claims exists.
type Reference struct {
	Kind       Kind       `json:"kind"`
	Text       string     `json:"text"` // original text, backticks stripped
	Norm       string     `json:"norm"` // normalized lookup key
	Confidence Confidence `json:"confidence"`
	Loc        Location   `json:"loc"`
	Section    string     `json:"section,omitempty"` // nearest heading text ("" = before first heading)
	Context    string     `json:"context,omitempty"` // source line, trimmed
	// Lang is the fenced-block language the reference came from, if any
	// (e.g. "go", "sh"). Empty for spans/links/prose.
	Lang string `json:"lang,omitempty"`
}

// Finding is one problem docrot wants a human to look at.
type Finding struct {
	Rule        string         `json:"rule"`
	Severity    Severity       `json:"severity"`
	Message     string         `json:"message"`
	Loc         Location       `json:"loc"`
	Ref         *Reference     `json:"ref,omitempty"`
	Suggestion  string         `json:"suggestion,omitempty"`
	Fingerprint string         `json:"fingerprint"`
	Baselined   bool           `json:"baselined,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
}

// Rule names. Keep in sync with docs/rules.md.
const (
	RuleMissingPath      = "missing-path"
	RuleMissingSymbol    = "missing-symbol"
	RuleUnknownFlag      = "unknown-flag"
	RuleUnknownEnv       = "unknown-env"
	RuleUnknownConfigKey = "unknown-config-key"
	RuleBrokenAnchor     = "broken-anchor"
	RuleBrokenURL        = "broken-url"
	RuleMissingCommand   = "missing-command"
	RuleMissingImport    = "missing-import"
	RuleStaleSection     = "stale-section"
	RulePairHeading      = "pair-heading"
	RulePairCode         = "pair-code"
	RulePairLink         = "pair-link"
	RulePairTable        = "pair-table"
	RulePairNumber       = "pair-number"
	RulePairLag          = "pair-lag"
	RuleUndocumented     = "undocumented"
	RuleStaleComment     = "stale-comment"
	RuleCommentMentions  = "comment-mentions-missing"
	RuleMissingRoute     = "missing-route"
	RuleInstallMismatch  = "install-mismatch"
	RuleToolchain        = "toolchain-mismatch"
	RuleMissingTarget    = "missing-target"
)

// AllRules lists every rule in a stable order (for SARIF rule tables etc.).
var AllRules = []string{
	RuleMissingPath, RuleMissingSymbol, RuleUnknownFlag, RuleUnknownEnv,
	RuleUnknownConfigKey, RuleBrokenAnchor, RuleBrokenURL, RuleMissingCommand,
	RuleMissingImport, RuleStaleSection, RulePairHeading, RulePairCode,
	RulePairLink, RulePairTable, RulePairNumber, RulePairLag, RuleUndocumented,
	RuleStaleComment, RuleCommentMentions, RuleMissingRoute,
	RuleInstallMismatch, RuleToolchain, RuleMissingTarget,
}

// RuleDescriptions is the short text shown in SARIF/HTML rule metadata.
var RuleDescriptions = map[string]string{
	RuleMissingPath:      "A file or directory path mentioned in the document does not exist.",
	RuleMissingSymbol:    "A code symbol (function, type, method…) mentioned in the document does not exist.",
	RuleUnknownFlag:      "A command-line flag mentioned in the document is not defined in the code.",
	RuleUnknownEnv:       "An environment variable mentioned in the document is never read by the code.",
	RuleUnknownConfigKey: "A configuration key mentioned in the document does not appear in any config struct or sample.",
	RuleBrokenAnchor:     "A Markdown link points to a heading that does not exist.",
	RuleBrokenURL:        "An external URL does not respond successfully.",
	RuleMissingCommand:   "A command in a shell example refers to a script or path that does not exist.",
	RuleMissingImport:    "A Go import path in a code example does not correspond to a package in this module.",
	RuleStaleSection:     "The code a section refers to has changed substantially since the section was last edited.",
	RulePairHeading:      "A translated document has a different heading structure than its source.",
	RulePairCode:         "A code block in a translated document differs from the source.",
	RulePairLink:         "A link exists in only one of a source/translation pair.",
	RulePairTable:        "A table has a different shape in the source and the translation.",
	RulePairNumber:       "A number or version appears in only one of a source/translation pair.",
	RulePairLag:          "The source document has commits newer than the translation's last change.",
	RuleUndocumented:     "An exported symbol, flag or environment variable is not mentioned by any document.",
	RuleStaleComment:     "The body of a documented function changed in several commits after its comment was last edited.",
	RuleCommentMentions:  "A code comment names a parameter, symbol, flag or path that no longer exists.",
	RuleMissingRoute:     "An HTTP route mentioned in the document is not registered by any handler in the code.",
	RuleInstallMismatch:  "An install line (go get, pip install, npm install) names this project by a different path or name than its manifest.",
	RuleToolchain:        "The Go or Python version the document requires differs from go.mod / pyproject.toml.",
	RuleMissingTarget:    "A make/npm/just/task target mentioned in the document is not defined.",
}

// Fingerprint computes the stable identity of a finding for baselining.
// It intentionally excludes line/column so that edits elsewhere in the
// document do not invalidate the baseline. Parts are joined with '|'.
func Fingerprint(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(h[:8])
}

// NewFinding builds a Finding for a reference-based rule and fills the
// fingerprint from rule|file|kind|norm. The section heading is deliberately
// left out so that renaming a heading does not invalidate a baseline; the
// same reference repeated in several sections of one file shares one
// fingerprint.
func NewFinding(rule string, sev Severity, ref Reference, msg string) Finding {
	r := ref
	return Finding{
		Rule:        rule,
		Severity:    sev,
		Message:     msg,
		Loc:         ref.Loc,
		Ref:         &r,
		Fingerprint: Fingerprint(rule, ref.Loc.File, string(ref.Kind), ref.Norm),
	}
}

// Index is what resolvers query. Every method must be safe for concurrent
// use once the index has been built. Paths are relative to the repo root
// with forward slashes and no leading "./".
type Index interface {
	// --- files ---
	FileExists(rel string) bool
	DirExists(rel string) bool
	// Glob returns matching files/dirs for a pattern that may contain
	// '*', '?' and '**'. Returns nil when nothing matches.
	Glob(pattern string) []string
	// SimilarPaths returns up to n existing paths that are plausible
	// corrections for rel (same base name elsewhere, case mismatch, small
	// edit distance). Best first.
	SimilarPaths(rel string, n int) []string
	// TopLevelDirs returns the names of directories directly under root.
	TopLevelDirs() []string

	// --- Go ---
	ModulePath() string // "" when no go.mod
	// GoPackages returns known package names (the `package x` identifier).
	GoPackages() []string
	IsGoPackage(name string) bool
	// GoPackageDir maps a full import path to a directory (relative), ok=false if unknown.
	GoPackageDir(importPath string) (string, bool)
	// HasGoSymbol reports whether a qualified name exists. Accepted forms:
	// "pkg.Name", "pkg.Type.Method", "Type.Method" (any pkg), "Name" (any pkg).
	HasGoSymbol(qualified string) bool
	// GoSymbolFile returns the defining file (relative) of a qualified symbol.
	GoSymbolFile(qualified string) (string, bool)
	// SimilarGoSymbols returns up to n candidates close to qualified. Best first.
	SimilarGoSymbols(qualified string, n int) []string
	// IsGoType reports whether name is a type in any package (used by the
	// extractor to recognise "Type.Method" forms).
	IsGoType(name string) bool
	// HasGoMember reports whether any type has a field or method named name.
	HasGoMember(name string) bool
	HasFlag(name string) bool
	Flags() []string
	HasEnv(name string) bool
	Envs() []string
	HasJSONKey(dotted string) bool
	JSONKeys() []string
	// GoExported lists exported symbols for coverage (see Exported).
	GoExported() []Exported

	// --- Odin / Python ---
	HasOdin() bool
	OdinPackages() []string
	HasOdinSymbol(qualified string) bool
	SimilarOdinSymbols(qualified string, n int) []string
	HasPython() bool
	PyModules() []string
	HasPySymbol(qualified string) bool
	// PyModuleIsExample reports whether a Python module lives under a tests,
	// docs or examples tree (so claims about it are weaker).
	PyModuleIsExample(module string) bool
	SimilarPySymbols(qualified string, n int) []string

	// --- Markdown anchors ---
	// HasAnchor reports whether docRel (a markdown file) has a heading
	// whose slug equals slug (already lower-cased, URL-decoded).
	HasAnchor(docRel, slug string) bool
	Anchors(docRel string) []string

	// --- config samples ---
	HasConfigKey(dotted string) bool
	ConfigKeys() []string

	// --- HTTP routes ---
	// HasRoutes reports whether the code registers any HTTP route at all;
	// when it does not, route claims in documents are not checked.
	HasRoutes() bool
	// MatchRoute checks a documented path for a method ("" = any).
	MatchRoute(method, path string) RouteMatch
	// Routes lists every registration as "METHOD /path" or "/path", sorted.
	Routes() []string
	// SimilarRoutes returns up to n registered paths close to path. Best first.
	SimilarRoutes(path string, n int) []string

	// --- string literals ---
	// HasLiteral reports whether the code contains s as a string literal
	// (or struct tag value), spelled exactly so: the last resort before a
	// documented name is reported missing.
	HasLiteral(s string) bool

	// --- project identity ---
	// Project returns what the manifests declare: module path and Go
	// version, Python distribution name and requirement, npm name, and the
	// targets of Makefile / justfile / Taskfile / package.json scripts.
	Project() Project
}

// Project is the repository's declared identity (see Index.Project).
// Every field may be empty; Targets and TargetFiles may be nil.
type Project struct {
	GoModule    string              `json:"goModule,omitempty"`
	GoVersion   string              `json:"goVersion,omitempty"`  // "1.22", from the go directive
	PyName      string              `json:"pyName,omitempty"`     // [project] name
	PyRequires  string              `json:"pyRequires,omitempty"` // ">=3.10", "^3.9"
	NPMName     string              `json:"npmName,omitempty"`
	Targets     map[string][]string `json:"targets,omitempty"`     // tool ("make", "npm", "just", "task") → sorted names
	TargetFiles map[string]string   `json:"targetFiles,omitempty"` // tool → defining file (relative)
}

// HasTarget reports whether tool defines name.
func (p Project) HasTarget(tool, name string) bool {
	for _, t := range p.Targets[tool] {
		if t == name {
			return true
		}
	}
	return false
}

// RouteMatch is the outcome of Index.MatchRoute.
type RouteMatch struct {
	OK      bool
	Methods []string // when the path exists but only for other methods
	File    string   // registering file (relative) when OK
	Line    int
	Mounted bool // matched by its trailing segments (a router mounted under a prefix)
}

// Exported describes one exported code surface item for coverage.
type Exported struct {
	Package   string `json:"package"`   // Go package name ("" for flags/env)
	Qualified string `json:"qualified"` // "pkg.Name", "pkg.Type.Method", "--flag", "ENV_VAR"
	Kind      Kind   `json:"kind"`      // KindGoSymbol | KindFlag | KindEnv
	File      string `json:"file"`      // defining file, relative
	Line      int    `json:"line"`
}

// SymbolSpan locates a declaration together with its attached comment, for
// the comment checks. Line numbers are 1-based and inclusive; a zero
// DocStart means the declaration has no comment. Body covers the signature
// line through the closing line (for Python: through the last indented
// line; docstring lines are excluded from the churn count).
type SymbolSpan struct {
	Qualified string   `json:"qualified"`
	Kind      Kind     `json:"kind"` // KindGoSymbol | KindPySym | KindOdinSym
	File      string   `json:"file"`
	DocStart  int      `json:"docStart"`
	DocEnd    int      `json:"docEnd"`
	DeclLine  int      `json:"declLine"`
	BodyStart int      `json:"bodyStart"`
	BodyEnd   int      `json:"bodyEnd"`
	Doc       []string `json:"doc"`    // comment text, markers stripped
	Params    []string `json:"params"` // receiver, parameter and named result identifiers
	Exported  bool     `json:"exported"`
}
