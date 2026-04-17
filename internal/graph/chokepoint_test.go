package graph_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// extractCypherString attempts to extract the raw Cypher text from a call
// expression that is the second argument of a runQuery call. It handles:
//   - plain string literals ("..." or `...`)
//   - fmt.Sprintf(formatStr, ...) — returns just the format string
//
// Returns ("", false) when the expression shape is unrecognised.
func extractCypherString(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, err := strconv.Unquote(e.Value)
			if err != nil {
				// raw string (backtick) — strip delimiters manually
				s = strings.Trim(e.Value, "`")
			}
			return s, true
		}
	case *ast.CallExpr:
		// fmt.Sprintf(format, ...) — inspect the format string only
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "fmt" {
				if len(e.Args) > 0 {
					return extractCypherString(e.Args[0])
				}
			}
		}
	}
	return "", false
}

// locateGraphPkgDir returns the absolute path of the internal/graph package
// directory by walking up from this test file.
func locateGraphPkgDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Dir(thisFile)
}

// TestAllRunQueryCallsReferenceProjectID verifies that every runQuery call in
// the graph package passes a Cypher string that references project_id (either
// directly or through $props/$identity that carry the project scope).
//
// Rationale: a runQuery call without project_id in the Cypher (and no $props /
// $identity carrying it) would be an unscoped cross-project read/write.
//
// Legitimate exceptions (runQueryNoPID) are not tested here — they are covered
// by TestRunQueryNoPIDCallerAllowlist.
func TestAllRunQueryCallsReferenceProjectID(t *testing.T) {
	pkgDir := locateGraphPkgDir(t)

	fset := token.NewFileSet()
	//nolint:staticcheck
	pkgs, err := parser.ParseDir(fset, pkgDir, nil, 0)
	if err != nil {
		t.Fatalf("parse graph package: %v", err)
	}

	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filepath.Base(filename)
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name != "runQuery" {
					return true
				}
				// runQuery signature: (ctx, cypher, params) — cypher is arg[1]
				if len(call.Args) < 2 {
					return true
				}
				cypher, ok := extractCypherString(call.Args[1])
				if !ok {
					// Can't statically extract the string — skip (dynamic query)
					return true
				}
				// Accept Cypher that carries project_id directly or via param maps
				// that always include it ($props, $identity, $pid).
				hasIsolation := strings.Contains(cypher, "project_id") ||
					strings.Contains(cypher, "$props") ||
					strings.Contains(cypher, "$identity")
				if !hasIsolation {
					pos := fset.Position(call.Pos())
					t.Errorf(
						"%s:%d — runQuery Cypher does not reference project_id "+
							"(or $props/$identity): %q",
						base, pos.Line, cypher,
					)
				}
				return true
			})
		}
	}
}

// TestRunQueryNoPIDCallerAllowlist locks the set of functions that are
// permitted to call runQueryNoPID. Any new caller that is not in this list
// fails the test, preventing accidental unscoped Cypher from slipping in.
//
// To add a legitimate new caller (DDL / schema-level query), add its
// "file:funcName" entry to the allowlist below and explain why in a comment.
func TestRunQueryNoPIDCallerAllowlist(t *testing.T) {
	// allowlist maps base filename → set of enclosing function names.
	// Only DDL / schema-level callers belong here.
	allowlist := map[string]map[string]bool{
		"schema.go": {"SchemaInit": true},
	}

	pkgDir := locateGraphPkgDir(t)

	fset := token.NewFileSet()
	//nolint:staticcheck
	pkgs, err := parser.ParseDir(fset, pkgDir, nil, 0)
	if err != nil {
		t.Fatalf("parse graph package: %v", err)
	}

	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filepath.Base(filename)
			if strings.HasSuffix(base, "_test.go") || base == "queries.go" {
				continue
			}
			// Walk top-level function declarations to track enclosing name.
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if sel.Sel.Name != "runQueryNoPID" {
						return true
					}
					permitted := allowlist[base][fn.Name.Name]
					if !permitted {
						pos := fset.Position(call.Pos())
						t.Errorf(
							"%s:%d — unexpected runQueryNoPID caller %q in %s. "+
								"Only DDL/schema queries may bypass project_id scoping. "+
								"Add to the allowlist in TestRunQueryNoPIDCallerAllowlist if intentional.",
							base, pos.Line, fn.Name.Name, base,
						)
					}
					return true
				})
			}
		}
	}
}

// TestNoRawSessRunOutsideQueriesGo enforces the Cypher choke-point contract:
// every sess.Run call inside the graph package must live in queries.go.
//
// Any other file calling sess.Run (or the neo4j session's Run method directly)
// bypasses the single instrumentation and audit point and risks cross-project
// data leaks because it skips the project_id injection done by runQuery /
// runQueryNoPID.
//
// The test scans the package directory with the Go AST parser, which means it
// catches violations at compile-CI time — no runtime Memgraph connection needed.
func TestNoRawSessRunOutsideQueriesGo(t *testing.T) {
	// Locate the internal/graph package directory relative to this test file.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	pkgDir := filepath.Dir(thisFile)

	fset := token.NewFileSet()
	//nolint:staticcheck // parser.ParseDir is deprecated in Go 1.25 but still available;
	// go/packages would add a heavy dependency for this simple AST scan.
	pkgs, err := parser.ParseDir(fset, pkgDir, nil, 0)
	if err != nil {
		t.Fatalf("parse graph package: %v", err)
	}

	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filepath.Base(filename)
			// queries.go is the only permitted location for sess.Run calls.
			if base == "queries.go" {
				continue
			}
			// Test files may import neo4j types for assertions but must not
			// call sess.Run themselves.
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name != "Run" {
					return true
				}
				// Receiver name heuristic: "sess", "session", or any ident
				// whose name contains "sess" — catches c.session(...).Run
				// and local `sess := c.session(ctx); sess.Run(...)`.
				var receiverName string
				switch x := sel.X.(type) {
				case *ast.Ident:
					receiverName = x.Name
				case *ast.CallExpr:
					// e.g. c.session(ctx).Run(...)
					if inner, ok := x.Fun.(*ast.SelectorExpr); ok {
						receiverName = inner.Sel.Name
					}
				}
				if strings.Contains(strings.ToLower(receiverName), "sess") ||
					strings.Contains(strings.ToLower(receiverName), "session") {
					pos := fset.Position(call.Pos())
					t.Errorf(
						"forbidden raw .Run call in %s:%d — "+
							"all Cypher execution must go through runQuery or runQueryNoPID in queries.go",
						base, pos.Line,
					)
				}
				return true
			})
		}
	}
}
