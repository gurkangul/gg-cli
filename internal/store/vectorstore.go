package store

import (
	"context"
)

// VectorStore is the backend-agnostic vector index abstraction used by every
// store operation in this package. It is deliberately shaped to the existing
// the vector store call-sites — the request and response types are the the vector store protobuf
// types so the two implementations are interchangeable without rewriting the
// ~23 files that build these requests.
//
// Two implementations exist:
//   - the embedded store  — a thin pass-through over *Client (parity oracle).
//   - sqlitevec    — a pure-Go, CGO-free brute-force cosine store (TASK-493).
//
// Only the methods actually invoked on the the vector store client across this package
// are included; the interface is intentionally minimal to keep churn low.
type VectorStore interface {
	// --- collection lifecycle ---
	ListCollections(ctx context.Context) ([]string, error)
	CreateCollection(ctx context.Context, req *CreateCollection) error
	DeleteCollection(ctx context.Context, name string) error

	// --- writes ---
	Upsert(ctx context.Context, req *UpsertPoints) (*UpdateResult, error)
	SetPayload(ctx context.Context, req *SetPayloadPoints) (*UpdateResult, error)
	Delete(ctx context.Context, req *DeletePoints) (*UpdateResult, error)

	// --- reads ---
	Query(ctx context.Context, req *QueryPoints) ([]*ScoredPoint, error)
	Get(ctx context.Context, req *GetPoints) ([]*RetrievedPoint, error)
	ScrollAndOffset(ctx context.Context, req *ScrollPoints) ([]*RetrievedPoint, *PointId, error)
	Count(ctx context.Context, req *CountPoints) (uint64, error)

	// --- health / lifecycle ---
	HealthCheck(ctx context.Context) error
	Close() error
}
