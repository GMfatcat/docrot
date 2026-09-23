package gosym

import (
	"go/ast"
	"reflect"
	"strconv"
	"strings"

	"docrot/internal/index/defaults"
)

// maxKeyDepth caps how deep nested struct types are followed when building
// dotted configuration key paths.
const maxKeyDepth = 6

// tagKeys are the struct tags that name a configuration key. Documents
// reference whichever spelling the project's config format uses.
var tagKeys = [...]string{"json", "yaml", "toml"}

// collectJSONKeys walks every named struct type of one package and records the
// dotted key paths its fields describe, including every prefix.
func collectJSONKeys(structs map[string]*ast.StructType, out map[string]bool, defs *defaults.Set) {
	for name, st := range structs {
		visiting := map[string]bool{name: true}
		walkStructKeys(st, "", 0, structs, visiting, out, defs)
		delete(visiting, name)
	}
}

// tagDefault returns the value of a `default:"…"` struct tag.
func tagDefault(fld *ast.Field) string {
	if fld.Tag == nil {
		return ""
	}
	raw, err := strconv.Unquote(fld.Tag.Value)
	if err != nil {
		return ""
	}
	v, _ := reflect.StructTag(raw).Lookup("default")
	return v
}

// walkStructKeys records the keys of one struct below prefix.
func walkStructKeys(st *ast.StructType, prefix string, depth int, structs map[string]*ast.StructType, visiting map[string]bool, out map[string]bool, defs *defaults.Set) {
	if st == nil || st.Fields == nil || depth >= maxKeyDepth {
		return
	}
	for _, fld := range st.Fields.List {
		names, hidden := fieldKeyNames(fld)
		if hidden {
			continue
		}
		if len(names) == 0 {
			// An embedded field with no tag is inlined by encoding/json, so
			// its own fields live at this level.
			if len(fld.Names) == 0 {
				descend(fld.Type, prefix, depth, structs, visiting, out, defs)
			}
			continue
		}
		for _, name := range names {
			key := name
			if prefix != "" {
				key = prefix + "." + name
			}
			out[key] = true
			if isMapType(fld.Type) {
				out[key+".*"] = true // a map: any child key is valid
			}
			if d := tagDefault(fld); d != "" && defs != nil {
				defs.Add("key", key, d)
			}
			descend(fld.Type, key, depth+1, structs, visiting, out, defs)
		}
	}
}

// descend follows a field type into a struct declared in the same package, an
// inline struct literal, or one wrapped in a pointer, slice, array or map.
func descend(t ast.Expr, prefix string, depth int, structs map[string]*ast.StructType, visiting map[string]bool, out map[string]bool, defs *defaults.Set) {
	if depth >= maxKeyDepth {
		return
	}
	st, name := structOf(t, structs)
	if st == nil {
		return
	}
	if name != "" {
		if visiting[name] {
			return // cyclic type
		}
		visiting[name] = true
		defer delete(visiting, name)
	}
	walkStructKeys(st, prefix, depth, structs, visiting, out, defs)
}

// isMapType reports whether a field type is a map, through pointers and
// parentheses.
func isMapType(t ast.Expr) bool {
	switch e := t.(type) {
	case *ast.StarExpr:
		return isMapType(e.X)
	case *ast.ParenExpr:
		return isMapType(e.X)
	case *ast.MapType:
		return true
	}
	return false
}

// structOf resolves a field type to a struct type declared in the same
// package (or an inline struct), unwrapping pointers, slices, arrays, maps and
// generic instantiations. The returned name is empty for inline structs.
func structOf(t ast.Expr, structs map[string]*ast.StructType) (*ast.StructType, string) {
	switch e := t.(type) {
	case *ast.StarExpr:
		return structOf(e.X, structs)
	case *ast.ParenExpr:
		return structOf(e.X, structs)
	case *ast.ArrayType:
		return structOf(e.Elt, structs)
	case *ast.MapType:
		return structOf(e.Value, structs)
	case *ast.IndexExpr:
		return structOf(e.X, structs)
	case *ast.IndexListExpr:
		return structOf(e.X, structs)
	case *ast.StructType:
		return e, ""
	case *ast.Ident:
		if st, ok := structs[e.Name]; ok {
			return st, e.Name
		}
	}
	return nil, ""
}

// fieldKeyNames returns the configuration key names a field contributes: one
// per json/yaml/toml tag, or the Go field name when the field carries no such
// tag. A "-" tag hides the field, which is reported separately so that an
// embedded field is not inlined by mistake.
func fieldKeyNames(fld *ast.Field) (names []string, hidden bool) {
	seen := map[string]bool{}
	if fld.Tag != nil {
		if raw, err := strconv.Unquote(fld.Tag.Value); err == nil {
			tag := reflect.StructTag(raw)
			for _, key := range tagKeys {
				v, ok := tag.Lookup(key)
				if !ok {
					continue
				}
				name := strings.TrimSpace(strings.SplitN(v, ",", 2)[0])
				if name == "-" {
					hidden = true
					continue
				}
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	if len(names) > 0 {
		return names, false
	}
	if hidden {
		return nil, true
	}
	for _, n := range fld.Names {
		if n.Name == "_" || seen[n.Name] {
			continue
		}
		seen[n.Name] = true
		names = append(names, n.Name)
	}
	return names, false
}
