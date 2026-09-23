// Package lang is the contract every lightweight language index satisfies
// (Odin, Python, Rust, …): a declaration index built from line-level
// patterns rather than a parser. Go keeps its own richer index under
// gosym; the composite index treats every other language through this
// interface, so adding a language means adding one package that implements
// it and one row in model.Langs.
package lang

import (
	"docrot/internal/index/defaults"
	"docrot/internal/index/literals"
	"docrot/internal/index/routes"
	"docrot/internal/model"
)

// Stats counts what an index found.
type Stats struct {
	Files      int // source files read
	Namespaces int // packages, modules, crates or namespaces
	Symbols    int // recognised declarations
}

// Index is a read-only, concurrency-safe declaration index of one
// language. Qualified names use the language's separator as documents
// spell it ("pkg.name", "Class.method", "crate::module::item"); a trailing
// call suffix "()" is tolerated everywhere.
type Index interface {
	// Empty reports whether no source file of the language was found.
	Empty() bool
	// Namespaces lists every package, module, crate or namespace, sorted.
	Namespaces() []string
	// IsNamespace reports whether qualified names a namespace rather than
	// a declaration ("fastapi.responses", "crate::io").
	IsNamespace(qualified string) bool
	// IsExample reports whether a namespace lives under a tests, docs or
	// examples tree, so that claims about it are weaker.
	IsExample(namespace string) bool
	// Has reports whether qualified exists.
	Has(qualified string) bool
	// File returns the declaring file (relative, forward slashes) and
	// 1-based line of qualified.
	File(qualified string) (string, int, bool)
	// Similar returns up to n existing candidates close to qualified,
	// best first.
	Similar(qualified string, n int) []string
	// Symbols lists every qualified declaration, sorted (docrot index).
	Symbols() []string
	// Span returns the declaration span (comment and body geometry) of
	// qualified, for the comment and stale-symbol checks.
	Span(qualified string) (model.SymbolSpan, bool)
	// AllSpans returns the documented declaration surface, sorted by
	// qualified name.
	AllSpans() []model.SymbolSpan
	// Literals returns the identifier-like string literals of the tree.
	Literals() *literals.Set
	// Routes returns the HTTP route registrations found.
	Routes() []routes.Route
	// Defaults returns the flag, key and environment defaults found.
	Defaults() *defaults.Set
	// Counts returns the index-wide counters.
	Counts() Stats
}
