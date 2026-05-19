// Package cmd — tests for gg audit inbox-obedience metric computation.
package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/store"
)

// fakeMessageClient satisfies the narrow interface needed by
// computeObedienceRows without requiring a real Qdrant instance.
type fakeMessageClient struct {
	messages []store.Message
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

// computeObedienceRowsFromClient is a test-only variant that accepts a
// fakeMessageClient instead of *store.Client.
func computeObedienceRowsFromClient(ctx context.Context, client *fakeMessageClient, since time.Time, roleFilter string) ([]ObedienceRow, error) {
	msgs, err := client.ListMessagesSince(ctx, since)
	if err != nil {
		return nil, err
	}

	type counts struct{ received, acknowledged int }
	byRole := make(map[string]*counts)

	for _, m := range msgs {
		targets := targetRoles(m)
		for _, role := range targets {
			if roleFilter != "" && !strings.EqualFold(role, roleFilter) {
				continue
			}
			if byRole[role] == nil {
				byRole[role] = &counts{}
			}
			byRole[role].received++
			if m.Read {
				byRole[role].acknowledged++
			}
		}
	}

	rows := make([]ObedienceRow, 0, len(byRole))
	for role, c := range byRole {
		ratio := 0.0
		if c.received > 0 {
			ratio = float64(c.acknowledged) / float64(c.received)
		}
		rows = append(rows, ObedienceRow{
			Role:           role,
			Received:       c.received,
			Acknowledged:   c.acknowledged,
			ObedienceRatio: ratio,
			LowCompliance:  ratio < 0.5 && c.received > 3,
		})
	}
	sortObedienceRows(rows)
	return rows, nil
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
		{FromRole: "orchestrator", ToRole: "all", Content: "TASK-423 picked up", Read: false, CreatedAt: now()},
	}}
	rows, err := computeObedienceRowsFromClient(context.Background(), client,
		time.Now().UTC().Add(-time.Hour), "")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("broadcast-only messages should not create obedience rows, got %+v", rows)
	}
}

func TestObedience_AllBroadcastWithMentionCountsMentionedRole(t *testing.T) {
	client := &fakeMessageClient{messages: []store.Message{
		{FromRole: "orchestrator", ToRole: "all", Content: "@reviewer please verify BUG-059", Read: true, CreatedAt: now()},
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
		{FromRole: "orchestrator", ToRole: "developer", Content: "@developer please ack", Read: true, CreatedAt: now()},
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
	for _, field := range []string{"role", "received", "acknowledged", "obedience_ratio", "low_compliance"} {
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
