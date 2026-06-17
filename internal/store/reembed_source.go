package store

import (
	"github.com/gurkangul/gg-cli/internal/brain"
)

// reembedPoint is one record to be re-embedded: its point id, payload, and the
// text extracted for embedding.
type reembedPoint struct {
	id      string
	payload map[string]*Value
	text    string
}

// payloadRecency returns the sortable timestamp of a record (updated_at, falling
// back to created_at) for picking the newer of two stores. Empty when neither
// field is present.
func payloadRecency(pay map[string]*Value) string {
	if u := pay["updated_at"].GetStringValue(); u != "" {
		return u
	}
	return pay["created_at"].GetStringValue()
}

// mergeJSONLSource overlays the JSONL source of truth onto the the vector store-sourced
// points for a collection (BUG-069). JSONL is authoritative for mutated records:
//
//   - a uuid present only in JSONL (never reached the vector store, or dropped by a prior
//     the vector store-only rebuild) is added, so reembed no longer silently drops it;
//   - a uuid present in both keeps whichever copy is newer by updated_at, so a
//     JSONL-first decision/bug mutation wins while a the vector store-authoritative record
//     (e.g. task claim state) is preserved.
//
// fromStore is the points already read from the vector store; suffix is both the
// collection suffix and the JSONL kind name. Returns the merged, deterministic
// (input-order-stable) point list.
func (c *Client) mergeJSONLSource(suffix string, extractor func(map[string]*Value) string, fromStore []reembedPoint) ([]reembedPoint, error) {
	entries, err := brain.ReadLatest(c.dataDir, suffix)
	if err != nil {
		// JSONL unreadable — fall back to the the vector store-only view rather than fail
		// the whole migration.
		return fromStore, nil //nolint:nilerr // degrade, do not abort reembed
	}
	if len(entries) == 0 {
		return fromStore, nil
	}

	index := make(map[string]int, len(fromStore))
	merged := make([]reembedPoint, len(fromStore))
	copy(merged, fromStore)
	for i, p := range merged {
		index[p.id] = i
	}

	for _, e := range entries {
		pay, vErr := TryValueMap(coerceBrainPayloadForStore(e.Payload))
		if vErr != nil {
			continue // skip a single malformed record, keep migrating
		}
		jp := reembedPoint{id: e.UUID, payload: pay, text: extractor(pay)}
		if i, ok := index[e.UUID]; ok {
			// Present in both — keep the newer copy by updated_at/created_at.
			if payloadRecency(pay) >= payloadRecency(merged[i].payload) {
				merged[i] = jp
			}
			continue
		}
		index[e.UUID] = len(merged)
		merged = append(merged, jp)
	}
	return merged, nil
}
