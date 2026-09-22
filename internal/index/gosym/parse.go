package gosym

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// symKind classifies an indexed declaration.
type symKind uint8

const (
	kindFunc symKind = iota
	kindType
	kindConst
	kindVar
	kindMethod // method of a named type, or an interface method
	kindField  // struct field, including embedded names
)

// entry is one declaration found in one file.
type entry struct {
	pkg   string // package name, with any "_test" suffix removed
	owner string // receiver / struct / interface type, "" for top level
	name  string
	kind  symKind
	file  string // relative to the root, forward slashes
	line  int
	test  bool
}

// qual returns the fully qualified lookup key of the entry.
func (e entry) qual() string {
	if e.owner != "" {
		return e.pkg + "." + e.owner + "." + e.name
	}
	return e.pkg + "." + e.name
}

// fileResult is everything one worker extracted from one file.
type fileResult struct {
	rel     string
	dir     string
	pkg     string
	test    bool
	entries []entry
	types   []string
	flags   []string
	envs    []string
	structs map[string]*ast.StructType
	err     error
}

// flagMethods are the flag.FlagSet methods that define a flag by name.
var flagMethods = map[string]bool{
	"Bool": true, "Int": true, "Int64": true, "Uint": true, "Uint64": true,
	"Float64": true, "String": true, "Duration": true, "Func": true,
	"BoolFunc": true, "Text": true, "Var": true, "TextVar": true,
}

var (
	flagNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)
	envNameRe  = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// envFuncs are the standard library calls whose first string argument is an
// environment variable name.
var envFuncs = map[string]bool{
	"os.Getenv": true, "os.LookupEnv": true, "os.Setenv": true,
	"os.Unsetenv": true, "syscall.Getenv": true, "syscall.Setenv": true,
	"syscall.Unsetenv": true,
}

// parseFile parses one Go file and extracts everything the index needs.
// A parse failure is returned inside the result and never panics.
func parseFile(root, rel string) *fileResult {
	r := &fileResult{
		rel:  rel,
		dir:  path4Dir(rel),
		test: strings.HasSuffix(rel, "_test.go"),
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
	if err != nil {
		r.err = wrapParseError(rel, err)
		return r
	}
	r.pkg = f.Name.Name
	if r.test {
		r.pkg = strings.TrimSuffix(r.pkg, "_test")
	}
	r.collectDecls(f, fset)
	r.collectCalls(f, fset)
	return r
}

// line returns the 1-based line of a position.
func lineOf(fset *token.FileSet, p token.Pos) int { return fset.Position(p).Line }

// add appends an entry, filling in the file, package and test flag.
func (r *fileResult) add(owner, name string, kind symKind, fset *token.FileSet, pos token.Pos) {
	if name == "" || name == "_" {
		return
	}
	r.entries = append(r.entries, entry{
		pkg:   r.pkg,
		owner: owner,
		name:  name,
		kind:  kind,
		file:  r.rel,
		line:  lineOf(fset, pos),
		test:  r.test,
	})
}

// collectDecls walks the top level declarations of a file.
func (r *fileResult) collectDecls(f *ast.File, fset *token.FileSet) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil || len(d.Recv.List) == 0 {
				r.add("", d.Name.Name, kindFunc, fset, d.Name.Pos())
				continue
			}
			if owner := recvBase(d.Recv.List[0].Type); owner != "" {
				r.add(owner, d.Name.Name, kindMethod, fset, d.Name.Pos())
			}
		case *ast.GenDecl:
			r.collectGenDecl(d, fset)
		}
	}
}

// collectGenDecl handles const, var and type declaration groups.
func (r *fileResult) collectGenDecl(d *ast.GenDecl, fset *token.FileSet) {
	switch d.Tok {
	case token.CONST, token.VAR:
		kind := kindConst
		if d.Tok == token.VAR {
			kind = kindVar
		}
		for _, spec := range d.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range vs.Names {
				r.add("", n.Name, kind, fset, n.Pos())
			}
		}
	case token.TYPE:
		for _, spec := range d.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name == "_" {
				continue
			}
			name := ts.Name.Name
			r.add("", name, kindType, fset, ts.Name.Pos())
			r.types = append(r.types, name)
			switch t := ts.Type.(type) {
			case *ast.StructType:
				if r.structs == nil {
					r.structs = map[string]*ast.StructType{}
				}
				r.structs[name] = t
				r.collectStructFields(name, t, fset)
			case *ast.InterfaceType:
				r.collectInterfaceMethods(name, t, fset)
			}
		}
	}
}

