//go:build integration

package graph_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
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

// TestCrossProjectIsolation seeds 10 nodes in project A and 10 in project B,
// then verifies that each client only sees its own nodes — no cross-project leak.
func TestCrossProjectIsolation(t *testing.T) {
	ctx := context.Background()

	cfgA := &config.MemgraphConfig{URI: "bolt://localhost:7687"}
	cfgB := &config.MemgraphConfig{URI: "bolt://localhost:7687"}

	clientA, err := graph.New(cfgA, "test-isolation-project-A")
	if err != nil {
		t.Fatalf("New(A): %v", err)
	}
	t.Cleanup(func() {
		_ = clientA.SweepProject(ctx)
		_ = clientA.Close(ctx)
	})

	clientB, err := graph.New(cfgB, "test-isolation-project-B")
	if err != nil {
		t.Fatalf("New(B): %v", err)
	}
	t.Cleanup(func() {
		_ = clientB.SweepProject(ctx)
		_ = clientB.Close(ctx)
	})

	// Sweep any leftover state from a previous interrupted test run.
	_ = clientA.SweepProject(ctx)
	_ = clientB.SweepProject(ctx)

	// Seed 10 nodes into project A.
	for i := 0; i < 10; i++ {
		n := &graph.Node{
			Label:      "IsolationSymbol",
			Properties: map[string]any{"name": fmt.Sprintf("sym-a-%d", i), "owner": "A"},
		}
		if err := clientA.CreateNode(ctx, n); err != nil {
			t.Fatalf("CreateNode A[%d]: %v", i, err)
		}
	}

	// Seed 10 nodes into project B.
	for i := 0; i < 10; i++ {
		n := &graph.Node{
			Label:      "IsolationSymbol",
			Properties: map[string]any{"name": fmt.Sprintf("sym-b-%d", i), "owner": "B"},
		}
		if err := clientB.CreateNode(ctx, n); err != nil {
			t.Fatalf("CreateNode B[%d]: %v", i, err)
		}
	}

	// Query A: must see only A's nodes.
	for i := 0; i < 10; i++ {
		n, err := clientA.FindNodeByProperty(ctx, "IsolationSymbol", "name", fmt.Sprintf("sym-a-%d", i))
		if err != nil {
			t.Fatalf("A FindNodeByProperty sym-a-%d: %v", i, err)
		}
		if n == nil {
			t.Errorf("project A: expected to find sym-a-%d, got nil", i)
		}
	}

	// Query A for B's nodes: must return nil (no cross-project leak).
	for i := 0; i < 10; i++ {
		n, err := clientA.FindNodeByProperty(ctx, "IsolationSymbol", "name", fmt.Sprintf("sym-b-%d", i))
		if err != nil {
			t.Fatalf("A FindNodeByProperty sym-b-%d: %v", i, err)
		}
		if n != nil {
			t.Errorf("cross-project leak: project A found B's node sym-b-%d (id=%s)", i, n.ID)
		}
	}

	// Query B: must see only B's nodes.
	for i := 0; i < 10; i++ {
		n, err := clientB.FindNodeByProperty(ctx, "IsolationSymbol", "name", fmt.Sprintf("sym-b-%d", i))
		if err != nil {
			t.Fatalf("B FindNodeByProperty sym-b-%d: %v", i, err)
		}
		if n == nil {
			t.Errorf("project B: expected to find sym-b-%d, got nil", i)
		}
	}

	// Query B for A's nodes: must return nil.
	for i := 0; i < 10; i++ {
		n, err := clientB.FindNodeByProperty(ctx, "IsolationSymbol", "name", fmt.Sprintf("sym-a-%d", i))
		if err != nil {
			t.Fatalf("B FindNodeByProperty sym-a-%d: %v", i, err)
		}
		if n != nil {
			t.Errorf("cross-project leak: project B found A's node sym-a-%d (id=%s)", i, n.ID)
		}
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
