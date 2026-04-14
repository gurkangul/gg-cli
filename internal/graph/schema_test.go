//go:build integration

package graph_test

import (
	"context"
	"testing"

	"github.com/gurkangul/gg-cli/internal/graph"
)

func TestSchemaInit(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	// Idempotent — safe to call multiple times.
	if err := c.SchemaInit(ctx); err != nil {
		t.Fatalf("SchemaInit: %v", err)
	}
	if err := c.SchemaInit(ctx); err != nil {
		t.Fatalf("SchemaInit second call: %v", err)
	}
}

func TestSymbolNodeConstructor(t *testing.T) {
	n := graph.SymbolNode("NewClient", "go", graph.KindFunction, graph.VisPublic, graph.ResolutionSemantic)
	if n.Label != graph.LabelSymbol {
		t.Errorf("want label %s, got %s", graph.LabelSymbol, n.Label)
	}
	if n.Properties["boundary"] != true {
		t.Errorf("public symbol should have boundary=true")
	}
	if n.Properties["kind"] != string(graph.KindFunction) {
		t.Errorf("unexpected kind=%v", n.Properties["kind"])
	}
	if n.Properties["resolution"] != string(graph.ResolutionSemantic) {
		t.Errorf("resolution should be 'semantic', got %v", n.Properties["resolution"])
	}

	// Private symbol must NOT be a boundary.
	priv := graph.SymbolNode("internalHelper", "go", graph.KindFunction, graph.VisPrivate, graph.ResolutionSemantic)
	if priv.Properties["boundary"] != false {
		t.Errorf("private symbol should have boundary=false")
	}
}

func TestBoundarySymbolsRoundtrip(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t)

	if err := c.SchemaInit(ctx); err != nil {
		t.Fatalf("SchemaInit: %v", err)
	}

	// Create one public + one private symbol.
	pub := graph.SymbolNode("ExportedFunc", "go", graph.KindFunction, graph.VisPublic, graph.ResolutionSemantic)
	priv := graph.SymbolNode("internalFunc", "go", graph.KindFunction, graph.VisPrivate, graph.ResolutionSemantic)

	if err := c.CreateNode(ctx, pub); err != nil {
		t.Fatalf("CreateNode pub: %v", err)
	}
	if err := c.CreateNode(ctx, priv); err != nil {
		t.Fatalf("CreateNode priv: %v", err)
	}
	t.Cleanup(func() {
		_ = c.DeleteNode(ctx, pub.ID)
		_ = c.DeleteNode(ctx, priv.ID)
	})

	boundaries, err := c.BoundarySymbols(ctx)
	if err != nil {
		t.Fatalf("BoundarySymbols: %v", err)
	}

	// Must contain ExportedFunc, must not contain internalFunc.
	found := false
	for _, n := range boundaries {
		if n.Properties["name"] == "internalFunc" {
			t.Errorf("private symbol leaked into boundary set")
		}
		if n.Properties["name"] == "ExportedFunc" {
			found = true
		}
	}
	if !found {
		t.Errorf("ExportedFunc not in boundary symbols (got %d nodes)", len(boundaries))
	}
}

func TestFileAndPackageNodes(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t)

	file := graph.FileNode("internal/graph/client.go", "go", "abc123", graph.ResolutionSemantic)
	pkg := graph.PackageNode("graph", "go", "github.com/gurkangul/gg-cli/internal/graph")

	if err := c.CreateNode(ctx, file); err != nil {
		t.Fatalf("CreateNode file: %v", err)
	}
	if err := c.CreateNode(ctx, pkg); err != nil {
		t.Fatalf("CreateNode pkg: %v", err)
	}
	t.Cleanup(func() {
		_ = c.DeleteNode(ctx, file.ID)
		_ = c.DeleteNode(ctx, pkg.ID)
	})

	if file.Label != graph.LabelFile {
		t.Errorf("file label: want %s, got %s", graph.LabelFile, file.Label)
	}
	if pkg.Label != graph.LabelPackage {
		t.Errorf("pkg label: want %s, got %s", graph.LabelPackage, pkg.Label)
	}
}
