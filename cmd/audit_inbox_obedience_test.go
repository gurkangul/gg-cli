// Package cmd — tests for gg audit inbox-obedience handoff acknowledgement metric computation.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/store"
)

// fakeMessageClient satisfies the narrow interface needed by
// computeObedienceRows without requiring a real Qdrant instance.
type fakeMessageClient struct {
	messages []store.Message
	tasks    map[string]*store.Task
}

func (f *fakeMessageClient) ListMessagesSince(_ context.Context, since time.Time) ([]store.Message, error) {
	var out []store.Message
	for _, m := range f.messages {
		t, err := time.Parse(time.RFC3339, m.CreatedAt)
		if err != nil || !t.Before(since) {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeMessageClient) GetTask(_ context.Context, taskID string) (*store.Task, error) {
	if f.tasks == nil {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	t, ok := f.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	return t, nil
}

// computeObedienceRowsFromClient is a test-only variant that accepts a
// fakeMessageClient instead of *store.Client.
func computeObedienceRowsFromClient(ctx context.Context, client *fakeMessageClient, since time.Time, roleFilter string) ([]ObedienceRow, error) {
	return computeObedienceRows(ctx, client, since, roleFilter)
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func TestObedience_RatioComputed(t *testing.T) {
	msgs := make([]store.Message, 10)
	for i := range msgs {
		msgs[i] = store.Message{
			FromRole:  "claude-code",
			ToRole:    "gsd",
			Content:   "please handle TASK-0" + string(rune('0'+i)),
			Read:      i < 4, // 4 acknowledged out of 10
			CreatedAt: now(),
		}
	}

	client := &fakeMessageClient{messages: msgs}
	rows, err := computeObedienceRowsFromClient(context.Background(), client,
		time.Now().UTC().Add(-time.Hour), "gsd")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("want 1 row for gsd, got %d", len(rows))
	}
	r := rows[0]
	if r.Received != 10 {
		t.Errorf("Received want 10, got %d", r.Received)
	}
	if r.Acknowledged != 4 {
		t.Errorf("Acknowledged want 4, got %d", r.Acknowledged)
	}
	if r.Actionable != 10 || r.StaleFiltered != 0 {
		t.Errorf("expected no stale filtering, got actionable=%d stale=%d", r.Actionable, r.StaleFiltered)
	}
	want := 0.4
	if r.ObedienceRatio < want-0.01 || r.ObedienceRatio > want+0.01 {
		t.Errorf("ObedienceRatio want ~0.4, got %f", r.ObedienceRatio)
	}
	if !r.LowCompliance {
		t.Errorf("LowCompliance should be true (ratio 0.4 < 0.5, received 10 > 3)")
	}
}

func TestObedience_EmptyWindow_NoError(t *testing.T) {
	client := &fakeMessageClient{messages: nil}
	rows, err := computeObedienceRowsFromClient(context.Background(), client,
		time.Now().UTC().Add(-time.Hour), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("want 0 rows for empty window, got %d", len(rows))
	}
}

func TestObedience_AllBroadcastsAreNotRoleAcknowledgement(t *testing.T) {
	client := &fakeMessageClient{messages: []store.Message{
		{FromRole: "coordinator", ToRole: "all", Content: "TASK-423 picked up", Read: false, CreatedAt: now()},
	}}
	rows, err := computeObedienceRowsFromClient(context.Background(), client,
		time.Now().UTC().Add(-time.Hour), "")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("broadcast-only messages should not create role-targeted handoff rows, got %+v", rows)
	}
}

func TestObedience_AllBroadcastWithMentionCountsMentionedRole(t *testing.T) {
	client := &fakeMessageClient{messages: []store.Message{
		{FromRole: "coordinator", ToRole: "all", Content: "@reviewer please verify BUG-059", Read: true, CreatedAt: now()},
	}}
	rows, err := computeObedienceRowsFromClient(context.Background(), client,
		time.Now().UTC().Add(-time.Hour), "")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(rows) != 1 || rows[0].Role != "reviewer" || rows[0].Received != 1 || rows[0].Acknowledged != 1 {
		t.Fatalf("mention should count as reviewer acknowledgement row, got %+v", rows)
	}
}

func TestObedience_DuplicateMentionDoesNotDoubleCount(t *testing.T) {
	client := &fakeMessageClient{messages: []store.Message{
		{FromRole: "coordinator", ToRole: "developer", Content: "@developer please ack", Read: true, CreatedAt: now()},
	}}
	rows, err := computeObedienceRowsFromClient(context.Background(), client,
		time.Now().UTC().Add(-time.Hour), "")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(rows) != 1 || rows[0].Role != "developer" || rows[0].Received != 1 {
		t.Fatalf("duplicate direct+mention target should count once, got %+v", rows)
	}
}

func TestObedience_JSONSchema(t *testing.T) {
	msgs := []store.Message{
		{FromRole: "a", ToRole: "gsd", Content: "x", Read: true, CreatedAt: now()},
		{FromRole: "a", ToRole: "gsd", Content: "y", Read: false, CreatedAt: now()},
	}
	client := &fakeMessageClient{messages: msgs}
	rows, err := computeObedienceRowsFromClient(context.Background(), client,
		time.Now().UTC().Add(-time.Hour), "gsd")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}

	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Verify stable schema fields.
	var parsed []map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed) == 0 {
		t.Fatal("expected non-empty JSON array")
	}
	for _, field := range []string{"role", "received", "acknowledged", "obedience_ratio", "low_compliance", "stale_filtered", "actionable"} {
		if _, ok := parsed[0][field]; !ok {
			t.Errorf("JSON schema missing field %q; got: %v", field, parsed[0])
		}
	}
}

