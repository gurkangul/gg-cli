package graph

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Stats summarizes the project-scoped code graph.
type Stats struct {
	Files   int64 `json:"files"`
	Symbols int64 `json:"symbols"`
	Edges   int64 `json:"edges"`
}

// Stats returns project-scoped File, Symbol, and relationship counts.
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	files, err := c.countScalar(ctx, "MATCH (f:File {project_id: $pid}) RETURN count(f) AS n")
	if err != nil {
		return Stats{}, fmt.Errorf("count file nodes: %w", err)
	}
	symbols, err := c.countScalar(ctx, "MATCH (s:Symbol {project_id: $pid}) RETURN count(s) AS n")
	if err != nil {
		return Stats{}, fmt.Errorf("count symbol nodes: %w", err)
	}
	edges, err := c.countScalar(ctx, "MATCH ()-[r {project_id: $pid}]->() RETURN count(r) AS n")
	if err != nil {
		return Stats{}, fmt.Errorf("count graph edges: %w", err)
	}
	return Stats{Files: files, Symbols: symbols, Edges: edges}, nil
}

// BrainGraphStats returns Decision-node and brain-relationship-edge counts — a
// cheap drift signal (TASK-482): decision nodes present but zero brain edges
// means the relationship graph needs reconciling via gg doctor --fix-index.
func (c *Client) BrainGraphStats(ctx context.Context) (decisionNodes, brainEdges int64, err error) {
	decisionNodes, err = c.countScalar(ctx, "MATCH (n:Decision {project_id: $pid}) RETURN count(n) AS n")
	if err != nil {
		return 0, 0, fmt.Errorf("count decision nodes: %w", err)
	}
	brainEdges, err = c.countScalar(ctx, "MATCH (a {project_id: $pid})-[r]->(b {project_id: $pid}) WHERE type(r) IN ['DECIDES','DEPENDS_ON','BLOCKS','IMPLEMENTS','REJECTS'] RETURN count(r) AS n")
	if err != nil {
		return decisionNodes, 0, fmt.Errorf("count brain edges: %w", err)
	}
	return decisionNodes, brainEdges, nil
}

func (c *Client) countScalar(ctx context.Context, cypher string) (int64, error) {
	result, cleanup, err := c.runQuery(ctx, cypher, nil)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	record, err := result.Single(ctx)
	if err != nil {
		return 0, err
	}
	n, _, err := neo4j.GetRecordValue[int64](record, "n")
	if err != nil {
		return 0, err
	}
	return n, nil
}
