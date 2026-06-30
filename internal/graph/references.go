package graph

import (
	"context"
	"fmt"
)

// ReferencersOf returns the paths of files that reference the given Symbol node
// (1-hop reverse lookup over REFERENCES edges). This is the symbol-exact answer
// to "who uses symbol X": because the edge targets the specific Symbol — not its
// file — a barrel/re-export that re-exports the symbol never makes a consumer of
// a *sibling* symbol appear here. symbolID is a Symbol node element id (as
// returned by FindSymbols).
//
// An empty result on an unbuilt or syntactic-only graph is not proof of "no
// users"; REFERENCES edges are written only for the semantic (SCIP) tier.
func (c *Client) ReferencersOf(ctx context.Context, symbolID string) ([]string, error) {
	if symbolID == "" {
		return nil, fmt.Errorf("referencers of: symbolID is required")
	}
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (d:File {project_id: $pid})-[:REFERENCES]->(s:Symbol {project_id: $pid}) "+
			"WHERE toString(id(s)) = $id RETURN DISTINCT d.path AS dep ORDER BY dep",
		map[string]any{"id": symbolID},
	)
	if err != nil {
		return nil, fmt.Errorf("referencers of %s: %w", symbolID, err)
	}
	defer cleanup()

	var refs []string
	for result.Next(ctx) {
		dep, _, _ := recordValue[string](result.Record(), "dep")
		if dep != "" {
			refs = append(refs, dep)
		}
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("referencers of %s iterate: %w", symbolID, err)
	}
	return refs, nil
}
