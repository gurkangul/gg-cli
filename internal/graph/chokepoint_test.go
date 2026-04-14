package graph_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
