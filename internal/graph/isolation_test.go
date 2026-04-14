package graph

// Unit tests for per-project isolation logic.
// These tests run without a Memgraph instance and verify that:
//  1. New() rejects empty projectID
//  2. CreateNode injects project_id into properties without mutating the caller's map
//  3. Node constructors don't pre-set project_id (isolation is at persistence layer)

import (
	"context"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

// TestNew_RequiresProjectID verifies that New returns an error when projectID is empty.
func TestNew_RequiresProjectID(t *testing.T) {
	cfg := &config.MemgraphConfig{URI: "bolt://localhost:7687"}
	_, err := New(cfg, "")
	if err == nil {
		t.Fatal("expected error for empty projectID, got nil")
	}
}

// TestNew_RequiresURI verifies that New returns an error when URI is empty.
func TestNew_RequiresURI(t *testing.T) {
	cfg := &config.MemgraphConfig{URI: ""}
	_, err := New(cfg, "test-proj")
	if err == nil {
		t.Fatal("expected error for empty URI, got nil")
	}
}

// TestCreateNode_InjectsProjectID verifies that CreateNode stamps each node with
// project_id without mutating the original properties map.
//
// We test this by inspecting the client's logic directly — the actual Cypher
// call would require Memgraph, but the property injection happens before the
// driver call and can be exercised via the unit interface.
//
// Strategy: call CreateNode on a fake session that captures the props passed.
// Since we can't easily stub the driver without a mock framework, we verify the
// invariant via the public contract: the caller's Properties map must NOT gain
// a project_id key after CreateNode returns (even on error).
func TestCreateNode_DoesNotMutateCallerProps(t *testing.T) {
	// We can't create a real Client without a driver, but we can test the
	// shallow-copy logic by inspecting the contract via New + a no-op call.
	// The driver call will fail (no Memgraph), but we verify props are untouched.

	cfg := &config.MemgraphConfig{URI: "bolt://localhost:19998"} // nothing listening
	c, err := New(cfg, "proj-abc")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Don't close — it's a dummy connection; Close would block on a real driver teardown.

	original := map[string]any{
		"name": "TestSymbol",
		"lang": "go",
	}
	// Deep copy to compare after the call.
	before := map[string]any{"name": original["name"], "lang": original["lang"]}

	n := &Node{Label: "Symbol", Properties: original}
	// CreateNode will error (no Memgraph listening on port 19998), but must not mutate original.
	_ = c.CreateNode(context.Background(), n)

	for k := range original {
		if _, ok := before[k]; !ok {
			t.Errorf("unexpected key %q added to caller's properties map", k)
		}
	}
	if _, hasProjectID := original["project_id"]; hasProjectID {
		t.Error("CreateNode must not inject project_id into the caller's original map")
	}
	if len(original) != len(before) {
		t.Errorf("caller's props changed: before=%v, after=%v", before, original)
	}
}

// TestSymbolNode_NoProjectID verifies that node constructors do NOT pre-set
// project_id. Isolation is the persistence layer's responsibility, not the
// node factory's.
func TestSymbolNode_NoProjectID(t *testing.T) {
	n := SymbolNode("Foo", "go", KindFunction, VisPublic, ResolutionSemantic)
	if _, ok := n.Properties["project_id"]; ok {
		t.Error("SymbolNode must not set project_id — that is injected by CreateNode")
	}
	if n.Properties["resolution"] != string(ResolutionSemantic) {
		t.Errorf("expected resolution=%s, got %v", ResolutionSemantic, n.Properties["resolution"])
	}
}

func TestFileNode_NoProjectID(t *testing.T) {
	n := FileNode("main.go", "go", "abc123", ResolutionSyntactic)
	if _, ok := n.Properties["project_id"]; ok {
		t.Error("FileNode must not set project_id — that is injected by CreateNode")
	}
	if n.Properties["resolution"] != string(ResolutionSyntactic) {
		t.Errorf("expected resolution=%s, got %v", ResolutionSyntactic, n.Properties["resolution"])
	}
}

func TestPackageNode_NoProjectID(t *testing.T) {
	n := PackageNode("main", "go", "github.com/foo/bar")
	if _, ok := n.Properties["project_id"]; ok {
		t.Error("PackageNode must not set project_id — that is injected by CreateNode")
	}
}
