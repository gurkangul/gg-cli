//go:build integration

package graph_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/gurkangul/gg/internal/config"
	"github.com/gurkangul/gg/internal/graph"
)

// Run with: go test -tags integration ./internal/graph/...
// Requires Memgraph running on bolt://localhost:7687 (docker-compose up memgraph).

func newTestClient(t *testing.T) *graph.Client {
	t.Helper()
	cfg := &config.MemgraphConfig{
		URI:      "bolt://localhost:7687",
		Username: "",
		Password: "",
	}
	// Each test run uses a unique project ID to avoid cross-test data leakage.
	projectID := fmt.Sprintf("test-%s", t.Name())
	c, err := graph.New(cfg, projectID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { c.Close(context.Background()) })
	return c
}

func TestHealthCheck(t *testing.T) {
	c := newTestClient(t)
	if err := c.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestCreateFindDeleteNode(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t)

	// CREATE
	n := &graph.Node{
		Label: "TestSymbol",
		Properties: map[string]any{
			"name": "gg_bootstrap_test",
			"lang": "go",
		},
	}
	if err := c.CreateNode(ctx, n); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if n.ID == "" {
		t.Fatal("expected non-empty ID after CreateNode")
	}
	t.Logf("created node id=%s", n.ID)

	// MATCH
	found, err := c.FindNodeByProperty(ctx, "TestSymbol", "name", "gg_bootstrap_test")
	if err != nil {
		t.Fatalf("FindNodeByProperty: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find node, got nil")
	}
	if found.Properties["lang"] != "go" {
		t.Errorf("unexpected lang=%v", found.Properties["lang"])
	}

	// DELETE
	if err := c.DeleteNode(ctx, n.ID); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}

	// confirm gone
	gone, err := c.FindNodeByProperty(ctx, "TestSymbol", "name", "gg_bootstrap_test")
	if err != nil {
		t.Fatalf("FindNodeByProperty after delete: %v", err)
	}
	if gone != nil {
		t.Errorf("expected nil after delete, got id=%s", gone.ID)
	}
}

func TestCreateEdge(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t)

	a := &graph.Node{Label: "TestSym", Properties: map[string]any{"name": "caller"}}
	b := &graph.Node{Label: "TestSym", Properties: map[string]any{"name": "callee"}}

	if err := c.CreateNode(ctx, a); err != nil {
		t.Fatalf("CreateNode a: %v", err)
	}
	if err := c.CreateNode(ctx, b); err != nil {
		t.Fatalf("CreateNode b: %v", err)
	}
	t.Cleanup(func() {
		_ = c.DeleteNode(ctx, a.ID)
		_ = c.DeleteNode(ctx, b.ID)
	})

	edge := &graph.Edge{
		FromID:     a.ID,
		ToID:       b.ID,
		Type:       "CALLS",
		Properties: map[string]any{"line": 42},
	}
	if err := c.CreateEdge(ctx, edge); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
}
