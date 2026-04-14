package store

import (
	"testing"

	"github.com/qdrant/go-client/qdrant"
)

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
			pay, err := qdrant.TryValueMap(tc.payload)
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
