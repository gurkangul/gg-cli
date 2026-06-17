package graph

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/gurkangul/gg-cli/internal/sqliteretry"
	_ "modernc.org/sqlite" // registers the CGO-free "sqlite" driver
)

// sqliteStore is the pure-Go, CGO-free GraphStore implementation (TASK-494).
//
// It replaces the Memgraph Docker container with an embedded property graph over
// modernc.org/sqlite (already a dependency). The graph is normalized into two
// tables:
//
//   - nodes(internal_id, label, project_id, props) — one row per node; props is
//     the JSON-encoded property map (mirroring Memgraph's schemaless node).
//   - edges(internal_id, src, dst, type, project_id, props) — one row per
//     relationship; src/dst reference nodes.internal_id.
//
// Indexes on (project_id, label) and (project_id, src/dst, type) make the
// package's single-hop neighbor lookups fast. Every query in this package is
// single-hop (the multi-depth BFS in symbol_flow.go / dependents.go runs in Go
// and only asks for direct neighbors), so an indexed edge lookup is a faithful
// replacement for Memgraph with no graph algorithm lost.
//
// Run dispatches the package's fixed set of Cypher templates to SQL handlers
// (graphstore_sqlite_exec.go). The Cypher string is matched, not interpreted —
// the template set is closed and validated against the neo4j oracle via the
// TASK-494 parity proof.
//
// The database lives at <dataDir>/graph.db.
type sqliteStore struct {
	db *sql.DB
	mu sync.Mutex // serializes writes; modernc sqlite is single-writer
}

const sqliteGraphDBName = "graph.db"

// newSQLiteStore opens (creating if needed) the embedded graph database under
// dataDir and ensures the schema exists.
func newSQLiteStore(dataDir string) (*sqliteStore, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("sqlite graph store requires a data directory")
	}
	dbPath := filepath.Join(dataDir, sqliteGraphDBName)
	// _journal_mode=WAL: concurrent readers + a single writer.
	// _busy_timeout: wait rather than failing immediately on a held lock.
	// _foreign_keys=on: enforce the edges→nodes references.
	dsn := "file:" + dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open graph store: %w", err)
	}
	// A single open connection avoids "database is locked" churn under WAL for
	// the read/write pattern this store uses.
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
CREATE TABLE IF NOT EXISTS nodes (
	internal_id INTEGER PRIMARY KEY AUTOINCREMENT,
	label       TEXT NOT NULL,
	project_id  TEXT NOT NULL,
	props       TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_nodes_pid_label ON nodes(project_id, label);
CREATE INDEX IF NOT EXISTS idx_nodes_pid       ON nodes(project_id);

CREATE TABLE IF NOT EXISTS edges (
	internal_id INTEGER PRIMARY KEY AUTOINCREMENT,
	src         INTEGER NOT NULL REFERENCES nodes(internal_id) ON DELETE CASCADE,
	dst         INTEGER NOT NULL REFERENCES nodes(internal_id) ON DELETE CASCADE,
	type        TEXT NOT NULL,
	project_id  TEXT NOT NULL,
	props       TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_edges_pid_src_type ON edges(project_id, src, type);
CREATE INDEX IF NOT EXISTS idx_edges_pid_dst_type ON edges(project_id, dst, type);
CREATE INDEX IF NOT EXISTS idx_edges_pid          ON edges(project_id);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("init graph store schema: %w", err)
	}
	return nil
}

// RunDDL is a no-op for the embedded backend: the index DDL the package emits
// (CREATE INDEX ON :Symbol(...)) describes Memgraph label indexes; the SQLite
// schema's composite indexes are created up front in initSchema, so there is
// nothing to do at SchemaInit time. Returning nil keeps `gg index`'s SchemaInit
// step working identically.
func (s *sqliteStore) RunDDL(ctx context.Context, cypher string, params map[string]any) error {
	return nil
}

func (s *sqliteStore) HealthCheck(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *sqliteStore) Close(ctx context.Context) error {
	return s.db.Close()
}

// execWrite runs a write statement with SQLITE_BUSY/SQLITE_LOCKED retry+backoff
// (TASK-503). All graph writes are single, idempotent INSERT/UPDATE/DELETE
// statements (the package serializes its own writers with s.mu and never
// interleaves a multi-statement write outside a handler), so re-running on a
// transient lock from a parallel agent's process is safe. busy_timeout already
// waits on a held lock; this rides out the case where a writer is starved past
// that timeout instead of surfacing a hard "database is locked".
func (s *sqliteStore) execWrite(ctx context.Context, query string, args ...any) (sql.Result, error) {
	var res sql.Result
	err := sqliteretry.Do(ctx, func() error {
		r, e := s.db.ExecContext(ctx, query, args...)
		if e != nil {
			return e
		}
		res = r
		return nil
	})
	return res, err
}
