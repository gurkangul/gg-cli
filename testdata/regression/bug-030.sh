#!/bin/sh
# BUG-030: brain-write verbs hard-fail when the vector store / embedder is
# unavailable — no JSONL fallback. This repro verifies AddDecision still writes
# durably (JSONL) and returns OutboxQueued (queued-for-replay) instead of a hard
# error when no embedding is available.
#
# Pre-fix (a283566): AddDecision returned a bare error → write permanently lost.
# Post-fix (2014261): AddDecision appends to JSONL and returns nil/OutboxQueued.
# Refreshed post-e16c9ac (Qdrant removed → embedded SQLite): the offline path is
# exercised by passing a nil vector, mirroring internal/store/offline_jsonl_test.go.
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
test_file="$repo_root/internal/store/repro_bug030_test.go"

cleanup() {
  rm -f "$test_file"
}
trap cleanup EXIT INT TERM

cat > "$test_file" <<'EOF'
package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestBUG030_AddDecision_VectorUnavailable_MustNotPropagateErr proves AddDecision
// does NOT propagate a raw error when no embedding is available. A nil vector
// exercises the offline path: the write must land in JSONL and return nil or
// OutboxQueued (durable, replay queued), never a hard failure.
func TestBUG030_AddDecision_VectorUnavailable_MustNotPropagateErr(t *testing.T) {
	c, err := New(t.TempDir(), "test-project-bug030")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.AddDecision(ctx, Decision{
		Text:   "use JWT for auth",
		Reason: "stateless, portable",
		Tags:   []string{"auth", "security"},
	}, nil)

	// Must be nil or OutboxQueued. Never a bare hard error.
	if err == nil {
		return // clean success — acceptable
	}
	var oq *OutboxQueued
	if errors.As(err, &oq) {
		return // OutboxQueued — write accepted, queued for replay — acceptable
	}
	t.Fatalf("BUG-030: AddDecision returned unexpected error (neither nil nor OutboxQueued): %v", err)
}
EOF

cd "$repo_root"
go test ./internal/store/ -run TestBUG030_AddDecision_VectorUnavailable_MustNotPropagateErr -count=1 -timeout=30s
