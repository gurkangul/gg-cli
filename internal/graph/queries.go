package graph

// queries.go — single choke-point for all graph query execution.
//
// # Isolation contract
//
// Every MATCH / CREATE / MERGE operation in this package MUST go through
// runQuery or runQueryNoPID — calling the backend's Run directly is forbidden
// outside of these two helpers. This gives a single instrumentation and audit
// point and is where project_id is injected.
//
// project_id scoping rules (enforced by convention, not the DB):
//
//   - MATCH / MERGE queries: include {project_id: $pid} in every node pattern
//     and pass "pid": c.projectID in the params map.
//   - CREATE queries: inject project_id into the $props map before calling;
//     no $pid param is needed because the property is inside $props.
//   - DDL (CREATE INDEX, SHOW INDEX, etc.): use runQueryNoPID — these are
//     schema-level, not data-level, and carry no project scope.
//
// If a new query is added that forgets project_id on a MATCH/MERGE, the
// resulting cross-project read/write is a scoping bug — identifiable in
// code review by checking whether the node pattern or WHERE clause filters
// on project_id.
//
// Backend independence (TASK-494): runQuery delegates to the configured
// GraphStore (Memgraph via neo4j, or the embedded SQLite store) and returns a
// backend-neutral GraphResult. The Cypher strings are the interface currency —
// both backends consume the identical Cypher template set.

import (
	"context"
	"time"

	"github.com/gurkangul/gg-cli/internal/trace"
)

// runQuery executes a Cypher query and returns a fully-materialized,
// backend-neutral result. The cleanup func is retained for call-site symmetry
// with the previous neo4j session lifecycle; it is now a no-op because results
// are materialized eagerly (no cursor stays open after Run returns), but callers
// keep deferring it so the choke-point shape and call-sites are unchanged.
//
// All data-plane queries (MATCH, CREATE, MERGE, DELETE) must use this method.
//
// project_id isolation: runQuery automatically injects {"pid": c.projectID}
// into every query so callers never need to pass it manually. Cypher strings
// that filter on project scope should reference $pid in their WHERE / pattern.
func (c *Client) runQuery(ctx context.Context, cypher string, params map[string]any) (*GraphResult, func(), error) {
	params = c.injectPID(params)
	start := time.Now()
	result, err := c.gs.Run(ctx, cypher, params)
	trace.Record("graph.query", start, err)
	noop := func() {}
	if err != nil {
		return nil, noop, err
	}
	return result, noop, nil
}

// injectPID returns a shallow copy of params with "pid" set to c.projectID.
// If params is nil a new map is allocated. The original map is never mutated.
func (c *Client) injectPID(params map[string]any) map[string]any {
	out := make(map[string]any, len(params)+1)
	for k, v := range params {
		out[k] = v
	}
	out["pid"] = c.projectID
	return out
}

// runQueryNoPID executes a Cypher statement that legitimately has no project_id
// scope — i.e. schema DDL (CREATE INDEX, SHOW INDEX). It returns no rows.
//
// Must NOT be used for data reads or writes — those require project isolation.
func (c *Client) runQueryNoPID(ctx context.Context, cypher string, params map[string]any) (func(), error) {
	noop := func() {}
	if err := c.gs.RunDDL(ctx, cypher, params); err != nil {
		return noop, err
	}
	return noop, nil
}
