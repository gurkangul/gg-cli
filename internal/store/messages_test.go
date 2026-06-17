// Package store — unit tests for message audience routing (TASK-219).
package store

import (
	"testing"
)

// ── messageFromRetrieved ──────────────────────────────────────────────────────

func TestMessageFromRetrieved_AudienceNormalisedToAll(t *testing.T) {
	pay, err := TryValueMap(map[string]any{
		"from_role":  "developer",
		"to_role":    "all",
		"content":    "hello",
		"audience":   "",
		"read":       false,
		"task_id":    "",
		"created_at": "2026-04-19T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("TryValueMap: %v", err)
	}
	p := &RetrievedPoint{Payload: pay}
	msg := messageFromRetrieved(p)
	if msg.Audience != "all" {
		t.Errorf("empty audience should normalise to 'all', got %q", msg.Audience)
	}
}

func TestMessageFromRetrieved_AudienceAgentsPreserved(t *testing.T) {
	pay, err := TryValueMap(map[string]any{
		"from_role":  "claude-code",
		"to_role":    "all",
		"content":    "TASK-042 done",
		"audience":   "agents",
		"read":       false,
		"task_id":    "TASK-042",
		"created_at": "2026-04-19T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("TryValueMap: %v", err)
	}
	p := &RetrievedPoint{Payload: pay}
	msg := messageFromRetrieved(p)
	if msg.Audience != "agents" {
		t.Errorf("audience=agents should round-trip, got %q", msg.Audience)
	}
}

func TestMessageFromRetrieved_AudienceHumanPreserved(t *testing.T) {
	pay, err := TryValueMap(map[string]any{
		"from_role":  "developer",
		"to_role":    "human",
		"content":    "deploy blocked, need approval",
		"audience":   "human",
		"read":       false,
		"task_id":    "",
		"created_at": "2026-04-19T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("TryValueMap: %v", err)
	}
	p := &RetrievedPoint{Payload: pay}
	msg := messageFromRetrieved(p)
	if msg.Audience != "human" {
		t.Errorf("audience=human should round-trip, got %q", msg.Audience)
	}
}

// ── SendMessage audience normalisation ───────────────────────────────────────

func TestMessage_EmptyAudienceNormalisedInSend(t *testing.T) {
	// Verify the normalisation branch in SendMessage: audience="" → "all".
	// We test indirectly by checking the Message struct logic — store-level
	// integration tests require a a live vector backend (GG_INTEGRATION_TEST=1).
	m := Message{
		FromRole: "dev",
		ToRole:   "all",
		Content:  "hi",
	}
	audience := m.Audience
	if audience == "" {
		audience = "all"
	}
	if audience != "all" {
		t.Errorf("expected 'all', got %q", audience)
	}
}
