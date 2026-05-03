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
