package store

import (
	"context"
	"testing"

	"github.com/gurkangul/gg-cli/internal/brain"
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

// TestReembedAll_RecoversMissingFromJSONL is the embedded-store successor to the
// old Qdrant-backed TestReconcile_FromJSONL_RecoversMissingFromQdrant (BUG-090).
// The Qdrant backend (and its remote reconcile path) was removed in e16c9ac;
// the durable-memory recovery contract now lives in ReembedAll, which overlays
// the JSONL source of truth (.gg/brain/<kind>.jsonl) onto the embedded SQLite
// vector store (BUG-069).
//
// It proves the contract that a record present ONLY in JSONL — e.g. one that
// never reached the vector index or was dropped by a prior vector-only rebuild —
// is rematerialized into vectorstore.db by a reembed/reconcile run, so semantic
// recall is restored from durable memory rather than silently lost.
func TestReembedAll_RecoversMissingFromJSONL(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir() // serves as both the .gg data dir and the brain/ root
	const projectID = "11111111-1111-1111-1111-111111111111"

	c, err := New(dir, projectID)
	if err != nil {
		t.Fatalf("New store: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.EnsureCollections(ctx, 3); err != nil {
		t.Fatalf("EnsureCollections: %v", err)
	}

	// A "survivor" decision lives in BOTH the vector store and JSONL.
	const survivorID = "22222222-2222-2222-2222-222222222222"
	survivorPay, err := TryValueMap(map[string]any{
		"text":       "use embedded SQLite vector store",
		"reason":     "no Docker dependency",
		"created_at": "2026-06-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("survivor payload: %v", err)
	}
	wait := true
	if err := c.vsUpsert(ctx, &UpsertPoints{
		CollectionName: c.collDecisions(),
		Wait:           &wait,
		Points: []*PointStruct{{
			Id:      NewID(survivorID),
			Vectors: NewVectors(1, 0, 0),
			Payload: survivorPay,
		}},
	}); err != nil {
		t.Fatalf("seed survivor in store: %v", err)
	}
	if err := brain.Append(dir, collSuffixDecisions, survivorID, "agent", map[string]any{
		"text":       "use embedded SQLite vector store",
		"reason":     "no Docker dependency",
		"created_at": "2026-06-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("append survivor to JSONL: %v", err)
	}

	// The "missing" decision exists ONLY in JSONL — it was never upserted into
	// the vector store (or was dropped by a prior rebuild). This is the record
	// the reconcile path must recover.
	const missingID = "33333333-3333-3333-3333-333333333333"
	if err := brain.Append(dir, collSuffixDecisions, missingID, "agent", map[string]any{
		"text":       "record durable evidence with gg record --evidence",
		"reason":     "future agents distinguish checked facts",
		"created_at": "2026-06-02T00:00:00Z",
	}); err != nil {
		t.Fatalf("append missing to JSONL: %v", err)
	}

	// Sanity: before reconcile the vector store has only the survivor.
	before, err := c.scrollAll(ctx, &ScrollPoints{
		CollectionName: c.collDecisions(),
		WithPayload:    NewWithPayloadEnable(true),
	})
	if err != nil {
		t.Fatalf("scroll before reembed: %v", err)
	}
	if len(before) != 1 || before[0].GetId().GetUuid() != survivorID {
		t.Fatalf("pre-reembed store should hold only the survivor, got %d points %v", len(before), idsOf(before))
	}

	// Run the embedded reconcile-from-JSONL path.
	if _, err := c.ReembedAll(ctx, &countingEmbedder{}, 3); err != nil {
		t.Fatalf("ReembedAll: %v", err)
	}

	// After reconcile BOTH records are present in the vector store: the survivor
	// is preserved and the JSONL-only record is recovered.
	after, err := c.scrollAll(ctx, &ScrollPoints{
		CollectionName: c.collDecisions(),
		WithPayload:    NewWithPayloadEnable(true),
	})
	if err != nil {
		t.Fatalf("scroll after reembed: %v", err)
	}
	got := make(map[string]bool, len(after))
	for _, p := range after {
		got[p.GetId().GetUuid()] = true
	}
	if !got[survivorID] {
		t.Errorf("survivor %s lost during reembed; have %v", survivorID, idsOf(after))
	}
	if !got[missingID] {
		t.Errorf("JSONL-only record %s was NOT recovered into the vector store; have %v", missingID, idsOf(after))
	}
	if len(after) != 2 {
		t.Errorf("want 2 points after reembed (survivor + recovered), got %d %v", len(after), idsOf(after))
	}
}

func idsOf(points []*RetrievedPoint) []string {
	out := make([]string, 0, len(points))
	for _, p := range points {
		out = append(out, p.GetId().GetUuid())
	}
	return out
}
