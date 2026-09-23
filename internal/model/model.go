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
	KindRustSym   Kind = "rustsym"   // Rust crate::module::item / Type::method / name
	KindJSSym     Kind = "jssym"     // JavaScript/TypeScript module.name / Class.method / name
	KindCSharpSym Kind = "cssym"     // C# Namespace.Class.Method / Class.Method
	KindCSym      Kind = "csym"      // C/C++ name / ns::name / Class::method
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
	KindDefault   Kind = "default"   // documented default value: "flag:port|8080", "key:log.level|info", "env:X|1"
)

// AllKinds lists every Kind in a stable order.
var AllKinds = []Kind{
	KindPath, KindGoSymbol, KindOdinSym, KindPySym, KindRustSym, KindJSSym,
	KindCSharpSym, KindCSym, KindFlag, KindEnv,
	KindConfigKey, KindAnchor, KindURL, KindCommand, KindImport, KindRoute,
	KindInstall, KindToolchain, KindTarget, KindDefault,
}

// Naming is the set of bare-call spellings ("name()") a language owns.
type Naming int

const (
	NamingSnake  Naming = 1 << iota // render_frame()
	NamingCamel                     // renderFrame()
	NamingPascal                    // RenderFrame()
)

// Lang describes one language whose symbols a lightweight declaration
// index resolves (every language except Go, which has go/parser). The
// table is the only place a language is enumerated: the composite index,
// the extractor, the resolver, the reports and the CLI all range over it.
type Lang struct {
	Kind Kind
	// ID names the language on the command line (docrot index --kind ID)
	// and in the report summary.
	ID string
	// Name is the language as messages spell it.
	Name string
	// Sep is how documents qualify names: "." or "::".
	Sep string
	// Exts lists the source file extensions the language's index reads.
	Exts []string
	// Naming lists the bare-call spellings that belong to the language.
	Naming Naming
	// Methods lists the spellings of a method on a capitalised owner
	// ("Runner.run_async", "Client.fetchAll", "Client.Fetch") that belong
	// to the language; zero when documents never write members that way.
	Methods Naming
	// Flat marks a language whose declarations are top-level procedures
	// (Odin, C): a bare call in a document is a claim at full severity,
	// where in an object language it is usually a method or local helper.
	Flat bool
	// Stdlib lists first segments that name the language's standard
	// library or runtime: a dotted name starting with one is never a claim
	// about the repository unless the repository defines it itself.
	Stdlib map[string]bool
}

// Langs lists every supported language in tie-break order: when a name
// fits several present languages, the first wins the classification (the
// resolver still consults every language before reporting a miss).
var Langs = []Lang{
	{Kind: KindOdinSym, ID: "odin", Name: "Odin", Sep: ".", Exts: []string{".odin"}, Naming: NamingSnake, Flat: true,
		Stdlib: set("core base vendor")},
	{Kind: KindPySym, ID: "python", Name: "Python", Sep: ".", Exts: []string{".py"}, Naming: NamingSnake, Methods: NamingSnake,
		Stdlib: set(`typing typing_extensions datetime enum dataclasses collections functools itertools
		os sys re json pathlib asyncio logging math random time uuid decimal fractions io shutil subprocess
		threading multiprocessing socket ssl http urllib email csv sqlite3 unittest pytest contextlib abc
		inspect types copy pickle struct hashlib hmac secrets base64 string textwrap operator warnings
		argparse configparser tempfile glob fnmatch zipfile tarfile gzip heapq bisect array queue weakref
		numbers statistics ipaddress mimetypes platform signal select selectors traceback importlib pkgutil
		builtins __future__ concurrent contextvars dis gc html xml zoneinfo tomllib venv pprint reprlib`)},
	{Kind: KindRustSym, ID: "rust", Name: "Rust", Sep: "::", Exts: []string{".rs"}, Naming: NamingSnake,
		Stdlib: set("std core alloc proc_macro test")},
	{Kind: KindJSSym, ID: "js", Name: "JavaScript", Sep: ".", Exts: []string{".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx"}, Naming: NamingCamel, Methods: NamingCamel,
		Stdlib: set(`console document window process Math JSON Object Array Promise Buffer fetch require
		module globalThis Number String Boolean Date RegExp Map Set Symbol Error Reflect Proxy navigator
		localStorage sessionStorage location history performance crypto URL URLSearchParams`)},
	{Kind: KindCSharpSym, ID: "csharp", Name: "C#", Sep: ".", Exts: []string{".cs"}, Naming: NamingPascal, Methods: NamingPascal,
		Stdlib: set("System Microsoft Newtonsoft Console Task String Int32 Int64 Math Convert Enum Guid DateTime")},
	{Kind: KindCSym, ID: "c", Name: "C/C++", Sep: "::", Exts: []string{".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx"}, Naming: NamingSnake, Flat: true,
		Stdlib: set("std boost")},
}

