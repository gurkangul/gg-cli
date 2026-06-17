package graph

import (
	"testing"
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

// NOTE: TestUpsertKnowledgeEdge_NoPanicOnBadConn and TestTaskSiblings_NoPanicOnBadConn
// were removed: they asserted the Memgraph-down ⇒ error contract, which no longer
// exists. The embedded SQLite graph store is always reachable, so those operations
// succeed instead of erroring. Their query structure is exercised by the embedded
// CRUD/traversal tests in graphstore_sqlite_test.go.
