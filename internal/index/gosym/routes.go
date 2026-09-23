package gosym

import (
	"go/ast"
	"go/token"
	"strings"

	"docrot/internal/index/routes"
)

// methodCalls are router methods named after the HTTP method they register
// (chi, gin, echo, fiber, gorilla's Methods-less helpers…).
var methodCalls = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Patch": "PATCH", "Delete": "DELETE",
	"Head": "HEAD", "Options": "OPTIONS", "Connect": "CONNECT", "Trace": "TRACE",
	"GET": "GET", "POST": "POST", "PUT": "PUT", "PATCH": "PATCH", "DELETE": "DELETE",
	"HEAD": "HEAD", "OPTIONS": "OPTIONS", "CONNECT": "CONNECT", "TRACE": "TRACE",
}

// anyCalls register a path for every method. The value says which argument
// holds the path.
var anyCalls = map[string]int{
	"Handle": 0, "HandleFunc": 0, "Any": 0, "All": 0, "Path": 0, "Static": 0,
	"StaticFS": 0, "StaticFile": 0, "File": 0, "Add": 1, "Method": 1, "MethodFunc": 1,
}

// prefixCalls mount a sub-router or a handler group under a prefix.
var prefixCalls = map[string]bool{
	"Route": true, "Group": true, "Mount": true, "PathPrefix": true, "StripPrefix": true,
	"Use": false, // Use takes middleware, never a path
}

// routeCalls recognises a route registration and returns the routes it
// declares. Only string literals count; a path held in a constant is left
// to the string-literal index.
func routeCalls(call *ast.CallExpr, fset *token.FileSet, rel string) []routes.Route {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return nil
	}
	name := sel.Sel.Name
	line := lineOf(fset, call.Pos())
	mk := func(method, path string, prefix bool) []routes.Route {
		if strings.Contains(path, "://") {
			return nil // http.Get("https://…"): a client call
		}
		if !strings.HasPrefix(path, "/") && name != "Handle" && name != "HandleFunc" {
			return nil // cache.Get("key"): only net/http patterns may carry a host
		}
		m, p, ok := routes.SplitPattern(path)
		if !ok {
			return nil
		}
		if method != "" {
			m = method
		}
		return []routes.Route{{Method: m, Path: p, File: rel, Line: line, Prefix: prefix}}
	}
	if m, ok := methodCalls[name]; ok {
		if p, ok := stringLit(call.Args[0]); ok {
			return mk(m, p, false)
		}
		return nil
	}
	if prefixCalls[name] {
		if p, ok := stringLit(call.Args[0]); ok && strings.HasPrefix(p, "/") {
			return mk("", p, true)
		}
		return nil
	}
	argIdx, ok := anyCalls[name]
	if !ok {
		return nil
	}
	switch name {
	case "Method", "MethodFunc":
		// chi: r.Method("GET", "/x", h)
		if len(call.Args) < 2 {
			return nil
		}
		m, _ := stringLit(call.Args[0])
		p, ok := stringLit(call.Args[1])
		if !ok || !routes.Methods[strings.ToUpper(m)] {
			return nil
		}
		return mk(strings.ToUpper(m), p, false)
	case "Handle", "Add":
		// gin: r.Handle("GET", "/x", h); echo: e.Add("GET", "/x", h);
		// net/http: mux.Handle("/x", h) or mux.Handle("GET /x", h)
		if len(call.Args) >= 2 {
			if m, ok := stringLit(call.Args[0]); ok && routes.Methods[strings.ToUpper(m)] {
				if p, ok := stringLit(call.Args[1]); ok {
					return mk(strings.ToUpper(m), p, false)
				}
				return nil
			}
		}
		argIdx = 0
	}
	if argIdx >= len(call.Args) {
		return nil
	}
	if p, ok := stringLit(call.Args[argIdx]); ok {
		return mk("", p, false)
	}
	return nil
}
