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
