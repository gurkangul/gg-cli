package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/gurkangul/gg-cli/internal/outbox"
	"github.com/gurkangul/gg-cli/internal/store"
)

// queueBrainOutbox writes an outbox entry for a failed Qdrant upsert so that
// `gg doctor --reconcile` can replay it when Qdrant recovers.
// Errors are non-fatal — we already wrote to JSONL; losing the outbox entry
// just means the user must manually trigger replay.
func queueBrainOutbox(oq *store.OutboxQueued, ggDir string) {
	if ggDir == "" {
		return
	}
	payload := map[string]any{"uuid": oq.UUID, "kind": oq.Kind}
	if _, err := outbox.Write(ggDir, oq.Kind, payload); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ outbox write failed: %v (JSONL is intact)\n", err)
	}
}

func warnBrainOutboxQueued(w io.Writer, cause error) {
	if errors.Is(cause, store.ErrSemanticVectorUnavailable) {
		fmt.Fprintln(w, "⚠ saved to JSONL; semantic indexing queued (embedding unavailable or Qdrant degraded). Run `gg doctor --reconcile`; `gg reembed` restores vectors.")
		return
	}
	fmt.Fprintln(w, "⚠ saved to JSONL; semantic indexing queued (Qdrant unreachable). Run `gg doctor --reconcile` after recovery.")
}

// brainOutboxPayload is the shape stored in the outbox for brain-write replay.
type brainOutboxPayload struct {
	UUID string `json:"uuid"`
	Kind string `json:"kind"`
}

// parseBrainOutboxPayload extracts a brainOutboxPayload from a raw JSON message.
func parseBrainOutboxPayload(raw json.RawMessage) (brainOutboxPayload, bool) {
	var p brainOutboxPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, false
	}
	return p, p.UUID != "" && p.Kind != ""
}
