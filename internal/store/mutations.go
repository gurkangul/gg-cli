package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/qdrant/go-client/qdrant"
)

// ErrMutationConflict wraps brain.ErrVersionConflict at the store boundary so
// command-layer callers can detect a concurrent-write loss (BUG-063). A mutation
// that returns this error wrote nothing — the record changed under it.
var ErrMutationConflict = errors.New("mutation conflict: record changed concurrently")

// ErrRecordNotFound is returned by applyBrainMutation when neither JSONL nor
// Qdrant has the record. Callers translate it into a typed "X not found".
var ErrRecordNotFound = errors.New("record not found")

// cloneAnyMap returns a shallow copy so mutations don't alias the folded entry.
func cloneAnyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// anyStringList coerces a payload field (string list from JSONL []any or Qdrant
// list, or a native []string) into []string.
func anyStringList(v any) []string {
	switch xs := v.(type) {
	case []string:
		return append([]string(nil), xs...)
	case []any:
		out := make([]string, 0, len(xs))
		for _, item := range xs {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// anyInt coerces a numeric payload field to int (handles float64 from JSONL and
// int64 from Qdrant).
func anyInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// coerceBrainPayloadForQdrant returns a copy with the integer "version" field
// normalised to int64 so future NewMatchInt CAS filters match (JSONL decodes
// numbers to float64, which would otherwise upsert as a Qdrant DoubleValue).
func coerceBrainPayloadForQdrant(raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		out[k] = v
	}
	if _, ok := out["version"]; ok {
		out["version"] = brain.PayloadVersion(raw)
	}
	return out
}

// currentBrainPayload returns the current folded payload for pointUUID from the
// JSONL source of truth, falling back to the live Qdrant point for legacy
// records that predate JSONL mutation history. version is the JSONL/payload
// version (0 when legacy/unversioned).
func (c *Client) currentBrainPayload(ctx context.Context, kind, collection, pointUUID string) (payload map[string]any, version int64, found bool, err error) {
	entries, readErr := brain.ReadLatest(c.dataDir, kind)
	if readErr == nil {
		for _, e := range entries {
			if e.UUID == pointUUID {
				p := cloneAnyMap(e.Payload)
				return p, brain.PayloadVersion(p), true, nil
			}
		}
	}
	// Legacy fallback: record exists only in Qdrant (created before JSONL).
	pts, getErr := c.vs.Get(ctx, &qdrant.GetPoints{
		CollectionName: collection,
		Ids:            []*qdrant.PointId{qdrant.NewID(pointUUID)},
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if getErr != nil {
		return nil, 0, false, getErr
	}
	if len(pts) == 0 {
		return nil, 0, false, nil
	}
	p := extractPayload(pts[0].GetPayload())
	return p, brain.PayloadVersion(p), true, nil
}

// applyBrainMutation performs a JSONL-first, compare-and-swap mutation (the fix
// for BUG-062 + BUG-063):
//
//  1. Load the current full record (JSONL source of truth, Qdrant fallback).
//  2. Run apply() to mutate the in-memory payload; apply may return a sentinel
//     error to abort before any write (illegal transition, already-in-state).
//  3. brain.AppendCAS atomically appends the full updated payload under flock,
//     enforcing the version guard — this is the durable commit and the CAS gate.
//  4. Mirror the result to Qdrant best-effort (payload-only SetPayload preserves
//     the vector); a Qdrant failure yields *OutboxQueued, not data loss.
//
// Because the full payload is appended every time, fold/reembed/reconcile
// reconstruct complete current state instead of silently reverting to
// create-time values on a rebuild.
func (c *Client) applyBrainMutation(
	ctx context.Context,
	kind, collection, outboxKind, pointUUID, author string,
	apply func(raw map[string]any) error,
) error {
	raw, version, found, err := c.currentBrainPayload(ctx, kind, collection, pointUUID)
	if err != nil {
		return err
	}
	if !found {
		return ErrRecordNotFound
	}
	if applyErr := apply(raw); applyErr != nil {
		return applyErr
	}
	raw["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	if author == "" {
		if a, ok := raw["author"].(string); ok {
			author = a
		} else if a, ok := raw["by"].(string); ok {
			author = a
		}
	}
	if _, appendErr := brain.AppendCAS(c.dataDir, kind, pointUUID, author, version, raw); appendErr != nil {
		if errors.Is(appendErr, brain.ErrVersionConflict) {
			return fmt.Errorf("%w: %s", ErrMutationConflict, pointUUID)
		}
		return appendErr
	}
	return c.mirrorMutationToQdrant(ctx, collection, outboxKind, pointUUID, raw)
}

// SyncBrainPayload pushes a folded JSONL payload onto an existing Qdrant point
// (payload-only SetPayload, preserving the vector). Used by `gg doctor
// --reconcile` to repair a Qdrant index that is stale relative to the JSONL
// source of truth after a JSONL-first mutation (BUG-062).
func (c *Client) SyncBrainPayload(ctx context.Context, collSuffix, pointUUID string, payload map[string]any) error {
	qp, err := qdrant.TryValueMap(coerceBrainPayloadForQdrant(payload))
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	wait := true
	_, err = c.vs.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: c.projectID + "-" + collSuffix,
		Wait:           &wait,
		Payload:        qp,
		PointsSelector: qdrant.NewPointsSelector(qdrant.NewID(pointUUID)),
	})
	return err
}

// mirrorMutationToQdrant SetPayloads the full updated record to its Qdrant point,
// preserving the existing vector. Returns *OutboxQueued when Qdrant is
// unreachable so the command layer can surface "queued — durable in JSONL".
// A missing Qdrant point is recovered later by `gg doctor --reconcile`.
func (c *Client) mirrorMutationToQdrant(ctx context.Context, collection, outboxKind, pointUUID string, raw map[string]any) error {
	qp, err := qdrant.TryValueMap(coerceBrainPayloadForQdrant(raw))
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	wait := true
	if _, setErr := c.vs.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: collection,
		Wait:           &wait,
		Payload:        qp,
		PointsSelector: qdrant.NewPointsSelector(qdrant.NewID(pointUUID)),
	}); setErr != nil {
		return &OutboxQueued{Kind: outboxKind, UUID: pointUUID, Cause: setErr}
	}
	return nil
}
