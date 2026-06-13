package graph

import (
	"context"
	"os"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

// newSQLiteClient builds a Client backed by the embedded SQLite graph store in a
// fresh temp directory. It exercises the TASK-494 backend directly (no live
// Memgraph), validating the Cypher-template → SQL dispatch end-to-end through the
// public Client API.
func newSQLiteClient(t *testing.T, projectID string) *Client {
	t.Helper()
	t.Setenv(GraphBackendEnv, "sqlite")
	cfg := &config.MemgraphConfig{DataDir: t.TempDir()}
	c, err := New(cfg, projectID)
	if err != nil {
		t.Fatalf("New(sqlite): %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// TestSQLite_NodeCRUD covers create / find / delete and the project_id guard.
func TestSQLite_NodeCRUD(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-crud")

	n := &Node{Label: "Symbol", Properties: map[string]any{"name": "Foo", "lang": "go"}}
	if err := c.CreateNode(ctx, n); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if n.ID == "" {
		t.Fatal("expected non-empty ID after CreateNode")
	}

	found, err := c.FindNodeByProperty(ctx, "Symbol", "name", "Foo")
	if err != nil {
		t.Fatalf("FindNodeByProperty: %v", err)
	}
	if found == nil || found.Properties["lang"] != "go" {
		t.Fatalf("expected to find node with lang=go, got %+v", found)
	}
	// project_id must be stamped on the node.
	if found.Properties["project_id"] != "proj-crud" {
		t.Errorf("expected project_id=proj-crud, got %v", found.Properties["project_id"])
	}

	if err := c.DeleteNode(ctx, n.ID); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	gone, err := c.FindNodeByProperty(ctx, "Symbol", "name", "Foo")
	if err != nil {
		t.Fatalf("FindNodeByProperty after delete: %v", err)
	}
	if gone != nil {
		t.Errorf("expected nil after delete, got %+v", gone)
	}
}

// TestSQLite_UpsertIsIdempotent verifies MERGE-by-key does not duplicate nodes.
func TestSQLite_UpsertIsIdempotent(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-upsert")

	for i := 0; i < 3; i++ {
		n := &Node{Label: "File", Properties: map[string]any{"path": "a.go", "checksum": "v"}}
		if err := c.UpsertNode(ctx, n, []string{"path"}); err != nil {
			t.Fatalf("UpsertNode #%d: %v", i, err)
		}
	}
	count, err := c.CountFileNodes(ctx)
	if err != nil {
		t.Fatalf("CountFileNodes: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 File node after 3 upserts, got %d", count)
	}
}

// TestSQLite_CrossProjectIsolation verifies that two projects sharing one
// embedded DB never see each other's nodes.
func TestSQLite_CrossProjectIsolation(t *testing.T) {
	ctx := context.Background()
	t.Setenv(GraphBackendEnv, "sqlite")
	dir := t.TempDir()
	cA, err := New(&config.MemgraphConfig{DataDir: dir}, "proj-A")
	if err != nil {
		t.Fatalf("New A: %v", err)
	}
	t.Cleanup(func() { _ = cA.Close(ctx) })
	cB, err := New(&config.MemgraphConfig{DataDir: dir}, "proj-B")
	if err != nil {
		t.Fatalf("New B: %v", err)
	}
	t.Cleanup(func() { _ = cB.Close(ctx) })

	if err := cA.CreateNode(ctx, &Node{Label: "Sym", Properties: map[string]any{"name": "onlyA"}}); err != nil {
		t.Fatalf("CreateNode A: %v", err)
	}
	got, err := cB.FindNodeByProperty(ctx, "Sym", "name", "onlyA")
	if err != nil {
		t.Fatalf("B FindNodeByProperty: %v", err)
	}
	if got != nil {
		t.Errorf("cross-project leak: B found A's node %+v", got)
	}
}

// TestSQLite_CallFlowSingleHop covers the CALLS neighbor lookup and the Go-side
// BFS that powers symbol call-flow.
func TestSQLite_CallFlowSingleHop(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-flow")

	a := &Node{Label: "Symbol", Properties: map[string]any{"name": "A", "source_file": "a.go"}}
	b := &Node{Label: "Symbol", Properties: map[string]any{"name": "B", "source_file": "b.go"}}
	d := &Node{Label: "Symbol", Properties: map[string]any{"name": "D", "source_file": "d.go"}}
	for _, n := range []*Node{a, b, d} {
		if err := c.CreateNode(ctx, n); err != nil {
			t.Fatalf("CreateNode %v: %v", n.Properties["name"], err)
		}
	}
	if err := c.CreateEdge(ctx, &Edge{FromID: a.ID, ToID: b.ID, Type: RelCalls}); err != nil {
		t.Fatalf("CreateEdge A->B: %v", err)
	}
	if err := c.CreateEdge(ctx, &Edge{FromID: b.ID, ToID: d.ID, Type: RelCalls}); err != nil {
		t.Fatalf("CreateEdge B->D: %v", err)
	}

	syms, err := c.FindSymbols(ctx, "A")
	if err != nil || len(syms) != 1 {
		t.Fatalf("FindSymbols A: %v (len=%d)", err, len(syms))
	}
	flow, err := c.ForwardCallFlow(ctx, syms[0], 2)
	if err != nil {
		t.Fatalf("ForwardCallFlow: %v", err)
	}
	// A->B at depth 1, B->D at depth 2.
	if len(flow.Edges) != 2 {
		t.Fatalf("expected 2 call-flow edges, got %d: %+v", len(flow.Edges), flow.Edges)
	}
}

// TestSQLite_BugAffectsAndImpact covers the bugs-affecting-file feature that
// `gg impact` relies on.
func TestSQLite_BugAffectsAndImpact(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-bug")

	if err := c.MergeBugAffects(ctx, "BUG-001", "leak", []string{"internal/x.go"}, nil); err != nil {
		t.Fatalf("MergeBugAffects: %v", err)
	}
	refs, err := c.BugsAffectingFile(ctx, "internal/x.go")
	if err != nil {
		t.Fatalf("BugsAffectingFile: %v", err)
	}
	if len(refs) != 1 || refs[0].BugID != "BUG-001" {
		t.Fatalf("expected [BUG-001], got %+v", refs)
	}
	files, _, err := c.BugAffects(ctx, "BUG-001")
	if err != nil {
		t.Fatalf("BugAffects: %v", err)
	}
	if len(files) != 1 || files[0] != "internal/x.go" {
		t.Errorf("expected [internal/x.go], got %v", files)
	}
}

// TestSQLite_DependentsOf covers the single-hop IMPORTS dependent lookup that
// the --changed invalidation closure uses.
func TestSQLite_DependentsOf(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-dep")

	dep := &Node{Label: "File", Properties: map[string]any{"path": "caller.go"}}
	target := &Node{Label: "File", Properties: map[string]any{"path": "lib.go"}}
	for _, n := range []*Node{dep, target} {
		if err := c.UpsertNode(ctx, n, []string{"path"}); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}
	if err := c.CreateEdge(ctx, &Edge{FromID: dep.ID, ToID: target.ID, Type: RelImports}); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	deps, err := c.DependentsOf(ctx, "lib.go")
	if err != nil {
		t.Fatalf("DependentsOf: %v", err)
	}
	if len(deps) != 1 || deps[0] != "caller.go" {
		t.Errorf("expected [caller.go], got %v", deps)
	}
}

// TestSQLite_ImportEdgesFromJSONL verifies the embedded store can be populated
// through the same edges.jsonl import path Memgraph uses (gg brain import).
func TestSQLite_ImportEdgesFromJSONL(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-import")

	// Seed the two File nodes the edge references (import is by stable domain key).
	for _, p := range []string{"a.go", "b.go"} {
		n := &Node{Label: "File", Properties: map[string]any{"path": p}}
		if err := c.UpsertNode(ctx, n, []string{"path"}); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}
	edgesPath := writeEdgesJSONL(t, `{"src":"file:a.go","dst":"file:b.go","type":"IMPORTS","properties":{}}`)
	imported, skipped, err := c.ImportEdges(ctx, edgesPath)
	if err != nil {
		t.Fatalf("ImportEdges: %v", err)
	}
	if imported != 1 || skipped != 0 {
		t.Fatalf("expected imported=1 skipped=0, got imported=%d skipped=%d", imported, skipped)
	}
	deps, err := c.DependentsOf(ctx, "b.go")
	if err != nil {
		t.Fatalf("DependentsOf: %v", err)
	}
	if len(deps) != 1 || deps[0] != "a.go" {
		t.Errorf("expected [a.go] after import, got %v", deps)
	}
}

// writeEdgesJSONL writes a one-line edges.jsonl fixture and returns its path.
func writeEdgesJSONL(t *testing.T, line string) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/edges.jsonl"
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write edges.jsonl: %v", err)
	}
	return path
}