// collectStructFields indexes every field of a struct, embedded ones included.
func (r *fileResult) collectStructFields(owner string, st *ast.StructType, fset *token.FileSet) {
	if st.Fields == nil {
		return
	}
	for _, fld := range st.Fields.List {
		if len(fld.Names) == 0 {
			r.add(owner, embeddedName(fld.Type), kindField, fset, fld.Pos())
			continue
		}
		for _, n := range fld.Names {
			r.add(owner, n.Name, kindField, fset, n.Pos())
		}
	}
}

// collectInterfaceMethods indexes an interface's method names; an embedded
// interface is indexed under its own name as a field-like member.
func (r *fileResult) collectInterfaceMethods(owner string, it *ast.InterfaceType, fset *token.FileSet) {
	if it.Methods == nil {
		return
	}
	for _, m := range it.Methods.List {
		if len(m.Names) == 0 {
			r.add(owner, embeddedName(m.Type), kindField, fset, m.Pos())
			continue
		}
		for _, n := range m.Names {
			r.add(owner, n.Name, kindMethod, fset, n.Pos())
		}
	}
}

// recvBase returns the base type name of a method receiver, unwrapping
// pointers and generic parameter lists ("*Stack[T]" -> "Stack").
func recvBase(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvBase(t.X)
	case *ast.IndexExpr:
		return recvBase(t.X)
	case *ast.IndexListExpr:
		return recvBase(t.X)
	case *ast.ParenExpr:
		return recvBase(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// embeddedName returns the name an embedded field is known by.
func embeddedName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return embeddedName(t.X)
	case *ast.IndexExpr:
		return embeddedName(t.X)
	case *ast.IndexListExpr:
		return embeddedName(t.X)
	case *ast.ParenExpr:
		return embeddedName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// collectCalls scans every call expression for flag definitions and
// environment variable accesses.
func (r *fileResult) collectCalls(f *ast.File, _ *token.FileSet) {
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name, ok := flagName(call); ok {
			r.flags = append(r.flags, name)
		}
		if name, ok := envName(call); ok {
			r.envs = append(r.envs, name)
		}
		return true
	})
}

// flagName reports the flag name defined by a call such as
// flag.String("out", ...), fs.BoolVar(&v, "debug", ...) or
// flag.Func("mode", ...). The receiver is not checked, only the method name.
func flagName(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	method := sel.Sel.Name
	if !flagMethods[method] && !strings.HasSuffix(method, "Var") {
		return "", false
	}
	if len(call.Args) < 2 {
		return "", false // too little context to be a flag definition
	}
	// XxxVar and Var take (target, name, ...); everything else takes (name, ...).
	first, second := 0, 1
	if strings.HasSuffix(method, "Var") {
		first, second = 1, 0
	}
	for _, i := range [2]int{first, second} {
		if i >= len(call.Args) {
			continue
		}
		s, ok := stringLit(call.Args[i])
		if !ok {
			continue
		}
		s = strings.TrimLeft(s, "-")
		if flagNameRe.MatchString(s) {
			return s, true
		}
	}
	return "", false
}

// envName reports the environment variable a call reads or writes. Known
// os/syscall helpers are trusted as they are; any other call whose name
// mentions "env" must pass an UPPER_SNAKE literal to qualify.
func envName(call *ast.CallExpr) (string, bool) {
	name := callName(call.Fun)
	if name == "" || len(call.Args) == 0 {
		return "", false
	}
	if strings.HasSuffix(name, "ExpandEnv") || strings.HasSuffix(name, "Expand") {
		return "", false // takes a template, not a variable name
	}
	s, ok := stringLit(call.Args[0])
	if !ok || s == "" {
		return "", false
	}
	if envFuncs[name] {
		return s, true
	}
	if strings.Contains(strings.ToLower(name), "env") && envNameRe.MatchString(s) {
		return s, true
	}
	return "", false
}

// callName renders a call target as "fn" or "recv.Sel" for matching.
func callName(fun ast.Expr) string {
	switch t := fun.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.IndexExpr:
		return callName(t.X)
	case *ast.IndexListExpr:
		return callName(t.X)
	case *ast.ParenExpr:
		return callName(t.X)
	}
	return ""
}

// stringLit unquotes a string literal expression.
func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}
