package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// cmdOrphans reconciles cassette fixture files against the tests that reference
// them: it flags fixtures that no test loads (dead fixtures) and New(...) calls
// whose fixture file is missing. It resolves cassettetest.New(t, "name") to
// <name>.yaml and New(t, "") to <EnclosingTestFunc>.yaml (the helper's default).
// Subtests (t.Run) that call New are attributed to their parent test — a known
// limitation, reported as a note.
func cmdOrphans(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags(), args, "usage: cassette orphans [dir]", stderr)
	if !ok || in.nargs() > 1 {
		return orphansUsage(stderr)
	}
	dir := in.arg(0)
	if dir == "" {
		dir = "."
	}

	referenced := map[string]bool{}
	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // skip unparseable test files
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			enclosing := fn.Name.Name
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				// Match cassettetest.New(t, name) (selector) or a dot-imported New(t, name).
				switch fn := call.Fun.(type) {
				case *ast.SelectorExpr:
					if fn.Sel.Name != "New" {
						return true
					}
				case *ast.Ident:
					if fn.Name != "New" {
						return true
					}
				default:
					return true
				}
				name := stringLit(call.Args[1])
				if name == "" {
					name = enclosing
				}
				if name != "" {
					referenced[name] = true
				}
				return true
			})
		}
		return nil
	})
	if walkErr != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", walkErr)
		return exitFail
	}

	// Collect fixture files under any testdata/cassettes directory.
	fixtures := map[string]string{} // name -> path
	for _, path := range walkYAML(dir) {
		if !strings.Contains(filepath.ToSlash(path), "testdata/cassettes/") {
			continue
		}
		fixtures[strings.TrimSuffix(filepath.Base(path), ".yaml")] = path
	}

	var orphans, missing []string
	for name, path := range fixtures {
		if !referenced[name] {
			orphans = append(orphans, path)
		}
	}
	for name := range referenced {
		if _, ok := fixtures[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(orphans)
	sort.Strings(missing)

	st := newStyle(stdout)
	for _, p := range orphans {
		fmt.Fprintf(stdout, "%s orphan fixture (no test references it): %s\n", st.yellow("orphan"), p)
	}
	for _, n := range missing {
		fmt.Fprintf(stdout, "%s missing fixture for New(…, %q) — record it with -update\n", st.red("missing"), n)
	}
	if len(orphans) == 0 && len(missing) == 0 {
		fmt.Fprintf(stdout, "%s %d fixture(s) all referenced; no orphans or missing\n", st.check(), len(fixtures))
		return exitOK
	}
	return exitFail
}

func stringLit(e ast.Expr) string {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return ""
	}
	return strings.Trim(bl.Value, "`\"")
}

func orphansUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, "usage: cassette orphans [dir]")
	return exitUsage
}
