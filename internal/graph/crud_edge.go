package graph

// Edge operations for the code graph. Split out of crud.go, which had reached
// 504 lines — over the project's own 500-line rule, in the repo that ships the
// rule. Node operations stayed in crud.go; this file owns the relationship half.

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Edge represents a directed relationship between two nodes identified by element ID.
type Edge struct {
	FromID     string
	ToID       string
	Type       string // relationship type, e.g. "CALLS", "IMPORTS"
	Properties map[string]any
}

// CreateEdge creates a directed relationship between two existing nodes — but
// ONLY if both nodes belong to this client's project. The edge itself also
// carries a project_id property so range scans on a relationship can filter
// by project (mirrors the node-level scoping in CreateNode).
func (c *Client) CreateEdge(ctx context.Context, e *Edge) error {
	if e.FromID == "" || e.ToID == "" {
		return fmt.Errorf("edge FromID and ToID are required")
	}
	if e.Type == "" {
		return fmt.Errorf("edge Type is required")
	}

	// Shallow-copy props and inject project_id without mutating the caller's map.
	props := make(map[string]any, len(e.Properties)+1)
	for k, v := range e.Properties {
		props[k] = v
	}
	props["project_id"] = c.projectID

	_, cleanup, err := c.runQuery(ctx,
		fmt.Sprintf(
			"MATCH (a) WHERE toString(id(a)) = $from AND a.project_id = $pid "+
				"MATCH (b) WHERE toString(id(b)) = $to AND b.project_id = $pid "+
				"CREATE (a)-[r:%s $props]->(b)",
			e.Type,
		),
		map[string]any{
			"from":  e.FromID,
			"to":    e.ToID,
			"props": props,
		},
	)
	if err != nil {
		return fmt.Errorf("create edge %s: %w", e.Type, err)
	}
	cleanup()
	return nil
}

// UpsertEdge creates a directed relationship between two existing nodes, or
// updates its properties if an identical (from, type, to) edge already exists.
// Like CreateEdge, both endpoints must belong to this client's project.
// Use this instead of CreateEdge when the operation may be retried (e.g. after
// an outbox reconcile), to avoid duplicate edges in the graph.
func (c *Client) UpsertEdge(ctx context.Context, e *Edge) error {
	if e.FromID == "" || e.ToID == "" {
		return fmt.Errorf("edge FromID and ToID are required")
	}
	if e.Type == "" {
		return fmt.Errorf("edge Type is required")
	}

	props := make(map[string]any, len(e.Properties)+1)
	for k, v := range e.Properties {
		props[k] = v
	}
	props["project_id"] = c.projectID

	_, cleanup, err := c.runQuery(ctx,
		fmt.Sprintf(
			"MATCH (a) WHERE toString(id(a)) = $from AND a.project_id = $pid "+
				"MATCH (b) WHERE toString(id(b)) = $to AND b.project_id = $pid "+
				"MERGE (a)-[r:%s]->(b) SET r += $props",
			e.Type,
		),
		map[string]any{
			"from":  e.FromID,
			"to":    e.ToID,
			"props": props,
		},
	)
	if err != nil {
		return fmt.Errorf("upsert edge %s: %w", e.Type, err)
	}
	cleanup()
	return nil
}

// UpsertEdgeByKey merges a directed relationship between two nodes identified
// by stable domain-key merge properties (not internal element IDs). Both nodes
// must already exist in Memgraph for this project. The Cypher uses MATCH on the
// merge-identity map for each endpoint, then MERGE for the relationship.
//
// srcIdentity and dstIdentity must contain the label-specific merge keys (e.g.
// {"path": "..."} for File, {"source_file": "...", "name": "..."} for Symbol).
// project_id is injected automatically.
func (c *Client) UpsertEdgeByKey(
	ctx context.Context,
	srcLabel string, srcIdentity map[string]any,
	dstLabel string, dstIdentity map[string]any,
	edgeType string, edgeProps map[string]any,
) error {
	if srcLabel == "" || dstLabel == "" {
		return fmt.Errorf("UpsertEdgeByKey: src and dst labels are required")
	}
	if edgeType == "" {
		return fmt.Errorf("UpsertEdgeByKey: edgeType is required")
	}

	params := map[string]any{}

	// Build parameterised WHERE clauses for src and dst match conditions.
	// Sorting the keys makes the generated Cypher deterministic.
	srcKeys := make([]string, 0, len(srcIdentity))
	for k := range srcIdentity {
		srcKeys = append(srcKeys, k)
	}
	sort.Strings(srcKeys)
	srcClauses := make([]string, 0, len(srcKeys))
	for _, k := range srcKeys {
		paramName := "src_" + k
		srcClauses = append(srcClauses, fmt.Sprintf("a.%s = $%s", k, paramName))
		params[paramName] = srcIdentity[k]
	}

	dstKeys := make([]string, 0, len(dstIdentity))
	for k := range dstIdentity {
		dstKeys = append(dstKeys, k)
	}
	sort.Strings(dstKeys)
	dstClauses := make([]string, 0, len(dstKeys))
	for _, k := range dstKeys {
		paramName := "dst_" + k
		dstClauses = append(dstClauses, fmt.Sprintf("b.%s = $%s", k, paramName))
		params[paramName] = dstIdentity[k]
	}

	rProps := make(map[string]any, len(edgeProps)+1)
	for k, v := range edgeProps {
		rProps[k] = v
	}
	rProps["project_id"] = c.projectID
	params["rprops"] = rProps

	cypher := fmt.Sprintf(
		"MATCH (a:%s {project_id: $pid}) WHERE %s\n"+
			"MATCH (b:%s {project_id: $pid}) WHERE %s\n"+
			"MERGE (a)-[r:%s]->(b) SET r += $rprops",
		srcLabel, strings.Join(srcClauses, " AND "),
		dstLabel, strings.Join(dstClauses, " AND "),
		edgeType,
	)

	_, cleanup, err := c.runQuery(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("upsert edge by key %s: %w", edgeType, err)
	}
	cleanup()
	return nil
}
