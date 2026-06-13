package store

import (
	"context"

	"github.com/qdrant/go-client/qdrant"
)

// VectorStore is the backend-agnostic vector index abstraction used by every
// store operation in this package. It is deliberately shaped to the existing
// Qdrant call-sites — the request and response types are the qdrant protobuf
// types so the two implementations are interchangeable without rewriting the
// ~23 files that build these requests.
//
// Two implementations exist:
//   - qdrantStore  — a thin pass-through over *qdrant.Client (parity oracle).
//   - sqlitevec    — a pure-Go, CGO-free brute-force cosine store (TASK-493).
//
// Only the methods actually invoked on the Qdrant client across this package
// are included; the interface is intentionally minimal to keep churn low.
type VectorStore interface {
	// --- collection lifecycle ---
	ListCollections(ctx context.Context) ([]string, error)
	CreateCollection(ctx context.Context, req *qdrant.CreateCollection) error
	DeleteCollection(ctx context.Context, name string) error

	// --- writes ---
	Upsert(ctx context.Context, req *qdrant.UpsertPoints) (*qdrant.UpdateResult, error)
	SetPayload(ctx context.Context, req *qdrant.SetPayloadPoints) (*qdrant.UpdateResult, error)
	Delete(ctx context.Context, req *qdrant.DeletePoints) (*qdrant.UpdateResult, error)

	// --- reads ---
	Query(ctx context.Context, req *qdrant.QueryPoints) ([]*qdrant.ScoredPoint, error)
	Get(ctx context.Context, req *qdrant.GetPoints) ([]*qdrant.RetrievedPoint, error)
	ScrollAndOffset(ctx context.Context, req *qdrant.ScrollPoints) ([]*qdrant.RetrievedPoint, *qdrant.PointId, error)
	Count(ctx context.Context, req *qdrant.CountPoints) (uint64, error)

	// --- health / lifecycle ---
	HealthCheck(ctx context.Context) error
	Close() error
}
