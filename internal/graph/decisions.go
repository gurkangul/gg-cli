package graph

import (
	"context"
	"fmt"
	"time"
)

// UpsertDecisionNode upserts a Decision node identified by its Qdrant UUID.
// Returns the node for chaining with cross-link helpers.
func (c *Client) UpsertDecisionNode(ctx context.Context, qdrantID, title string) error {
	n := DecisionNode(qdrantID, title)
	return c.UpsertNode(ctx, n, []string{"qdrant_id"})
}

// UpsertTaskNode upserts a Task node identified by its Qdrant UUID.
func (c *Client) UpsertTaskNode(ctx context.Context, qdrantID, title string) error {
	n := TaskNode(qdrantID, title)
	return c.UpsertNode(ctx, n, []string{"qdrant_id"})
}

// UpsertDecidesEdge creates or updates a (Decision)-[:DECIDES]->(Task) edge.
// Both nodes must already exist in this project. The edge carries project_id
// (auto-injected by runQuery) and created_at.
func (c *Client) UpsertDecidesEdge(ctx context.Context, decisionQdrantID, taskQdrantID string) error {
	return c.upsertKnowledgeEdge(ctx,
		LabelDecision, decisionQdrantID,
		LabelTask, taskQdrantID,
		RelDecides,
	)
}

// UpsertImplementsEdge creates or updates a (Task)-[:IMPLEMENTS]->(Decision) edge.
func (c *Client) UpsertImplementsEdge(ctx context.Context, taskQdrantID, decisionQdrantID string) error {
	return c.upsertKnowledgeEdge(ctx,
		LabelTask, taskQdrantID,
		LabelDecision, decisionQdrantID,
		RelImplements,
	)
}

// UpsertRejectsEdge creates or updates a (Decision)-[:REJECTS]->(Decision) edge.
func (c *Client) UpsertRejectsEdge(ctx context.Context, rejectingQdrantID, rejectedQdrantID string) error {
	return c.upsertKnowledgeEdge(ctx,
		LabelDecision, rejectingQdrantID,
		LabelDecision, rejectedQdrantID,
		RelRejects,
	)
}

// UpsertDependsOnEdge creates or updates a (Task)-[:DEPENDS_ON]->(Task) edge.
// Semantics: source task waits on target task.
func (c *Client) UpsertDependsOnEdge(ctx context.Context, waiterQdrantID, waitedOnQdrantID string) error {
	return c.upsertKnowledgeEdge(ctx,
		LabelTask, waiterQdrantID,
		LabelTask, waitedOnQdrantID,
		RelDependsOn,
	)
}

// UpsertBlocksEdge creates or updates a (Task)-[:BLOCKS]->(Task) edge.
// Semantics: source task gates target task.
func (c *Client) UpsertBlocksEdge(ctx context.Context, gaterQdrantID, gatedQdrantID string) error {
	return c.upsertKnowledgeEdge(ctx,
		LabelTask, gaterQdrantID,
		LabelTask, gatedQdrantID,
		RelBlocks,
	)
}

// DeleteTaskNode removes a Task node (and all its relationships) from the graph
// for this project. Identified by qdrant_id (the task ID, e.g. "TASK-235").
// Idempotent: a no-op if the node doesn't exist.
func (c *Client) DeleteTaskNode(ctx context.Context, taskQdrantID string) error {
	if taskQdrantID == "" {
		return fmt.Errorf("task id is required")
	}
	_, cleanup, err := c.runQuery(ctx,
		"MATCH (t:Task {qdrant_id: $tid, project_id: $pid}) DETACH DELETE t",
		map[string]any{"tid": taskQdrantID},
	)
	if err != nil {
		return fmt.Errorf("delete Task node %s: %w", taskQdrantID, err)
	}
	cleanup()
	return nil
}

// TaskDependents returns task IDs that depend on (or are blocked by) the given
// task — i.e. the downstream blast radius of closing or rejecting it. Both
// DEPENDS_ON (reverse) and BLOCKS (forward) are surfaced.
func (c *Client) TaskDependents(ctx context.Context, taskQdrantID string) ([]string, error) {
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (waiter:Task {project_id: $pid})-[:DEPENDS_ON]->(t:Task {qdrant_id: $tid, project_id: $pid}) "+
			"RETURN DISTINCT waiter.qdrant_id AS id "+
			"UNION "+
			"MATCH (t:Task {qdrant_id: $tid, project_id: $pid})-[:BLOCKS]->(gated:Task {project_id: $pid}) "+
			"RETURN DISTINCT gated.qdrant_id AS id",
		map[string]any{"tid": taskQdrantID},
	)
	if err != nil {
		return nil, fmt.Errorf("task dependents query: %w", err)
	}
	defer cleanup()

	var ids []string
	for result.Next(ctx) {
		record := result.Record()
		id, ok := record.Get("id")
		if !ok {
			continue
		}
		if s, isStr := id.(string); isStr && s != "" {
			ids = append(ids, s)
		}
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("task dependents iterate: %w", err)
	}
	return ids, nil
}

