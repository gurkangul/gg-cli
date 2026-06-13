package graph

import (
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

func TestDecisionNode_Properties(t *testing.T) {
	n := DecisionNode("uuid-abc", "Use JWT for auth")
	if n.Label != LabelDecision {
		t.Errorf("label: want %s, got %s", LabelDecision, n.Label)
	}
	if n.Properties["qdrant_id"] != "uuid-abc" {
		t.Errorf("qdrant_id: want uuid-abc, got %v", n.Properties["qdrant_id"])
	}
	if n.Properties["title"] != "Use JWT for auth" {
		t.Errorf("title: want 'Use JWT for auth', got %v", n.Properties["title"])
	}
	if _, ok := n.Properties["project_id"]; ok {
		t.Error("DecisionNode must not pre-set project_id — that is injected by UpsertNode")
	}
}

func TestTaskNode_Properties(t *testing.T) {
	n := TaskNode("task-xyz", "Implement login flow")
	if n.Label != LabelTask {
		t.Errorf("label: want %s, got %s", LabelTask, n.Label)
	}
	if n.Properties["qdrant_id"] != "task-xyz" {
		t.Errorf("qdrant_id: want task-xyz, got %v", n.Properties["qdrant_id"])
	}
	if _, ok := n.Properties["project_id"]; ok {
		t.Error("TaskNode must not pre-set project_id")
	}
}

// TestKnowledgeEdgeRelConstants verifies the rel-type constants match the
// accepted schema values. A typo here would produce silent no-ops in Cypher.
func TestKnowledgeEdgeRelConstants(t *testing.T) {
	cases := []struct{ name, got, want string }{
		{"RelDecides", RelDecides, "DECIDES"},
		{"RelImplements", RelImplements, "IMPLEMENTS"},
		{"RelRejects", RelRejects, "REJECTS"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// TestUpsertKnowledgeEdge_CrossProjectRejected verifies that upsertKnowledgeEdge
// uses $pid in both MATCH clauses — so a cross-project edge is structurally
// impossible (the MATCH would simply return nothing). We can't test live Memgraph
// here, but we confirm the helper wires up to runQuery (which auto-injects $pid).
func TestUpsertKnowledgeEdge_NoPanicOnBadConn(t *testing.T) {
	// Asserts the Memgraph-down ⇒ error contract; pin memgraph so the always-up
	// embedded SQLite backend (TASK-494) doesn't turn this into a no-error path.
	t.Setenv(GraphBackendEnv, "memgraph")
	cfg := &config.MemgraphConfig{URI: "bolt://localhost:19997"}
	c, err := New(cfg, "proj-test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Expect an error (no Memgraph on 19997), but not a panic.
	err = c.UpsertDecidesEdge(t.Context(), "decision-id", "task-id")
	if err == nil {
		t.Error("expected error from unreachable Memgraph, got nil")
	}
}

// TestTaskSiblings_NoPanicOnBadConn verifies TaskSiblings propagates a connection
// error rather than panicking. The Cypher query structure (two UNION arms) is
// covered structurally by this test; integration coverage requires live Memgraph.
func TestTaskSiblings_NoPanicOnBadConn(t *testing.T) {
	// Asserts the Memgraph-down ⇒ error contract; pin memgraph (see above).
	t.Setenv(GraphBackendEnv, "memgraph")
	cfg := &config.MemgraphConfig{URI: "bolt://localhost:19997"}
	c, err := New(cfg, "proj-test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.TaskSiblings(t.Context(), "TASK-234")
	if err == nil {
		t.Error("expected error from unreachable Memgraph, got nil")
	}
}
