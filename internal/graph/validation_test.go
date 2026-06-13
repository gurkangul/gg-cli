// Package graph — unit tests for input validation in crud.go and client.go.
//
// These tests verify the guard clauses that run BEFORE any Memgraph network
// call. They create a Client pointing at bolt://localhost:1 (nothing listening)
// and confirm that early-return validation errors are returned without ever
// reaching the driver. neo4j.NewDriverWithContext is a lazy connector and
// does not dial on construction.
package graph

import (
	"context"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

// newValidationClient creates a Client wired to an unreachable bolt URI.
// Use only for tests that exercise validation guards (before runQuery).
//
// This file tests the Memgraph/Bolt path specifically (lazy connector, guard
// clauses, fail-fast-on-unreachable). The default backend is pinned to memgraph
// regardless of ambient GG_GRAPH_BACKEND so the always-up embedded SQLite store
// (TASK-494) does not change the "Memgraph down ⇒ error" contract these tests
// assert — mirroring the TASK-493 cmd-suite backend pin.
func newValidationClient(t *testing.T) *Client {
	t.Helper()
	t.Setenv(GraphBackendEnv, "memgraph")
	cfg := &config.MemgraphConfig{URI: "bolt://localhost:1"}
	c, err := New(cfg, "test-project-validation")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// ── New() validation ─────────────────────────────────────────────────────────

func TestNew_EmptyURI_Error(t *testing.T) {
	// Empty URI is only an error on the Memgraph/Bolt path; the embedded SQLite
	// backend ignores the URI. Pin memgraph so this guard is exercised.
	t.Setenv(GraphBackendEnv, "memgraph")
	cfg := &config.MemgraphConfig{URI: ""}
	_, err := New(cfg, "some-project")
	if err == nil {
		t.Fatal("expected error for empty URI, got nil")
	}
}

func TestNew_EmptyProjectID_Error(t *testing.T) {
	cfg := &config.MemgraphConfig{URI: "bolt://localhost:7687"}
	_, err := New(cfg, "")
	if err == nil {
		t.Fatal("expected error for empty projectID, got nil")
	}
}

func TestNew_NoAuth_Succeeds(t *testing.T) {
	// Both username and password empty → NoAuth() path (Memgraph-specific).
	t.Setenv(GraphBackendEnv, "memgraph")
	cfg := &config.MemgraphConfig{URI: "bolt://localhost:1"}
	c, err := New(cfg, "test-no-auth")
	if err != nil {
		t.Fatalf("New with no auth: %v", err)
	}
	_ = c.Close(context.Background())
}

func TestNew_WithAuth_Succeeds(t *testing.T) {
	// Both fields set → BasicAuth() path (Memgraph-specific).
	t.Setenv(GraphBackendEnv, "memgraph")
	cfg := &config.MemgraphConfig{
		URI:      "bolt://localhost:1",
		Username: "user",
		Password: "pass",
	}
	c, err := New(cfg, "test-with-auth")
	if err != nil {
		t.Fatalf("New with auth: %v", err)
	}
	_ = c.Close(context.Background())
}

// ── CreateNode validation ─────────────────────────────────────────────────────

func TestCreateNode_EmptyLabel_Error(t *testing.T) {
	c := newValidationClient(t)
	n := &Node{Label: "", Properties: map[string]any{"name": "x"}}
	if err := c.CreateNode(context.Background(), n); err == nil {
		t.Fatal("expected error for empty label, got nil")
	}
}

// ── UpsertNode validation ─────────────────────────────────────────────────────

func TestUpsertNode_EmptyLabel_Error(t *testing.T) {
	c := newValidationClient(t)
	n := &Node{Label: "", Properties: map[string]any{"name": "x"}}
	if err := c.UpsertNode(context.Background(), n, []string{"name"}); err == nil {
		t.Fatal("expected error for empty label, got nil")
	}
}

func TestUpsertNode_NoMergeKeys_Error(t *testing.T) {
	c := newValidationClient(t)
	n := &Node{Label: "Symbol", Properties: map[string]any{"name": "x"}}
	if err := c.UpsertNode(context.Background(), n, nil); err == nil {
		t.Fatal("expected error for nil merge keys, got nil")
	}
}

func TestUpsertNode_EmptyMergeKeys_Error(t *testing.T) {
	c := newValidationClient(t)
	n := &Node{Label: "Symbol", Properties: map[string]any{"name": "x"}}
	if err := c.UpsertNode(context.Background(), n, []string{}); err == nil {
		t.Fatal("expected error for empty merge keys slice, got nil")
	}
}

func TestUpsertNode_MergeKeyNotInProps_Error(t *testing.T) {
	c := newValidationClient(t)
	n := &Node{Label: "Symbol", Properties: map[string]any{"name": "x"}}
	// "path" is not in properties — should fail before any network call.
	if err := c.UpsertNode(context.Background(), n, []string{"path"}); err == nil {
		t.Fatal("expected error for merge key not in properties, got nil")
	}
}

// ── CreateEdge validation ─────────────────────────────────────────────────────

func TestCreateEdge_EmptyFromID_Error(t *testing.T) {
	c := newValidationClient(t)
	e := &Edge{FromID: "", ToID: "b", Type: "CALLS"}
	if err := c.CreateEdge(context.Background(), e); err == nil {
		t.Fatal("expected error for empty FromID, got nil")
	}
}

func TestCreateEdge_EmptyToID_Error(t *testing.T) {
	c := newValidationClient(t)
	e := &Edge{FromID: "a", ToID: "", Type: "CALLS"}
	if err := c.CreateEdge(context.Background(), e); err == nil {
		t.Fatal("expected error for empty ToID, got nil")
	}
}

func TestCreateEdge_EmptyType_Error(t *testing.T) {
	c := newValidationClient(t)
	e := &Edge{FromID: "a", ToID: "b", Type: ""}
	if err := c.CreateEdge(context.Background(), e); err == nil {
		t.Fatal("expected error for empty Type, got nil")
	}
}

// ── UpsertEdge validation ─────────────────────────────────────────────────────

func TestUpsertEdge_EmptyFromID_Error(t *testing.T) {
	c := newValidationClient(t)
	e := &Edge{FromID: "", ToID: "b", Type: "IMPORTS"}
	if err := c.UpsertEdge(context.Background(), e); err == nil {
		t.Fatal("expected error for empty FromID, got nil")
	}
}

func TestUpsertEdge_EmptyToID_Error(t *testing.T) {
	c := newValidationClient(t)
	e := &Edge{FromID: "a", ToID: "", Type: "IMPORTS"}
	if err := c.UpsertEdge(context.Background(), e); err == nil {
		t.Fatal("expected error for empty ToID, got nil")
	}
}

func TestUpsertEdge_EmptyType_Error(t *testing.T) {
	c := newValidationClient(t)
	e := &Edge{FromID: "a", ToID: "b", Type: ""}
	if err := c.UpsertEdge(context.Background(), e); err == nil {
		t.Fatal("expected error for empty Type, got nil")
	}
}

// ── DeleteNode validation ─────────────────────────────────────────────────────

func TestDeleteNode_EmptyElementID_Error(t *testing.T) {
	c := newValidationClient(t)
	if err := c.DeleteNode(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty elementID, got nil")
	}
}

// ── schema constants ──────────────────────────────────────────────────────────

func TestLabelConstants(t *testing.T) {
	if LabelSymbol == "" || LabelFile == "" || LabelPackage == "" {
		t.Error("label constants must not be empty")
	}
	// Must be distinct.
	if LabelSymbol == LabelFile || LabelFile == LabelPackage {
		t.Error("label constants must be distinct")
	}
}

func TestRelationshipConstants(t *testing.T) {
	rels := []string{RelDefines, RelContains, RelCalls, RelImports}
	seen := make(map[string]bool)
	for _, r := range rels {
		if r == "" {
			t.Error("relationship constant must not be empty")
		}
		if seen[r] {
			t.Errorf("duplicate relationship constant: %q", r)
		}
		seen[r] = true
	}
}

func TestResolutionTierValues(t *testing.T) {
	if ResolutionSemantic == "" || ResolutionSyntactic == "" {
		t.Error("resolution tier constants must not be empty")
	}
	if ResolutionSemantic == ResolutionSyntactic {
		t.Error("resolution tier constants must be distinct")
	}
}

// ── Methods that reach the network (fail fast on unreachable Memgraph) ────────
// These verify that the method bodies run to the point of a network call and
// return a non-nil error. bolt://localhost:1 has nothing listening, so the
// driver returns connection refused quickly.

func TestHealthCheck_Unreachable(t *testing.T) {
	c := newValidationClient(t)
	err := c.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFindNodeByProperty_Unreachable(t *testing.T) {
	c := newValidationClient(t)
	_, err := c.FindNodeByProperty(context.Background(), "Symbol", "name", "foo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestInvalidateFile_Unreachable(t *testing.T) {
	c := newValidationClient(t)
	err := c.InvalidateFile(context.Background(), "internal/store/client.go")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDependentsOf_Unreachable(t *testing.T) {
	c := newValidationClient(t)
	_, err := c.DependentsOf(context.Background(), "internal/store/client.go")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFileSymbols_Unreachable(t *testing.T) {
	c := newValidationClient(t)
	_, err := c.FileSymbols(context.Background(), "internal/store/client.go")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSweepProject_Unreachable(t *testing.T) {
	c := newValidationClient(t)
	err := c.SweepProject(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSchemaInit_Unreachable(t *testing.T) {
	c := newValidationClient(t)
	err := c.SchemaInit(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBoundarySymbols_Unreachable(t *testing.T) {
	c := newValidationClient(t)
	_, err := c.BoundarySymbols(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── SymbolNode boundary logic ─────────────────────────────────────────────────

func TestSymbolNode_PublicBoundaryTrue(t *testing.T) {
	n := SymbolNode("NewClient", "go", KindFunction, VisPublic, ResolutionSemantic)
	if n.Properties["boundary"] != true {
		t.Errorf("public symbol must have boundary=true, got %v", n.Properties["boundary"])
	}
	if n.Properties["visibility"] != "public" {
		t.Errorf("visibility: got %v", n.Properties["visibility"])
	}
}

func TestSymbolNode_PrivateBoundaryFalse(t *testing.T) {
	n := SymbolNode("internalHelper", "go", KindFunction, VisPrivate, ResolutionSyntactic)
	if n.Properties["boundary"] != false {
		t.Errorf("private symbol must have boundary=false, got %v", n.Properties["boundary"])
	}
}

func TestSymbolNode_AllKinds(t *testing.T) {
	kinds := []SymbolKind{KindFunction, KindMethod, KindType, KindVariable, KindConstant, KindInterface}
	for _, k := range kinds {
		n := SymbolNode("sym", "go", k, VisPublic, ResolutionSemantic)
		if n.Properties["kind"] != string(k) {
			t.Errorf("kind %q: got %v", k, n.Properties["kind"])
		}
	}
}
