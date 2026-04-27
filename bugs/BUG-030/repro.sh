#!/bin/sh
# BUG-030: brain-write verbs hard-fail when Qdrant is down — no JSONL fallback.
# This repro verifies that AddDecision with a dead Qdrant client returns a non-error
# (write succeeds via JSONL) rather than propagating ErrQdrantDown.
#
# Pre-fix (a283566): AddDecision returns ErrQdrantDown → repro exits 1 (bug present).
# Post-fix (2014261): AddDecision appends to JSONL and returns nil → repro exits 0.
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

// TestBUG030_AddDecision_QdrantDown_MustNotPropagateErr proves that AddDecision
// does NOT propagate a raw ErrQdrantDown when Qdrant is unreachable.
// Pre-fix (a283566): AddDecision returned a bare ErrQdrantDown — write is permanently lost.
// Post-fix (2014261): AddDecision writes to JSONL first, then returns OutboxQueued
//   (soft signal: write is durable, Qdrant upsert is queued). ErrQdrantDown is still
//   in the error chain but wrapped inside OutboxQueued — the caller can exit 0.
func TestBUG030_AddDecision_QdrantDown_MustNotPropagateErr(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.AddDecision(ctx, Decision{
		Text:   "use JWT for auth",
		Reason: "stateless, portable",
		Tags:   []string{"auth", "security"},
	}, make([]float32, VectorSize))

	// Post-fix: must be nil or OutboxQueued. Never a bare ErrQdrantDown.
	// Check: error must either be nil, or be (or wrap) OutboxQueued.
	if err == nil {
		return // clean success — acceptable
	}
	var oq *OutboxQueued
	if errors.As(err, &oq) {
		return // OutboxQueued — write was accepted, queued for replay — acceptable
	}
	t.Fatalf("BUG-030: AddDecision returned unexpected error (neither nil nor OutboxQueued): %v", err)
}
EOF

cd "$repo_root"
go test ./internal/store/ -run TestBUG030_AddDecision_QdrantDown_MustNotPropagateErr -count=1 -timeout=30s
