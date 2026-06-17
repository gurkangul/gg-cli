package store

import (
	"context"
	"testing"
)

type countingEmbedder struct{ calls int }

func (e *countingEmbedder) Generate(context.Context, string) ([]float32, error) {
	e.calls++
	return []float32{1, 2, 3}, nil
}

// TestPayloadExtractors_KeyAlignment guards against the C1 regression:
// reembed extractors must read the SAME payload keys that Add*/Send*/Open*
// store. A mismatch silently produces zero-vector points after re-embedding,
// killing semantic recall on the affected collection.
//
// If you add a new collection or rename a payload field, add it here.
func TestPayloadExtractors_KeyAlignment(t *testing.T) {
	cases := []struct {
		name       string
		coll       string
		payload    map[string]any
		wantSubstr string // text the extractor must surface
	}{
		{
			name:       "decisions reads text+reason",
			coll:       collSuffixDecisions,
			payload:    map[string]any{"text": "JWT for auth", "reason": "stateless"},
			wantSubstr: "JWT for auth stateless",
		},
		{
			name:       "rejections reads approach+reason (NOT text)",
			coll:       collSuffixRejections,
			payload:    map[string]any{"approach": "session-based", "reason": "stateful"},
			wantSubstr: "session-based stateful",
		},
		{
			name:       "tasks reads title+detail",
			coll:       collSuffixTasks,
			payload:    map[string]any{"title": "JWT endpoint", "detail": "login+refresh"},
			wantSubstr: "JWT endpoint login+refresh",
		},
		{
			name:       "bugs reads title+detail",
			coll:       collSuffixBugs,
			payload:    map[string]any{"title": "login 500", "detail": "null email"},
			wantSubstr: "login 500 null email",
		},
		{
			name:       "notes reads text",
			coll:       collSuffixNotes,
			payload:    map[string]any{"text": "tried X, failed"},
			wantSubstr: "tried X, failed",
		},
		{
			name:       "messages reads content (NOT text)",
			coll:       collSuffixMessages,
			payload:    map[string]any{"content": "auth ready"},
			wantSubstr: "auth ready",
		},
		{
			name:       "discussions reads topic+detail (NOT question)",
			coll:       collSuffixDiscussions,
			payload:    map[string]any{"topic": "REST?", "detail": "scope"},
			wantSubstr: "REST? scope",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			extractor, ok := collTextExtractors[tc.coll]
			if !ok {
				t.Fatalf("no extractor for collection suffix %q", tc.coll)
			}
			pay, err := TryValueMap(tc.payload)
			if err != nil {
				t.Fatalf("build payload: %v", err)
			}
			got := extractor(pay)
			if got != tc.wantSubstr {
				t.Errorf("extractor returned %q, want %q (key mismatch?)", got, tc.wantSubstr)
			}
		})
	}
}

// TestPayloadExtractors_CollectionsCoveredAtLeastOnce guards against an
// extractor being added without coverage above.
func TestPayloadExtractors_CollectionsCoveredAtLeastOnce(t *testing.T) {
	for _, suffix := range collectionSuffixes {
		if _, ok := collTextExtractors[suffix]; !ok {
			t.Errorf("collection %q has no payload extractor — gg reembed will silently drop it", suffix)
		}
	}
}

func TestClearDegradedVectorMarkers_RemovesReplayFlags(t *testing.T) {
	text, _ := NewValue("keep me")
	marker, _ := NewValue("reconcile_zero_vector")
	markedAt, _ := NewValue("2026-05-21T00:00:00Z")
	payload := map[string]*Value{
		"text":                  text,
		"gg_vector_degraded":    marker,
		"gg_vector_degraded_at": markedAt,
	}

	cleaned := clearDegradedVectorMarkers(payload)

	if cleaned["text"].GetStringValue() != "keep me" {
		t.Fatalf("text payload not preserved: %#v", cleaned["text"])
	}
	if _, ok := cleaned["gg_vector_degraded"]; ok {
		t.Fatal("gg_vector_degraded marker should be removed after successful reembed")
	}
	if _, ok := cleaned["gg_vector_degraded_at"]; ok {
		t.Fatal("gg_vector_degraded_at marker should be removed after successful reembed")
	}
}

func TestVectorForReembed_MessagesKeepIntentionalZeroVector(t *testing.T) {
	embedder := &countingEmbedder{}
	vec, err := vectorForReembed(context.Background(), collSuffixMessages, "agent handoff", 3, embedder)
	if err != nil {
		t.Fatalf("vectorForReembed: %v", err)
	}
	if embedder.calls != 0 {
		t.Fatalf("message reembed should not call embedder, got %d calls", embedder.calls)
	}
	if len(vec) != 3 {
		t.Fatalf("len(vec) = %d, want 3", len(vec))
	}
	for i, v := range vec {
		if v != 0 {
			t.Fatalf("vec[%d] = %v, want zero vector", i, v)
		}
	}
}

func TestVectorForReembed_EmptySemanticTextSkipped(t *testing.T) {
	embedder := &countingEmbedder{}
	vec, err := vectorForReembed(context.Background(), collSuffixNotes, "", 3, embedder)
	if err != nil {
		t.Fatalf("vectorForReembed: %v", err)
	}
	if vec != nil {
		t.Fatalf("vec = %#v, want nil skip", vec)
	}
	if embedder.calls != 0 {
		t.Fatalf("empty text should not call embedder, got %d calls", embedder.calls)
	}
}
