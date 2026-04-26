package store

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
)

// Outbox kind constants for brain-write replay entries.
const (
	OutboxKindDecision  = "record-replay"
	OutboxKindRejection = "reject-replay"
	OutboxKindTask      = "task-replay"
	OutboxKindBug       = "bug-replay"
)

// OutboxQueued is returned by AddDecision / AddRejection / CreateTask /
// ReportBug when the JSONL write succeeded but the Qdrant upsert failed.
// The caller should enqueue an outbox entry and print a stderr note, then
// return exit 0 — the write is durable in JSONL.
type OutboxQueued struct {
	Kind  string // OutboxKind* constant
	UUID  string // entry UUID for idempotent outbox write
	Cause error  // underlying Qdrant error (for logging)
}

func (e *OutboxQueued) Error() string {
	return fmt.Sprintf("qdrant upsert queued (kind=%s uuid=%s): %v", e.Kind, e.UUID, e.Cause)
}

func (e *OutboxQueued) Unwrap() error { return e.Cause }

// CollectionUUIDs returns the set of all point UUIDs present in the given
// collection suffix (e.g. "decisions"). Used by gg doctor --reconcile to find
// entries that are in JSONL but missing from Qdrant after a SIGKILL.
func (c *Client) CollectionUUIDs(ctx context.Context, collSuffix string) (map[string]struct{}, error) {
	collName := c.projectID + "-" + collSuffix
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: collName,
		Limit:          qdrant.PtrOf(uint32(1000)),
		WithPayload:    qdrant.NewWithPayloadInclude(), // no payload — IDs only
	})
	if err != nil {
		return nil, fmt.Errorf("scroll %s: %w", collSuffix, err)
	}
	out := make(map[string]struct{}, len(points))
	for _, p := range points {
		if uid := p.GetId().GetUuid(); uid != "" {
			out[uid] = struct{}{}
		}
	}
	return out, nil
}

// ReplayBrainEntry upserts a brain JSONL payload into a Qdrant collection
// without a vector.  The collection name is derived from the collSuffix (e.g.
// "decisions") combined with the client's projectID prefix.
// Semantic search quality is degraded until a reindex supplies vectors; the
// replay restores the payload so the entry is not permanently lost.
func (c *Client) ReplayBrainEntry(ctx context.Context, collSuffix, uuid string, payload map[string]any) error {
	collName := c.projectID + "-" + collSuffix
	qdrantPayload, err := qdrant.TryValueMap(payload)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	// Zero vector — length must match the collection dimension.
	// We use VectorSize as the default; mismatched dims will be rejected by
	// Qdrant, which is the correct outcome (signals a reindex is needed).
	zeroVec := make([]float32, VectorSize)
	wait := true
	return c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collName,
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(uuid),
				Vectors: qdrant.NewVectors(zeroVec...),
				Payload: qdrantPayload,
			},
		},
	})
}
