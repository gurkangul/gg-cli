package graph

import "context"

// GraphStore is the backend-agnostic graph execution abstraction used by every
// query in this package. It is the graph-side analogue of store.VectorStore
// (TASK-493): the single choke-point through which all Cypher flows, with two
// interchangeable implementations behind it.
//
//   - neo4jStore  — a thin wrapper over the neo4j/Bolt driver that runs the
//     Cypher verbatim against Memgraph. It is the default and the parity oracle
//     the embedded backend is validated against (TASK-494).
//   - sqliteStore — a pure-Go, CGO-free embedded graph over modernc.org/sqlite
//     (already a dependency). Every Cypher template this package emits maps to a
//     handler that reads/writes a normalized nodes/edges schema. All queries are
//     single-hop, so an indexed edge lookup is a faithful replacement — the
//     multi-depth BFS in symbol_flow.go / dependents.go stays in Go.
//
// Run is the data-plane entry point (project_id is injected by Client.runQuery
// before the call). RunDDL is the schema-plane entry point for index DDL, which
// carries no project scope. Results are returned eagerly as a backend-neutral
// GraphResult so the neo4j session can be closed immediately and the SQLite
// backend never has to fake the driver's unexported result interface.
type GraphStore interface {
	// Run executes a data-plane query (MATCH / MERGE / CREATE / DELETE). The
	// params map already contains "pid" => project_id. The returned GraphResult
	// is fully materialized; the backend owns no open cursor after Run returns.
	Run(ctx context.Context, cypher string, params map[string]any) (*GraphResult, error)
	// RunDDL executes a schema-plane statement (CREATE INDEX, etc.). It returns
	// no rows and carries no project scope.
	RunDDL(ctx context.Context, cypher string, params map[string]any) error
	// HealthCheck verifies the backend is reachable.
	HealthCheck(ctx context.Context) error
	// Close releases backend resources.
	Close(ctx context.Context) error
}

// GraphRecord is one row of a query result: an ordered set of named values.
// It mirrors the subset of neo4j.Record this package actually consumes — Get by
// key and typed extraction — without depending on the driver's result type
// (whose interface has unexported methods and cannot be implemented externally).
type GraphRecord struct {
	keys   []string
	values []any
}

// Get returns the value for key and whether it was present.
func (r GraphRecord) Get(key string) (any, bool) {
	for i, k := range r.keys {
		if k == key {
			return r.values[i], true
		}
	}
	return nil, false
}

// GraphResult is a fully-materialized, single-use cursor over query rows. It is
// iterated exactly like neo4j.ResultWithContext (Next / Record / Err / Single)
// so the consumption loops in the Client methods change only their value
// extraction, not their control flow.
type GraphResult struct {
	keys    []string
	records []GraphRecord
	pos     int // -1 before the first Next()
	err     error
}

// newGraphResult builds a result from already-collected rows.
func newGraphResult(keys []string, records []GraphRecord) *GraphResult {
	return &GraphResult{keys: keys, records: records, pos: -1}
}

// Next advances to the next record, returning false when exhausted.
func (r *GraphResult) Next(ctx context.Context) bool {
	if r.err != nil {
		return false
	}
	if r.pos+1 >= len(r.records) {
		r.pos = len(r.records)
		return false
	}
	r.pos++
	return true
}

// Record returns the record at the current cursor position. Valid only after a
// Next() that returned true.
func (r *GraphResult) Record() GraphRecord {
	if r.pos < 0 || r.pos >= len(r.records) {
		return GraphRecord{}
	}
	return r.records[r.pos]
}

// Err returns the error that terminated iteration, if any.
func (r *GraphResult) Err() error { return r.err }

// Single returns the sole remaining record, erroring if there is not exactly
// one. It mirrors neo4j.ResultWithContext.Single, including its contract that a
// zero-row result is an error (callers that treat "not found" as non-error must
// check before calling, as the existing code does).
func (r *GraphResult) Single(ctx context.Context) (GraphRecord, error) {
	if r.err != nil {
		return GraphRecord{}, r.err
	}
	remaining := len(r.records) - (r.pos + 1)
	if remaining != 1 {
		return GraphRecord{}, errSingleRecord(remaining)
	}
	r.pos++
	return r.records[r.pos], nil
}