// TaskSiblings returns task IDs that share a Decision with the given task — i.e.
// other tasks that implement the same decisions (via IMPLEMENTS edges in both
// directions: task→decision and decision→task via DECIDES).
func (c *Client) TaskSiblings(ctx context.Context, taskID string) ([]string, error) {
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (t:Task {qdrant_id: $tid, project_id: $pid})-[:IMPLEMENTS]->(d:Decision {project_id: $pid})<-[:IMPLEMENTS]-(sibling:Task {project_id: $pid}) "+
			"WHERE sibling.qdrant_id <> $tid "+
			"RETURN DISTINCT sibling.qdrant_id AS id "+
			"UNION "+
			"MATCH (d:Decision {project_id: $pid})-[:DECIDES]->(t:Task {qdrant_id: $tid, project_id: $pid}) "+
			"MATCH (d)-[:DECIDES]->(sibling:Task {project_id: $pid}) "+
			"WHERE sibling.qdrant_id <> $tid "+
			"RETURN DISTINCT sibling.qdrant_id AS id",
		map[string]any{"tid": taskID},
	)
	if err != nil {
		return nil, fmt.Errorf("task siblings query: %w", err)
	}
	defer cleanup()

	var ids []string
	for result.Next(ctx) {
		record := result.Record()
		id, ok := record.Get("id")
		if !ok {
			continue
		}
		if s, isStr := id.(string); isStr && s != "" {
			ids = append(ids, s)
		}
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("task siblings iterate: %w", err)
	}
	return ids, nil
}

// UpsertDecidesEdgeBackfill creates or updates a (Decision)-[:DECIDES]->(Task) edge
// and stamps created_by=backfill_v1 so it can be identified and rolled back.
func (c *Client) UpsertDecidesEdgeBackfill(ctx context.Context, decisionQdrantID, taskQdrantID string) error {
	return c.upsertBackfillEdge(ctx,
		LabelDecision, decisionQdrantID,
		LabelTask, taskQdrantID,
		RelDecides,
	)
}

// upsertBackfillEdge is like upsertKnowledgeEdge but also stamps created_by=backfill_v1
// on the relationship so backfill-created edges can be identified and rolled back.
func (c *Client) upsertBackfillEdge(
	ctx context.Context,
	srcLabel, srcQdrantID string,
	dstLabel, dstQdrantID string,
	edgeType string,
) error {
	_, cleanup, err := c.runQuery(ctx,
		fmt.Sprintf(
			"MATCH (a:%s {qdrant_id: $src_id, project_id: $pid}) "+
				"MATCH (b:%s {qdrant_id: $dst_id, project_id: $pid}) "+
				"MERGE (a)-[r:%s]->(b) "+
				"SET r.project_id = $pid, r.created_at = $ts, r.created_by = $created_by",
			srcLabel, dstLabel, edgeType,
		),
		map[string]any{
			"src_id":     srcQdrantID,
			"dst_id":     dstQdrantID,
			"ts":         time.Now().UTC().Format(time.RFC3339),
			"created_by": "backfill_v1",
		},
	)
	if err != nil {
		return fmt.Errorf("upsert backfill %s edge (%s→%s): %w", edgeType, srcQdrantID, dstQdrantID, err)
	}
	cleanup()
	return nil
}

// upsertKnowledgeEdge is the shared implementation for knowledge-graph cross-link edges.
// Both endpoints are looked up by their qdrant_id within the same project_id — a
// cross-project edge is structurally impossible because the MATCH filters on $pid.
func (c *Client) upsertKnowledgeEdge(
	ctx context.Context,
	srcLabel, srcQdrantID string,
	dstLabel, dstQdrantID string,
	edgeType string,
) error {
	_, cleanup, err := c.runQuery(ctx,
		fmt.Sprintf(
			"MATCH (a:%s {qdrant_id: $src_id, project_id: $pid}) "+
				"MATCH (b:%s {qdrant_id: $dst_id, project_id: $pid}) "+
				"MERGE (a)-[r:%s]->(b) "+
				"SET r.project_id = $pid, r.created_at = $ts",
			srcLabel, dstLabel, edgeType,
		),
		map[string]any{
			"src_id": srcQdrantID,
			"dst_id": dstQdrantID,
			"ts":     time.Now().UTC().Format(time.RFC3339),
		},
	)
	if err != nil {
		return fmt.Errorf("upsert %s edge (%s→%s): %w", edgeType, srcQdrantID, dstQdrantID, err)
	}
	cleanup()
	return nil
}
