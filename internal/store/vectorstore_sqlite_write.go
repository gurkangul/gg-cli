package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/gurkangul/gg-cli/internal/sqliteretry"
)

// pointIDKey renders a PointId to the stable string key used as the SQLite
// primary key. Every upsert in this package uses NewID(uuid) (a UUID), but a
// numeric id is supported for completeness and round-trips via num: prefix.
func pointIDKey(id *PointId) string {
	if id == nil {
		return ""
	}
	if u := id.GetUuid(); u != "" {
		return u
	}
	return fmt.Sprintf("num:%d", id.GetNum())
}

// keyToPointID reverses pointIDKey for response construction.
func keyToPointID(key string) *PointId {
	var n uint64
	if _, err := fmt.Sscanf(key, "num:%d", &n); err == nil {
		return NewIDNum(n)
	}
	return NewID(key)
}

// denseVector extracts the dense float32 slice a PointStruct carries, tolerating
// both the structured Dense field (set by NewVectors) and the deprecated flat
// Data field.
func denseVector(v *Vectors) []float32 {
	if v == nil {
		return nil
	}
	vec := v.GetVector()
	if vec == nil {
		return nil
	}
	if d := vec.GetDense(); d != nil && len(d.GetData()) > 0 {
		return d.GetData()
	}
	// GetData is the deprecated flat fallback for older vector format versions
	// that predate the Dense oneof. Suppressed for both standalone staticcheck
	// (//lint:ignore) and golangci-lint's embedded staticcheck (//nolint).
	//lint:ignore SA1019 fallback for older vector format versions that predate the Dense oneof
	return vec.GetData() //nolint:staticcheck // GetData: deprecated flat fallback for pre-Dense-oneof the vector store
}

func (s *sqliteStore) Upsert(ctx context.Context, req *UpsertPoints) (*UpdateResult, error) {
	if req == nil {
		return nil, fmt.Errorf("upsert: nil request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Rebuild the whole transaction per attempt so a transient SQLITE_BUSY from a
	// parallel agent's writer retries cleanly with backoff (TASK-503) instead of
	// hard-failing once busy_timeout is exhausted.
	if err := sqliteretry.Do(ctx, func() error { return s.upsertTx(ctx, req) }); err != nil {
		return nil, err
	}
	return &UpdateResult{}, nil
}

func (s *sqliteStore) upsertTx(ctx context.Context, req *UpsertPoints) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO points(collection, id, vector, payload) VALUES(?, ?, ?, ?)
		 ON CONFLICT(collection, id) DO UPDATE SET vector=excluded.vector, payload=excluded.payload`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, p := range req.GetPoints() {
		key := pointIDKey(p.GetId())
		if key == "" {
			return fmt.Errorf("upsert into %s: point has no id", req.CollectionName)
		}
		payloadBlob, err := encodePayload(p.GetPayload())
		if err != nil {
			return fmt.Errorf("upsert into %s: %w", req.CollectionName, err)
		}
		vecBlob := encodeVector(denseVector(p.GetVectors()))
		if _, err := stmt.ExecContext(ctx, req.CollectionName, key, vecBlob, payloadBlob); err != nil {
			return fmt.Errorf("upsert into %s: %w", req.CollectionName, err)
		}
	}
	return tx.Commit()
}

// SetPayload merges new payload keys into the existing payload of the selected
// points, mirroring the vector store's partial SetPayload (NOT a full overwrite). The
// PointsSelector is expected to carry explicit ids (the only shape used here);
// a filter-based selector is also supported.
func (s *sqliteStore) SetPayload(ctx context.Context, req *SetPayloadPoints) (*UpdateResult, error) {
	if req == nil {
		return nil, fmt.Errorf("set payload: nil request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := sqliteretry.Do(ctx, func() error { return s.setPayloadTx(ctx, req) }); err != nil {
		return nil, err
	}
	return &UpdateResult{}, nil
}

func (s *sqliteStore) setPayloadTx(ctx context.Context, req *SetPayloadPoints) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	keys, err := s.selectorKeys(ctx, tx, req.CollectionName, req.GetPointsSelector())
	if err != nil {
		return err
	}
	for _, key := range keys {
		var blob []byte
		row := tx.QueryRowContext(ctx, `SELECT payload FROM points WHERE collection=? AND id=?`, req.CollectionName, key)
		if err := row.Scan(&blob); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return err
		}
		existing, err := decodePayload(blob)
		if err != nil {
			return err
		}
		for k, v := range req.GetPayload() {
			existing[k] = v
		}
		merged, err := encodePayload(existing)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE points SET payload=? WHERE collection=? AND id=?`, merged, req.CollectionName, key); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) Delete(ctx context.Context, req *DeletePoints) (*UpdateResult, error) {
	if req == nil {
		return nil, fmt.Errorf("delete: nil request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := sqliteretry.Do(ctx, func() error { return s.deleteTx(ctx, req) }); err != nil {
		return nil, err
	}
	return &UpdateResult{}, nil
}

func (s *sqliteStore) deleteTx(ctx context.Context, req *DeletePoints) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	keys, err := s.selectorKeys(ctx, tx, req.CollectionName, req.GetPoints())
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, `DELETE FROM points WHERE collection=? AND id=?`, req.CollectionName, key); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// selectorKeys resolves a PointsSelector to the set of SQLite primary keys it
// targets — either explicit ids or every point matching a filter.
func (s *sqliteStore) selectorKeys(ctx context.Context, tx *sql.Tx, coll string, sel *PointsSelector) ([]string, error) {
	if sel == nil {
		return nil, nil
	}
	if pts := sel.GetPoints(); pts != nil {
		out := make([]string, 0, len(pts.GetIds()))
		for _, id := range pts.GetIds() {
			out = append(out, pointIDKey(id))
		}
		return out, nil
	}
	filter := sel.GetFilter()
	rows, err := tx.QueryContext(ctx, `SELECT id, payload FROM points WHERE collection=?`, coll)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		payload, err := decodePayload(blob)
		if err != nil {
			return nil, err
		}
		if matchFilter(payload, filter) {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}
