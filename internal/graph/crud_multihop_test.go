package graph

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
)

func TestDependentsOfDepthIntegration(t *testing.T) {
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 with Memgraph running to exercise multi-hop traversal")
	}

	uri := os.Getenv("GG_TEST_MEMGRAPH_URI")
	if uri == "" {
		uri = "bolt://localhost:7687"
	}

	ctx := context.Background()
	c, err := New(&config.MemgraphConfig{URI: uri}, fmt.Sprintf("test-depth-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		_ = c.SweepProject(ctx)
		_ = c.Close(ctx)
	})
	if err := c.SweepProject(ctx); err != nil {
		t.Fatalf("SweepProject: %v", err)
	}

	target := FileNode("target.go", "go", "target", ResolutionSemantic)
	one := FileNode("one.go", "go", "one", ResolutionSemantic)
	two := FileNode("two.go", "go", "two", ResolutionSemantic)
	for _, n := range []*Node{target, one, two} {
		if err := c.CreateNode(ctx, n); err != nil {
			t.Fatalf("CreateNode %s: %v", n.Properties["path"], err)
		}
	}
	for _, edge := range []*Edge{
		{FromID: one.ID, ToID: target.ID, Type: "IMPORTS"},
		{FromID: two.ID, ToID: one.ID, Type: "IMPORTS"},
		{FromID: target.ID, ToID: two.ID, Type: "IMPORTS"},
	} {
		if err := c.CreateEdge(ctx, edge); err != nil {
			t.Fatalf("CreateEdge: %v", err)
		}
	}

	got, err := c.DependentsOfDepth(ctx, "target.go", 3)
	if err != nil {
		t.Fatalf("DependentsOfDepth: %v", err)
	}

	want := []DependentHop{{Path: "one.go", Hop: 1}, {Path: "two.go", Hop: 2}}
	if len(got.Dependents) != len(want) {
		t.Fatalf("dependents len=%d want %d: %#v", len(got.Dependents), len(want), got.Dependents)
	}
	for i := range want {
		if got.Dependents[i] != want[i] {
			t.Fatalf("dependent[%d]=%#v want %#v", i, got.Dependents[i], want[i])
		}
	}
	if len(got.Cycles) != 1 || got.Cycles[0] != "target.go" {
		t.Fatalf("cycles=%v want [target.go]", got.Cycles)
	}
	if got.Truncated {
		t.Fatal("depth 3 should not be truncated for the seeded graph")
	}

	shallow, err := c.DependentsOfDepth(ctx, "target.go", 1)
	if err != nil {
		t.Fatalf("DependentsOfDepth shallow: %v", err)
	}
	if !shallow.Truncated {
		t.Fatal("depth 1 should report truncation when another frontier remains")
	}
}
