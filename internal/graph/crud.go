package graph

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Node represents a graph node with a label and arbitrary properties.
// TASK-007 will define the concrete label taxonomy (Symbol, File, Package, etc.).
type Node struct {
	ID         string            // populated after CreateNode
	Label      string            // e.g. "Symbol", "File"
	Properties map[string]any    // Cypher-compatible property map
}

// CreateNode creates a node with the given label and properties.
// It assigns a server-generated internal ID to n.ID on success.
func (c *Client) CreateNode(ctx context.Context, n *Node) error {
	if n.Label == "" {
		return fmt.Errorf("node label is required")
	}

	sess := c.session(ctx)
	defer sess.Close(ctx)

	result, err := sess.Run(ctx,
		fmt.Sprintf("CREATE (n:%s $props) RETURN toString(id(n)) AS id", n.Label),
		map[string]any{"props": n.Properties},
	)
	if err != nil {
		return fmt.Errorf("create node %s: %w", n.Label, err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		return fmt.Errorf("create node single record: %w", err)
	}
	id, _, err := neo4j.GetRecordValue[string](record, "id")
	if err != nil {
		return fmt.Errorf("create node get id: %w", err)
	}
	n.ID = id
	return nil
}

// FindNodeByProperty returns the first node with the given label whose
// property key matches value. Returns (nil, nil) when not found.
func (c *Client) FindNodeByProperty(ctx context.Context, label, key string, value any) (*Node, error) {
	sess := c.session(ctx)
	defer sess.Close(ctx)

	result, err := sess.Run(ctx,
		fmt.Sprintf(
			"MATCH (n:%s {%s: $val}) RETURN toString(id(n)) AS id, properties(n) AS props LIMIT 1",
			label, key,
		),
		map[string]any{"val": value},
	)
	if err != nil {
		return nil, fmt.Errorf("find node %s.%s: %w", label, key, err)
	}

	record, err := result.Single(ctx)
	if err == nil {
		id, _, _ := neo4j.GetRecordValue[string](record, "id")
		props, _, _ := neo4j.GetRecordValue[map[string]any](record, "props")
		return &Node{ID: id, Label: label, Properties: props}, nil
	}
	// No results is not an error in our contract.
	summary, _ := result.Consume(ctx)
	_ = summary
	return nil, nil
}

// DeleteNode removes the node with the given element ID and all its relationships.
func (c *Client) DeleteNode(ctx context.Context, elementID string) error {
	if elementID == "" {
		return fmt.Errorf("elementID is required")
	}
	sess := c.session(ctx)
	defer sess.Close(ctx)

	_, err := sess.Run(ctx,
		"MATCH (n) WHERE toString(id(n)) = $id DETACH DELETE n",
		map[string]any{"id": elementID},
	)
	if err != nil {
		return fmt.Errorf("delete node %s: %w", elementID, err)
	}
	return nil
}

// Edge represents a directed relationship between two nodes identified by element ID.
type Edge struct {
	FromID     string
	ToID       string
	Type       string         // relationship type, e.g. "CALLS", "IMPORTS"
	Properties map[string]any
}

// CreateEdge creates a directed relationship between two existing nodes.
func (c *Client) CreateEdge(ctx context.Context, e *Edge) error {
	if e.FromID == "" || e.ToID == "" {
		return fmt.Errorf("edge FromID and ToID are required")
	}
	if e.Type == "" {
		return fmt.Errorf("edge Type is required")
	}

	sess := c.session(ctx)
	defer sess.Close(ctx)

	_, err := sess.Run(ctx,
		fmt.Sprintf(
			"MATCH (a) WHERE toString(id(a)) = $from "+
				"MATCH (b) WHERE toString(id(b)) = $to "+
				"CREATE (a)-[r:%s $props]->(b)",
			e.Type,
		),
		map[string]any{
			"from":  e.FromID,
			"to":    e.ToID,
			"props": e.Properties,
		},
	)
	if err != nil {
		return fmt.Errorf("create edge %s: %w", e.Type, err)
	}
	return nil
}
