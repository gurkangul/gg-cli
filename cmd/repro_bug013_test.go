package cmd

import (
	"context"
	"testing"

	"github.com/gurkangul/gg-cli/internal/graph"
)

// TestGraphHandler_OnReference_PopulatesPendingRefs guards BUG-013: before the
// fix, OnReference did nothing — IMPORTS edges were never written because no
// references were collected. After the fix, OnReference appends to pendingRefs
// so flushRefs() can resolve and write IMPORTS edges.
func TestGraphHandler_OnReference_PopulatesPendingRefs(t *testing.T) {
	h := &graphHandler{
		fileNodeByPath:  make(map[string]*graph.Node),
		scipToFile:      make(map[string]string),
		seenImportEdges: make(map[string]bool),
	}
	fromFile := &graph.Node{ID: "file-node-abc", Label: graph.LabelFile}
	sym := "scip-go gomod example.com/pkg/foo/`Foo#`"

	if err := h.OnReference(context.Background(), fromFile, sym); err != nil {
		t.Fatalf("OnReference returned unexpected error: %v", err)
	}
	if len(h.pendingRefs) != 1 {
		t.Fatalf("pendingRefs len = %d; want 1 (BUG-013: refs must be collected)", len(h.pendingRefs))
	}
	if h.pendingRefs[0].fromFileID != fromFile.ID {
		t.Errorf("pendingRefs[0].fromFileID = %q; want %q", h.pendingRefs[0].fromFileID, fromFile.ID)
	}
	if h.pendingRefs[0].scipSymbol != sym {
		t.Errorf("pendingRefs[0].scipSymbol = %q; want %q", h.pendingRefs[0].scipSymbol, sym)
	}
}
