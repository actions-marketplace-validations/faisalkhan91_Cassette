package cassette

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestPublicAPI_NoInternalTypeLeak is the durable guard the public-types refactor
// was chartered to make possible: no exported declaration in the root package may
// reference an internal/* type in its signature (params, results, or exported
// struct fields). Go's internal-package rule makes such a symbol uncallable /
// unconstructible by external modules, so it is dead public surface — exactly the
// RekeyFile(*wirefmt.File) / Options.Match leaks the audits found. Stdlib-only
// (go/parser), no extra dependency.
func TestPublicAPI_NoInternalTypeLeak(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var leaks []string

	for _, fp := range files {
		if strings.HasSuffix(fp, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, fp, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", fp, err)
		}
		// import alias -> import path
		imports := map[string]string{}
		for _, is := range f.Imports {
			path := strings.Trim(is.Path.Value, `"`)
			name := path[strings.LastIndex(path, "/")+1:]
			if is.Name != nil {
				name = is.Name.Name
			}
			imports[name] = path
		}
		// flag scans a type expression for qualified refs to an internal package.
		flag := func(where string, typ ast.Expr) {
			if typ == nil {
				return
			}
			ast.Inspect(typ, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if path, ok := imports[id.Name]; ok && strings.Contains(path, "/internal/") {
					leaks = append(leaks, where+": "+id.Name+"."+sel.Sel.Name+" ("+path+")")
				}
				return true
			})
		}

		ast.Inspect(f, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() {
					return true
				}
				// Skip methods on unexported receiver types (not public surface).
				if d.Recv != nil && len(d.Recv.List) > 0 {
					rt := d.Recv.List[0].Type
					if star, ok := rt.(*ast.StarExpr); ok {
						rt = star.X
					}
					if id, ok := rt.(*ast.Ident); ok && !id.IsExported() {
						return true
					}
				}
				w := "func " + d.Name.Name
				if d.Type.Params != nil {
					for _, p := range d.Type.Params.List {
						flag(w+" param", p.Type)
					}
				}
				if d.Type.Results != nil {
					for _, r := range d.Type.Results.List {
						flag(w+" result", r.Type)
					}
				}
			case *ast.TypeSpec:
				if !d.Name.IsExported() {
					return true
				}
				st, ok := d.Type.(*ast.StructType)
				if !ok {
					return true
				}
				for _, fld := range st.Fields.List {
					exported := len(fld.Names) == 0 // embedded
					for _, nm := range fld.Names {
						if nm.IsExported() {
							exported = true
						}
					}
					if exported {
						flag("type "+d.Name.Name+" field", fld.Type)
					}
				}
			}
			return true
		})
	}

	if len(leaks) > 0 {
		t.Errorf("exported root API references internal/* types (uncallable by external modules):\n  %s",
			strings.Join(leaks, "\n  "))
	}
}
