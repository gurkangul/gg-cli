#!/bin/sh
set -eu

# Repro for BUG-105: gg impact returned an authoritative-looking "0 deps 0 sym"
# for a file that is ABSENT from the code graph (build-tagged and excluded from
# the host-platform index, or otherwise skipped by the indexer), indistinguishable
# from a genuine leaf. The git-sha freshness contract reports "fresh" either way,
# so the empty blast radius looked trustworthy but was a false negative — exactly
# what CLAUDE.md tells agents to rely on gg impact to avoid.
#
# The fix adds graph.Client.FileNodeExists so impact can tell "0 because leaf"
# from "0 because not a graph node" and emit a loud, compact-visible warning.
#
# This repro guards the load-bearing new primitive hermetically: it drives the
# real sqlite graph store (client -> runQuery -> runRead -> countQuery ->
# countFileByPath), asserting a present path resolves true and an absent path
# resolves false. At the broken ref FileNodeExists does not exist, so the probe
# fails to compile — a valid pre-fix failure. It writes a throwaway *_test.go and
# removes it on exit, exactly like the other *_test.go repros.

cat > internal/graph/bug105_repro_test.go <<'GO'
package graph

import (
	"context"
	"testing"
)

// TestBUG105FileNodeExists is the BUG-105 regression guard: the graph must be
// able to answer "is this exact file a node?" so gg impact can distinguish a
// genuine 0-dependent leaf from a file absent from the graph.
func TestBUG105FileNodeExists(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-bug105")

	present := &Node{Label: "File", Properties: map[string]any{"path": "present.go"}}
	if err := c.UpsertNode(ctx, present, []string{"path"}); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	ok, err := c.FileNodeExists(ctx, "present.go")
	if err != nil {
		t.Fatalf("FileNodeExists(present): %v", err)
	}
	if !ok {
		t.Fatal("BUG-105: FileNodeExists returned false for a path that IS a File node — impact cannot trust a positive")
	}

	// A file the indexer never added (build-tagged / skipped). Pre-fix nothing
	// could ask this, so impact rendered it identically to a leaf.
	absent, err := c.FileNodeExists(ctx, "absent_build_tagged.go")
	if err != nil {
		t.Fatalf("FileNodeExists(absent): %v", err)
	}
	if absent {
		t.Fatal("BUG-105: FileNodeExists returned true for a path NOT in the graph — false negatives would look authoritative")
	}

	// Cross-check the path scoping: a different present path must not leak.
	other, err := c.FileNodeExists(ctx, "present.go.bak")
	if err != nil {
		t.Fatalf("FileNodeExists(near-miss): %v", err)
	}
	if other {
		t.Fatal("BUG-105: FileNodeExists matched a near-miss path — the query is not exact")
	}
}
GO

trap 'rm -f internal/graph/bug105_repro_test.go' EXIT

go test ./internal/graph -run TestBUG105FileNodeExists -count=1
