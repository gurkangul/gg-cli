package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite" // registers the CGO-free "sqlite" driver
)

// errCollectionNotFound is the native sentinel raised when a read targets a
// collection that has not been created. The package-wide
// isCollectionNotFoundError check (errors.Is) recognizes it, so a missing
// collection on a fresh install surfaces a NotFound-equivalent that the command
// layer treats as "fall back to the offline JSONL scan".
var errCollectionNotFound = errors.New("collection not found")

// collectionNotFoundErr wraps the sentinel with the collection name for context.
func collectionNotFoundErr(name string) error {
	return fmt.Errorf("collection %s not found: %w", name, errCollectionNotFound)
}

// sqliteStore is the pure-Go, CGO-free VectorStore implementation (TASK-493).
//
// It replaces the the vector store Docker container with an embedded brute-force cosine
// index. Vectors are stored as little-endian float32 BLOBs in modernc.org/sqlite
// (already a dependency); semantic search loads the candidate set for a
// collection, applies the same server-side payload filters the vector store enforced, and
// computes exact cosine similarity in Go. At the project's scale (~3.5k × 768)
// this is single-digit milliseconds and is exact rather than approximate, so it
// matches or strictly improves on the vector store's HNSW recall.
//
// The database lives at <dataDir>/vectorstore.db. Collections are virtual: a
// row in the `collections` table records the configured dimension, mirroring
// the vector store's CreateCollection/ListCollections/DeleteCollection semantics so the
// dimension-mismatch guard (embedding meta) and `gg reembed` keep working.
type sqliteStore struct {
	db *sql.DB
	mu sync.Mutex // serializes writes; modernc sqlite is single-writer
}

const sqliteVecDBName = "vectorstore.db"

// newSQLiteStore opens (creating if needed) the embedded vector database under
// dataDir and ensures the schema exists.
func newSQLiteStore(dataDir string) (*sqliteStore, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("sqlite vector store requires a data directory")
	}
	dbPath := filepath.Join(dataDir, sqliteVecDBName)
	// _journal_mode=WAL: concurrent readers + a single writer.
	// _busy_timeout: wait rather than failing immediately on a held lock.
	dsn := "file:" + dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open vector store: %w", err)
	}
	// A single open connection avoids "database is locked" churn under WAL for
	// the brute-force read/write pattern this store uses.
	db.SetMaxOpenConns(1)
	s := &sqliteStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *sqliteStore) initSchema() error {
	const schema = `
CREATE TABLE IF NOT EXISTS collections (
	name TEXT PRIMARY KEY,
	dim  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS points (
	collection TEXT NOT NULL,
	id         TEXT NOT NULL,
	vector     BLOB NOT NULL,
	payload    BLOB NOT NULL,
	PRIMARY KEY (collection, id)
);
CREATE INDEX IF NOT EXISTS idx_points_collection ON points(collection);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("init vector store schema: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListCollections(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM collections ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s *sqliteStore) CreateCollection(ctx context.Context, req *CreateCollection) error {
	if req == nil || req.CollectionName == "" {
		return fmt.Errorf("create collection: empty collection name")
	}
	dim := collectionDim(req)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO collections(name, dim) VALUES(?, ?)
		 ON CONFLICT(name) DO UPDATE SET dim=excluded.dim`,
		req.CollectionName, dim)
	if err != nil {
		return fmt.Errorf("create collection %s: %w", req.CollectionName, err)
	}
	return nil
}

// collectionDim extracts the configured dense-vector dimension from a
// CreateCollection request, defaulting to VectorSize when unspecified.
func collectionDim(req *CreateCollection) int64 {
	dim := int64(VectorSize)
	if vc := req.GetVectorsConfig(); vc != nil {
		if p := vc.GetParams(); p != nil && p.GetSize() > 0 {
			dim = int64(p.GetSize())
		}
	}
	return dim
}

func (s *sqliteStore) DeleteCollection(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM points WHERE collection=?`, name); err != nil {
		return fmt.Errorf("delete collection %s points: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM collections WHERE name=?`, name); err != nil {
		return fmt.Errorf("delete collection %s: %w", name, err)
	}
	return tx.Commit()
}

// collectionExists reports whether the named collection has been created. Reads
// that target a missing collection must surface a NotFound-equivalent error so
// callers' isCollectionNotFoundError checks keep working on a fresh install.
func (s *sqliteStore) collectionExists(ctx context.Context, name string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM collections WHERE name=?`, name).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *sqliteStore) HealthCheck(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}
