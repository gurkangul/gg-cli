package graph

import (
	"context"
	"fmt"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// neo4jStore is the GraphStore implementation backed by a live Memgraph instance
// reached over Bolt with the neo4j-go-driver. It runs the package's Cypher
// verbatim and is the default backend and the parity oracle for the embedded
// SQLite store (TASK-494).
//
// Thread safety: DriverWithContext is safe for concurrent use; a fresh session
// is opened per query and closed before Run returns (results are materialized
// eagerly into the backend-neutral GraphResult).
type neo4jStore struct {
	driver neo4j.DriverWithContext
}

// newNeo4jStore dials Memgraph using the supplied config. It preserves the
// pre-TASK-494 connection semantics exactly (NoAuth when credentials are empty).
func newNeo4jStore(cfg *config.MemgraphConfig) (*neo4jStore, error) {
	if cfg.URI == "" {
		return nil, fmt.Errorf("memgraph.uri is required")
	}
	var auth neo4j.AuthToken
	if cfg.Username == "" && cfg.Password == "" {
		auth = neo4j.NoAuth()
	} else {
		auth = neo4j.BasicAuth(cfg.Username, cfg.Password, "")
	}
	driver, err := neo4j.NewDriverWithContext(cfg.URI, auth)
	if err != nil {
		return nil, fmt.Errorf("memgraph driver init: %w", err)
	}
	return &neo4jStore{driver: driver}, nil
}

// session returns a new auto-commit write session (read-only queries run fine on
// a write session). The caller must close it.
func (s *neo4jStore) session(ctx context.Context) neo4j.SessionWithContext {
	return s.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
}

// Run executes a data-plane Cypher query and materializes its rows into a
// backend-neutral GraphResult before closing the session.
func (s *neo4jStore) Run(ctx context.Context, cypher string, params map[string]any) (*GraphResult, error) {
	sess := s.session(ctx)
	defer func() { _ = sess.Close(ctx) }()
	result, err := sess.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return collectNeo4jResult(ctx, result)
}

// RunDDL executes a schema-plane Cypher statement (CREATE INDEX, etc.) and
// discards any rows.
func (s *neo4jStore) RunDDL(ctx context.Context, cypher string, params map[string]any) error {
	sess := s.session(ctx)
	defer func() { _ = sess.Close(ctx) }()
	result, err := sess.Run(ctx, cypher, params)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func (s *neo4jStore) HealthCheck(ctx context.Context) error {
	return s.driver.VerifyConnectivity(ctx)
}

func (s *neo4jStore) Close(ctx context.Context) error {
	return s.driver.Close(ctx)
}

// collectNeo4jResult drains a neo4j result into a GraphResult, normalizing
// driver-specific value types so consumers never see neo4j.Node:
//   - neo4j.Node values are flattened to their property map (map[string]any),
//     matching how RETURN <nodeVar> rows are consumed (wave.go reads node.Props).
//
// All other value types (string, int64, []any, map[string]any) pass through
// unchanged — the driver already returns these as plain Go types.
func collectNeo4jResult(ctx context.Context, result neo4j.ResultWithContext) (*GraphResult, error) {
	keys, err := result.Keys()
	if err != nil {
		return nil, err
	}
	var records []GraphRecord
	for result.Next(ctx) {
		rec := result.Record()
		vals := make([]any, len(rec.Values))
		for i, v := range rec.Values {
			vals[i] = normalizeNeo4jValue(v)
		}
		records = append(records, GraphRecord{keys: rec.Keys, values: vals})
	}
	if err := result.Err(); err != nil {
		return nil, err
	}
	return newGraphResult(keys, records), nil
}

// normalizeNeo4jValue flattens a neo4j.Node to its property map so the neutral
// result never carries driver-specific node types.
func normalizeNeo4jValue(v any) any {
	if node, ok := v.(neo4j.Node); ok {
		return map[string]any(node.Props)
	}
	return v
}