func TestObedience_AllAcknowledged_NotLowCompliance(t *testing.T) {
	msgs := make([]store.Message, 5)
	for i := range msgs {
		msgs[i] = store.Message{
			FromRole: "a", ToRole: "gsd", Content: "x", Read: true, CreatedAt: now(),
		}
	}
	client := &fakeMessageClient{messages: msgs}
	rows, err := computeObedienceRowsFromClient(context.Background(), client,
		time.Now().UTC().Add(-time.Hour), "gsd")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].LowCompliance {
		t.Errorf("100%% acknowledged should not be LowCompliance")
	}
}

// TestObedience_ReadByCountsAsAcknowledged is the BUG-091 facet-C regression
// guard: a real (non-peek) role-targeted read records the recipient in read_by
// (BUG-082 per-recipient model), NOT the legacy global read flag. The audit must
// count read_by membership as acknowledgement so a diligent reviewer no longer
// shows a false 0% ("reviewer 0/15"). Mixed with one stale (read=false, no
// read_by) message to confirm the ratio reflects reality.
func TestObedience_ReadByCountsAsAcknowledged(t *testing.T) {
	client := &fakeMessageClient{messages: []store.Message{
		{FromRole: "developer", ToRole: "reviewer", Content: "TASK-1 ready", Read: false, ReadBy: []string{"codex-1"}, CreatedAt: now()},
		{FromRole: "developer", ToRole: "reviewer", Content: "TASK-2 ready", Read: false, ReadBy: []string{"claude-code-abc12345"}, CreatedAt: now()},
		{FromRole: "developer", ToRole: "reviewer", Content: "TASK-3 ready", Read: false, CreatedAt: now()},
	}}
	rows, err := computeObedienceRowsFromClient(context.Background(), client,
		time.Now().UTC().Add(-time.Hour), "reviewer")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row for reviewer, got %d", len(rows))
	}
	r := rows[0]
	if r.Received != 3 || r.Acknowledged != 2 {
		t.Fatalf("read_by reads must count as acknowledged: received=%d acknowledged=%d (want 3, 2)", r.Received, r.Acknowledged)
	}
	if r.ObedienceRatio < 0.66 || r.ObedienceRatio > 0.67 {
		t.Fatalf("ratio should be 2/3 ~0.667, got %f", r.ObedienceRatio)
	}
}

func TestObedience_FiltersResolvedTaskLinkedMessages(t *testing.T) {
	client := &fakeMessageClient{
		messages: []store.Message{
			{FromRole: "a", ToRole: "gsd", Content: "done task", TaskID: "TASK-1", Read: true, CreatedAt: now()},
			{FromRole: "a", ToRole: "gsd", Content: "open task", TaskID: "TASK-2", Read: false, CreatedAt: now()},
			{FromRole: "a", ToRole: "gsd", Content: "general", Read: true, CreatedAt: now()},
		},
		tasks: map[string]*store.Task{
			"TASK-1": {ID: "TASK-1", Status: "done"},
			"TASK-2": {ID: "TASK-2", Status: "in_progress"},
		},
	}

	rows, err := computeObedienceRowsFromClient(context.Background(), client,
		time.Now().UTC().Add(-time.Hour), "gsd")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Received != 3 || r.StaleFiltered != 1 || r.Actionable != 2 {
		t.Fatalf("unexpected counts: %+v", r)
	}
	if r.Acknowledged != 1 {
		t.Fatalf("acknowledged should exclude resolved messages, got %d", r.Acknowledged)
	}
	if r.ObedienceRatio != 0.5 {
		t.Fatalf("ratio should be 1/2 after stale filtering, got %f", r.ObedienceRatio)
	}
}
