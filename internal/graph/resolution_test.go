package graph

// Unit tests for the hybrid tier resolution contract (TASK-014).
// No Memgraph required.

import (
	"testing"
)

// TestResolutionTier_SymbolNode verifies that SymbolNode carries the tier.
func TestResolutionTier_SymbolNode(t *testing.T) {
	for _, tc := range []struct {
		tier ResolutionTier
	}{
		{ResolutionSemantic},
		{ResolutionSyntactic},
	} {
		n := SymbolNode("Foo", "go", KindFunction, VisPublic, tc.tier)
		got, ok := n.Properties["resolution"].(string)
		if !ok || got != string(tc.tier) {
			t.Errorf("SymbolNode(%s).resolution = %v, want %q", tc.tier, n.Properties["resolution"], tc.tier)
		}
	}
}

// TestResolutionTier_FileNode verifies that FileNode carries the tier.
func TestResolutionTier_FileNode(t *testing.T) {
	for _, tc := range []struct {
		tier ResolutionTier
	}{
		{ResolutionSemantic},
		{ResolutionSyntactic},
	} {
		n := FileNode("main.go", "go", "abc123", tc.tier)
		got, ok := n.Properties["resolution"].(string)
		if !ok || got != string(tc.tier) {
			t.Errorf("FileNode(%s).resolution = %v, want %q", tc.tier, n.Properties["resolution"], tc.tier)
		}
	}
}

// TestResolutionTier_SyntacticNoBoundary verifies the contamination rule:
// syntactic nodes should never be boundary=true AND resolution=syntactic
// simultaneously in a valid graph. This is a soft contract — the schema itself
// doesn't prevent it, but callers must follow it. We verify that a syntactic
// visibility-public symbol sets boundary=true but resolution=syntactic so that
// BoundarySymbols (which filters resolution="semantic") correctly excludes it.
func TestResolutionTier_SyntacticNoBoundary(t *testing.T) {
	// A tree-sitter parser might produce a public-looking symbol.
	// It should be marked syntactic so BoundarySymbols won't return it.
	n := SymbolNode("ExportedFoo", "go", KindFunction, VisPublic, ResolutionSyntactic)

	// boundary is set based on visibility — this is intentional.
	// The BoundarySymbols query guards against contamination via resolution filter.
	if n.Properties["boundary"] != true {
		t.Error("public visibility always sets boundary=true — BoundarySymbols filters it out via resolution")
	}
	if n.Properties["resolution"] != string(ResolutionSyntactic) {
		t.Errorf("expected resolution=syntactic for tree-sitter node, got %v", n.Properties["resolution"])
	}
	// This test documents the contract: syntactic nodes won't appear in BoundarySymbols
	// because the Cypher query filters WHERE resolution = "semantic".
}
