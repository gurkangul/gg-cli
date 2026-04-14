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
// It automatically stamps the node with {project_id: c.projectID} so that
// every node is scoped to this project.
// It assigns a server-generated internal ID to n.ID on success.
func (c *Client) CreateNode(ctx context.Context, n *Node) error {
	if n.Label == "" {
		return fmt.Errorf("node label is required")
	}

	// Shallow-copy props and inject project_id without mutating the caller's map.
	props := make(map[string]any, len(n.Properties)+1)
	for k, v := range n.Properties {
		props[k] = v
	}
	props["project_id"] = c.projectID

	sess := c.session(ctx)
	defer sess.Close(ctx)

	result, err := sess.Run(ctx,
		fmt.Sprintf("CREATE (n:%s $props) RETURN toString(id(n)) AS id", n.Label),
		map[string]any{"props": props},
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
// property key matches value, scoped to this client's project_id.
// Returns (nil, nil) when not found.
func (c *Client) FindNodeByProperty(ctx context.Context, label, key string, value any) (*Node, error) {
	sess := c.session(ctx)
	defer sess.Close(ctx)

	result, err := sess.Run(ctx,
		fmt.Sprintf(
			"MATCH (n:%s {%s: $val, project_id: $pid}) RETURN toString(id(n)) AS id, properties(n) AS props LIMIT 1",
			label, key,
		),
		map[string]any{"val": value, "pid": c.projectID},
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

// DeleteNode removes the node with the given element ID — but ONLY if it
// belongs to this client's project. Cross-project deletion is impossible:
// a request matching a node in a different project becomes a no-op.
func (c *Client) DeleteNode(ctx context.Context, elementID string) error {
	if elementID == "" {
		return fmt.Errorf("elementID is required")
	}
	sess := c.session(ctx)
	defer sess.Close(ctx)

	_, err := sess.Run(ctx,
		"MATCH (n) WHERE toString(id(n)) = $id AND n.project_id = $pid DETACH DELETE n",
		map[string]any{"id": elementID, "pid": c.projectID},
	)
	if err != nil {
		return fmt.Errorf("delete node %s: %w", elementID, err)
	}
	return nil
}

// InvalidateFile removes all Symbol and File nodes for the given source file
// path (scoped to this project), along with any relationships they participate in.
// This is the "reaping" step from CHANGED_CONTRACT.md §3 — call before re-indexing
// a changed file to ensure the graph reflects only the current version of the file.
//
// The operation is idempotent: running it twice produces the same graph state.
// If the file no longer exists on disk, call this and skip the SCIP run.
func (c *Client) InvalidateFile(ctx context.Context, filePath string) error {
	sess := c.session(ctx)
	defer sess.Close(ctx)

	// Step 1: delete all Symbol nodes produced from this file.
	_, err := sess.Run(ctx,
		"MATCH (n:Symbol {source_file: $path, project_id: $pid}) DETACH DELETE n",
		map[string]any{"path": filePath, "pid": c.projectID},
	)
	if err != nil {
		return fmt.Errorf("invalidate symbols for %s: %w", filePath, err)
	}

	// Step 2: delete the File node itself.
	_, err = sess.Run(ctx,
		"MATCH (f:File {path: $path, project_id: $pid}) DETACH DELETE f",
		map[string]any{"path": filePath, "pid": c.projectID},
	)
	if err != nil {
		return fmt.Errorf("invalidate file node for %s: %w", filePath, err)
	}
	return nil
}

// DependentsOf returns the paths of files that directly import the given file
// (1-hop dependent lookup, CHANGED_CONTRACT.md §2). Used to expand the
// invalidation set during --changed runs.
func (c *Client) DependentsOf(ctx context.Context, filePath string) ([]string, error) {
	sess := c.session(ctx)
	defer sess.Close(ctx)

	result, err := sess.Run(ctx,
		"MATCH (d:File {project_id: $pid})-[:IMPORTS]->(f:File {path: $path, project_id: $pid}) RETURN d.path AS dep",
		map[string]any{"path": filePath, "pid": c.projectID},
	)
	if err != nil {
		return nil, fmt.Errorf("dependents of %s: %w", filePath, err)
	}

	var deps []string
	for result.Next(ctx) {
		dep, _, _ := neo4j.GetRecordValue[string](result.Record(), "dep")
		if dep != "" {
			deps = append(deps, dep)
		}
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("dependents of %s iterate: %w", filePath, err)
	}
	return deps, nil
}

// Edge represents a directed relationship between two nodes identified by element ID.
type Edge struct {
	FromID     string
	ToID       string
	Type       string         // relationship type, e.g. "CALLS", "IMPORTS"
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

	sess := c.session(ctx)
	defer sess.Close(ctx)

	_, err := sess.Run(ctx,
		fmt.Sprintf(
			"MATCH (a) WHERE toString(id(a)) = $from AND a.project_id = $pid "+
				"MATCH (b) WHERE toString(id(b)) = $to AND b.project_id = $pid "+
				"CREATE (a)-[r:%s $props]->(b)",
			e.Type,
		),
		map[string]any{
			"from":  e.FromID,
			"to":    e.ToID,
			"pid":   c.projectID,
			"props": props,
		},
	)
	if err != nil {
		return fmt.Errorf("create edge %s: %w", e.Type, err)
	}
	return nil
}
