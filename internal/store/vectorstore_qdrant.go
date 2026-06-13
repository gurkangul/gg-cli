package store

import (
	"context"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/qdrant/go-client/qdrant"
)

// qdrantStore is the VectorStore implementation backed by a live Qdrant
// instance. It is a thin pass-through over *qdrant.Client and preserves the
// pre-TASK-493 behavior exactly — it is the parity oracle the SQLite backend is
// validated against, and the default backend.
type qdrantStore struct {
	qc *qdrant.Client
}

// newQdrantStore dials a Qdrant instance using the supplied config. The
// *qdrant.Client it builds is owned by the returned qdrantStore; callers do not
// receive it directly (the previous third return value populated a now-removed
// vestigial Client.qc field).
func newQdrantStore(cfg *config.QdrantConfig) (*qdrantStore, error) {
	qc, err := qdrant.NewClient(&qdrant.Config{
		Host:                   cfg.Host,
		Port:                   cfg.Port,
		SkipCompatibilityCheck: true,
	})
	if err != nil {
		return nil, err
	}
	return &qdrantStore{qc: qc}, nil
}

func (s *qdrantStore) ListCollections(ctx context.Context) ([]string, error) {
	return s.qc.ListCollections(ctx)
}

func (s *qdrantStore) CreateCollection(ctx context.Context, req *qdrant.CreateCollection) error {
	return s.qc.CreateCollection(ctx, req)
}

func (s *qdrantStore) DeleteCollection(ctx context.Context, name string) error {
	return s.qc.DeleteCollection(ctx, name)
}

func (s *qdrantStore) Upsert(ctx context.Context, req *qdrant.UpsertPoints) (*qdrant.UpdateResult, error) {
	return s.qc.Upsert(ctx, req)
}

func (s *qdrantStore) SetPayload(ctx context.Context, req *qdrant.SetPayloadPoints) (*qdrant.UpdateResult, error) {
	return s.qc.SetPayload(ctx, req)
}

func (s *qdrantStore) Delete(ctx context.Context, req *qdrant.DeletePoints) (*qdrant.UpdateResult, error) {
	return s.qc.Delete(ctx, req)
}

func (s *qdrantStore) Query(ctx context.Context, req *qdrant.QueryPoints) ([]*qdrant.ScoredPoint, error) {
	return s.qc.Query(ctx, req)
}

func (s *qdrantStore) Get(ctx context.Context, req *qdrant.GetPoints) ([]*qdrant.RetrievedPoint, error) {
	return s.qc.Get(ctx, req)
}

func (s *qdrantStore) ScrollAndOffset(ctx context.Context, req *qdrant.ScrollPoints) ([]*qdrant.RetrievedPoint, *qdrant.PointId, error) {
	return s.qc.ScrollAndOffset(ctx, req)
}

func (s *qdrantStore) Count(ctx context.Context, req *qdrant.CountPoints) (uint64, error) {
	return s.qc.Count(ctx, req)
}

func (s *qdrantStore) HealthCheck(ctx context.Context) error {
	_, err := s.qc.HealthCheck(ctx)
	return err
}

func (s *qdrantStore) Close() error {
	return s.qc.Close()
}