func set(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

// LangOf returns the language a symbol Kind belongs to.
func LangOf(k Kind) (Lang, bool) {
	for _, l := range Langs {
		if l.Kind == k {
			return l, true
		}
	}
	return Lang{}, false
}

// LangByID returns the language named on the command line.
func LangByID(id string) (Lang, bool) {
	for _, l := range Langs {
		if l.ID == id {
			return l, true
		}
	}
	return Lang{}, false
}

// IsSymbol reports whether the Kind names a code declaration in any
// language, Go included.
func (k Kind) IsSymbol() bool {
	if k == KindGoSymbol {
		return true
	}
	_, ok := LangOf(k)
	return ok
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
	// Rooted marks a path written relative to a documentation source root
	// that is not the repository root (Sphinx "/topics/x", written from
	// docs/): the resolver tries every ancestor of the document.
	Rooted bool `json:"rooted,omitempty"`
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
	RuleStaleSymbol      = "stale-symbol"
	RuleDefaultMismatch  = "default-mismatch"
)

// AllRules lists every rule in a stable order (for SARIF rule tables etc.).
var AllRules = []string{
	RuleMissingPath, RuleMissingSymbol, RuleUnknownFlag, RuleUnknownEnv,
	RuleUnknownConfigKey, RuleBrokenAnchor, RuleBrokenURL, RuleMissingCommand,
	RuleMissingImport, RuleStaleSection, RulePairHeading, RulePairCode,
	RulePairLink, RulePairTable, RulePairNumber, RulePairLag, RuleUndocumented,
	RuleStaleComment, RuleCommentMentions, RuleMissingRoute,
	RuleInstallMismatch, RuleToolchain, RuleMissingTarget, RuleStaleSymbol, RuleDefaultMismatch,
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
	RuleStaleSymbol:      "The body of a function or type this section names changed in several commits after the section was last edited.",
	RuleDefaultMismatch:  "The default value the document gives for a flag, config key or environment variable differs from the one the code declares.",
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

	// --- other languages (see Langs) ---
	// Languages lists the kinds of the languages with at least one indexed
	// source file, in Langs order.
	Languages() []Kind
	HasLang(kind Kind) bool
	// Namespaces lists the packages, modules, crates or namespaces of a
	// language, sorted.
	Namespaces(kind Kind) []string
	// IsNamespace reports whether qualified names a namespace of the
	// language rather than a declaration.
	IsNamespace(kind Kind, qualified string) bool
	// IsExample reports whether a namespace lives under a tests, docs or
	// examples tree (so claims about it are weaker).
	IsExample(kind Kind, namespace string) bool
	HasSymbol(kind Kind, qualified string) bool
	// Opaque reports whether an unknown member of qualified is not worth
	// a finding because the language index cannot list its members.
	Opaque(kind Kind, qualified string) bool
	SimilarSymbols(kind Kind, qualified string, n int) []string

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

	// --- declared defaults ---
	// Default returns the default the code declares for a flag ("flag"),
	// a config key ("key") or an environment variable ("env"), as spelled
	// in the code.
	Default(kind, name string) (string, bool)

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
	NPMExports  []string            `json:"npmExports,omitempty"`  // keys of package.json "exports": ".", "./server", "./v4/*"
	NodeVersion string              `json:"nodeVersion,omitempty"` // package.json engines.node: ">=18"
	CargoName   string              `json:"cargoName,omitempty"`   // [package] name of Cargo.toml
	RustVersion string              `json:"rustVersion,omitempty"` // rust-version = "1.70"
	Targets     map[string][]string `json:"targets,omitempty"`     // tool ("make", "npm", "just", "task") → sorted names
	TargetFiles map[string]string   `json:"targetFiles,omitempty"` // tool → defining file (relative)
	// Intersphinx is set when a Sphinx conf.py maps other projects'
	// inventories: a :ref: label that no local document defines may be
	// theirs.
	Intersphinx bool `json:"intersphinx,omitempty"`
}

// HasTarget reports whether tool defines name. A make pattern rule
// ("test-%") defines every name it matches.
func (p Project) HasTarget(tool, name string) bool {
	for _, t := range p.Targets[tool] {
		if t == name {
			return true
		}
		if i := strings.Index(t, "%"); i >= 0 && tool == "make" {
			pre, suf := t[:i], t[i+1:]
			if len(name) > len(pre)+len(suf) && strings.HasPrefix(name, pre) && strings.HasSuffix(name, suf) {
				return true
			}
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
	Kind      Kind     `json:"kind"` // KindGoSymbol or a Langs kind
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
