// Package store — BUG-094 batched-archive tests for ArchiveAgentBroadcasts.
package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/brain"
)

// newArchiveTestClient builds a real Client backed by the embedded SQLite store
// in a temp dir — no network, no live vector backend. Used by the batched-archive
// tests so they exercise the real JSONL + store mutation path.
func newArchiveTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(t.TempDir(), "archive-test")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.EnsureCollections(context.Background(), uint64(VectorSize)); err != nil {
		t.Fatalf("EnsureCollections: %v", err)
	}
	return c
}

// sendBackdatedBroadcast sends an audience=agents broadcast with an explicit
// created_at so tests can place a message before/after the archive cutoff.
func sendBackdatedBroadcast(t *testing.T, c *Client, id, content string, at time.Time) {
	t.Helper()
	if err := c.SendMessage(context.Background(), Message{
		ID:        id,
		FromRole:  "claude-code",
		ToRole:    "all",
		Content:   content,
		Audience:  "agents",
		CreatedAt: at.UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SendMessage(%s): %v", id, err)
	}
}

// TestArchiveAgentBroadcasts_DrainsStaleKeepsRecent verifies the core BUG-094
// behavior: broadcasts older than the cutoff are archived (drop out of the
// inbox), recent ones stay, and the JSONL is preserved forward-only (the
// original create line is never deleted, an archived=true line is appended).
func TestArchiveAgentBroadcasts_DrainsStaleKeepsRecent(t *testing.T) {
	c := newArchiveTestClient(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sendBackdatedBroadcast(t, c, "11111111-1111-1111-1111-111111111111", "TASK-1 done", now.Add(-40*24*time.Hour))
	sendBackdatedBroadcast(t, c, "22222222-2222-2222-2222-222222222222", "TASK-2 done", now.Add(-35*24*time.Hour))
	sendBackdatedBroadcast(t, c, "33333333-3333-3333-3333-333333333333", "TASK-3 done", now.Add(-2*time.Hour))

	before := now.Add(-30 * 24 * time.Hour)
	n, err := c.ArchiveAgentBroadcasts(ctx, before)
	if err != nil {
		t.Fatalf("ArchiveAgentBroadcasts: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 stale broadcasts archived, got %d", n)
	}

	// Inbox now shows only the recent broadcast (archived ones are filtered out).
	inbox, err := c.GetInbox(ctx, "all", false, "")
	if err != nil {
		t.Fatalf("GetInbox: %v", err)
	}
	if len(inbox) != 1 || inbox[0].ID != "33333333-3333-3333-3333-333333333333" {
		t.Fatalf("expected only the recent broadcast in inbox, got %+v", inbox)
	}

	// Forward-only JSONL guarantee: both create lines AND the appended archived
	// lines are present (nothing deleted), and the folded current state is
	// archived=true for the two stale records.
	all, err := brain.ReadAll(c.dataDir, "messages")
	if err != nil {
		t.Fatalf("brain.ReadAll: %v", err)
	}
	if len(all) != 5 { // 3 creates + 2 appended archived lines
		t.Fatalf("expected 5 raw JSONL lines (3 create + 2 archive), got %d", len(all))
	}
	latest, err := brain.ReadLatest(c.dataDir, "messages")
	if err != nil {
		t.Fatalf("brain.ReadLatest: %v", err)
	}
	archivedFlag := map[string]bool{}
	for _, e := range latest {
		archivedFlag[e.UUID], _ = e.Payload["archived"].(bool)
	}
	if !archivedFlag["11111111-1111-1111-1111-111111111111"] || !archivedFlag["22222222-2222-2222-2222-222222222222"] {
		t.Fatalf("stale records should fold to archived=true, got %+v", archivedFlag)
	}
	if archivedFlag["33333333-3333-3333-3333-333333333333"] {
		t.Fatalf("recent record should not be archived")
	}
}

// TestArchiveAgentBroadcasts_Idempotent verifies a second drain is a no-op:
// already-archived broadcasts are skipped, so n=0 and no extra JSONL lines.
func TestArchiveAgentBroadcasts_Idempotent(t *testing.T) {
	c := newArchiveTestClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	sendBackdatedBroadcast(t, c, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "TASK-1 done", now.Add(-40*24*time.Hour))

	before := now.Add(-30 * 24 * time.Hour)
	if n, err := c.ArchiveAgentBroadcasts(ctx, before); err != nil || n != 1 {
		t.Fatalf("first archive: n=%d err=%v (want 1, nil)", n, err)
	}
	if n, err := c.ArchiveAgentBroadcasts(ctx, before); err != nil || n != 0 {
		t.Fatalf("second archive should be a no-op: n=%d err=%v (want 0, nil)", n, err)
	}
}

// TestArchiveAgentBroadcasts_BatchScalesUnderDeadline is the BUG-094 regression
// guard: archiving 600 stale broadcasts must complete well under the 10s command
// timeout. The old per-message loop (re-fold whole JSONL + whole-file CAS rescan
// + one store round-trip each) was O(N²) and blew the deadline; the batched path
// folds once, appends forward-only, and mirrors in a single SetPayload txn.
func TestArchiveAgentBroadcasts_BatchScalesUnderDeadline(t *testing.T) {
	c := newArchiveTestClient(t)
	now := time.Now().UTC()
	const count = 600
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
		sendBackdatedBroadcast(t, c, id, fmt.Sprintf("TASK-%d done", i), now.Add(-40*24*time.Hour))
	}

	// Bound the call exactly as the command layer does (10s withTimeout).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	before := now.Add(-30 * 24 * time.Hour)

	start := time.Now()
	n, err := c.ArchiveAgentBroadcasts(ctx, before)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ArchiveAgentBroadcasts(%d): %v (elapsed %s)", count, err, elapsed)
	}
	if n != count {
		t.Fatalf("expected %d archived, got %d", count, n)
	}
	if ctx.Err() != nil {
		t.Fatalf("archive exceeded the 10s deadline: %v (elapsed %s)", ctx.Err(), elapsed)
	}
	// Generous guard: the batched path finishes in well under the deadline; the
	// old O(N²) loop would not. Use half the deadline as the regression line.
	if elapsed > 5*time.Second {
		t.Fatalf("archive of %d broadcasts took %s — expected well under the 10s deadline (regression in batching)", count, elapsed)
	}
}
