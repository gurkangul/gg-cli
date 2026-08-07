package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
)

// TASK-543: a packet that found nothing and a packet that could not look must
// never share a bucket — only the second indicts the backend, and merging them
// sends the owner of a young project to debug a system that is working.
func TestContextPacketOutcome(t *testing.T) {
	tests := []struct {
		name string
		p    ContextPacket
		want string
	}{
		{"records delivered", ContextPacket{Items: 3}, OutcomeDelivered},
		{"lookup worked, brain empty", ContextPacket{Items: 0}, OutcomeEmpty},
		{"lookup failed with nothing", ContextPacket{Items: 0, Degraded: true}, OutcomeFailed},
		// Precedence matters: a partial result lost one of its three searches, so
		// it is a failure to report even though it delivered something.
		{"partial result is a failure, not a delivery", ContextPacket{Items: 3, Degraded: true}, OutcomeFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.outcome(); got != tc.want {
				t.Errorf("outcome() = %q, want %q", got, tc.want)
			}
		})
	}
}

// An entry written before ContextOutcome existed carries no verdict. It must
// stay counted in the packet total but land in none of the three buckets —
// guessing it into one backdates a verdict onto data that never carried it,
// which is the failure mode that killed the two-field design.
func TestRecordContextPacket_PreFieldEntryLandsInNoBucket(t *testing.T) {
	preField := Entry{
		Verb:              VerbTaskStartContext,
		WithContext:       true,
		ContextBlockBytes: 90,
		// no ContextOutcome — this is what 799d612 wrote
	}
	var s WeeklySummary
	s.recordContextPacket(preField)

	if s.TaskStartContextCalls != 1 {
		t.Errorf("pre-field entry should still count as a packet: got %d, want 1", s.TaskStartContextCalls)
	}
	if bucketed := s.TaskStartContextDelivered + s.TaskStartContextEmpty + s.TaskStartContextFailed; bucketed != 0 {
		t.Errorf("pre-field entry must land in no bucket, got %d bucketed", bucketed)
	}
}

func TestRecordContextPacket_BucketsByOutcome(t *testing.T) {
	tests := []struct {
		outcome                  string
		delivered, empty, failed int
	}{
		{OutcomeDelivered, 1, 0, 0},
		{OutcomeEmpty, 0, 1, 0},
		{OutcomeFailed, 0, 0, 1},
	}
	for _, tc := range tests {
		t.Run(tc.outcome, func(t *testing.T) {
			var s WeeklySummary
			s.recordContextPacket(Entry{
				Verb:           VerbTaskStartContext,
				WithContext:    true,
				ContextOutcome: tc.outcome,
			})
			if s.TaskStartContextDelivered != tc.delivered ||
				s.TaskStartContextEmpty != tc.empty ||
				s.TaskStartContextFailed != tc.failed {
				t.Errorf("outcome %q bucketed as (d=%d e=%d f=%d), want (d=%d e=%d f=%d)",
					tc.outcome,
					s.TaskStartContextDelivered, s.TaskStartContextEmpty, s.TaskStartContextFailed,
					tc.delivered, tc.empty, tc.failed)
			}
		})
	}
}

// The pull path keeps its own counters and must never leak into the push-path
// split — the two measure what agents chose to pull versus what gg pushed.
func TestRecordContextPacket_PullPathStaysSeparate(t *testing.T) {
	var s WeeklySummary
	s.recordContextPacket(Entry{
		Verb:              "task",
		WithContext:       true,
		ContextBlockBytes: 1020,
		ContextOutcome:    OutcomeDelivered,
	})
	if s.WithContextCalls != 1 || s.WithContextBytesTotal != 1020 {
		t.Errorf("pull packet not counted: calls=%d bytes=%d", s.WithContextCalls, s.WithContextBytesTotal)
	}
	if s.TaskStartContextCalls != 0 || s.TaskStartContextDelivered != 0 {
		t.Errorf("pull packet leaked into the push split: calls=%d delivered=%d",
			s.TaskStartContextCalls, s.TaskStartContextDelivered)
	}
}

// An empty packet must write "context_items":0 rather than omit the key, or the
// raw log cannot tell "delivered nothing" from "written before the field
// existed" — the ambiguity that sank the rejected two-field verdict design.
func TestEntryJSON_EmptyPacketWritesExplicitZero(t *testing.T) {
	zero := 0
	blob, err := json.Marshal(Entry{
		Verb:           VerbTaskStartContext,
		WithContext:    true,
		ContextItems:   &zero,
		ContextOutcome: OutcomeEmpty,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(blob), `"context_items":0`) {
		t.Errorf("empty packet must record an explicit zero item count, got: %s", blob)
	}

	// A non-context entry must not carry the key at all.
	blob, err = json.Marshal(Entry{Verb: "status"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), "context_items") {
		t.Errorf("non-context entry must omit the item count, got: %s", blob)
	}
}
