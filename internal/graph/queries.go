package graph

// queries.go — single choke-point for all Memgraph Cypher execution.
//
// # Isolation contract
//
// Every MATCH / CREATE / MERGE operation in this package MUST go through
// runQuery or runQueryNoPID — calling sess.Run directly is forbidden outside
// of these two helpers. This gives a single instrumentation and audit point.
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
// Note: Go's package visibility already prevents code outside this package from
// obtaining a session. The choke-point here is about intra-package discipline.

import (
	"context"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// runQuery executes a Cypher query and returns the result. The caller must
// consume the result and then call cleanup(), which closes the underlying
// session.
//
// All data-plane queries (MATCH, CREATE, MERGE, DELETE) must use this method.
// Do not call sess.Run directly — use runQuery or runQueryNoPID.
func (c *Client) runQuery(ctx context.Context, cypher string, params map[string]any) (neo4j.ResultWithContext, func(), error) {
	sess := c.session(ctx)
	result, err := sess.Run(ctx, cypher, params)
	cleanup := func() { _ = sess.Close(ctx) }
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return result, cleanup, nil
}

// runQueryNoPID executes a Cypher query that legitimately has no project_id
// scope — i.e. schema DDL (CREATE INDEX, SHOW INDEX) and connectivity checks.
//
// Must NOT be used for data reads or writes — those require project isolation.
func (c *Client) runQueryNoPID(ctx context.Context, cypher string, params map[string]any) (neo4j.ResultWithContext, func(), error) {
	sess := c.session(ctx)
	result, err := sess.Run(ctx, cypher, params)
	cleanup := func() { _ = sess.Close(ctx) }
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return result, cleanup, nil
}
