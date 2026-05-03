package graph

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Node represents a graph node with a label and arbitrary properties.
// TASK-007 will define the concrete label taxonomy (Symbol, File, Package, etc.).
type Node struct {
	ID         string         // populated after CreateNode / UpsertNode
	Label      string         // e.g. "Symbol", "File"
	Properties map[string]any // Cypher-compatible property map
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

	// CREATE query: project_id is inside $props (not a WHERE filter), so
	// runQuery is used even though no explicit $pid param appears in the Cypher.
	result, cleanup, err := c.runQuery(ctx,
		fmt.Sprintf("CREATE (n:%s $props) RETURN toString(id(n)) AS id", n.Label),
		map[string]any{"props": props},
	)
	if err != nil {
		return fmt.Errorf("create node %s: %w", n.Label, err)
	}
	defer cleanup()

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

// UpsertNode merges a node by the given mergeKeys (plus the implicit project_id),
// creating it when absent or updating all properties when it already exists.
// This makes index writes safe to retry without generating duplicate nodes.
//
// mergeKeys must name properties present in n.Properties. The combination of
// mergeKeys + project_id is treated as the unique identity for the MERGE.
func (c *Client) UpsertNode(ctx context.Context, n *Node, mergeKeys []string) error {
	if n.Label == "" {
		return fmt.Errorf("node label is required")
	}
	if len(mergeKeys) == 0 {
		return fmt.Errorf("upsert node: at least one merge key is required")
	}

	// Build full property map with project_id stamped in.
	props := make(map[string]any, len(n.Properties)+1)
	for k, v := range n.Properties {
		props[k] = v
	}
	props["project_id"] = c.projectID

	// Build the MERGE identity: project_id + caller-specified keys.
	// All mergeKeys must be present in props — a missing key would silently
	// produce a wrong MERGE identity, causing either duplicate nodes or
	// incorrect matches. Fail loudly so the caller can fix the call site.
	identity := make(map[string]any, len(mergeKeys)+1)
	identity["project_id"] = c.projectID
	for _, k := range mergeKeys {
		v, ok := props[k]
		if !ok {
			return fmt.Errorf("upsert node %s: merge key %q not present in properties", n.Label, k)
		}
		identity[k] = v
	}

	// Build the MERGE clause with explicit property matching — Memgraph does not
	// support map-literal matching in MERGE (e.g. MERGE (n:L $map)).
	// Sort keys for deterministic Cypher output.
	idKeys := make([]string, 0, len(identity))
	for k := range identity {
		idKeys = append(idKeys, k)
	}
	sort.Strings(idKeys)
	mergeParams := make(map[string]any, len(idKeys)+1)
	mergeParams["props"] = props
	idParts := make([]string, 0, len(idKeys))
	for _, k := range idKeys {
		paramName := "id_" + k
		idParts = append(idParts, fmt.Sprintf("%s: $%s", k, paramName))
		mergeParams[paramName] = identity[k]
	}
	cypher := fmt.Sprintf(
		"MERGE (n:%s {%s}) SET n += $props RETURN toString(id(n)) AS id",
		n.Label, strings.Join(idParts, ", "),
	)

	// runQuery is used; project_id is in the MERGE identity keys above.
	result, cleanup, err := c.runQuery(ctx, cypher, mergeParams)
	if err != nil {
		return fmt.Errorf("upsert node %s: %w", n.Label, err)
	}
	defer cleanup()

	record, err := result.Single(ctx)
	if err != nil {
		return fmt.Errorf("upsert node %s single record: %w", n.Label, err)
	}
	id, _, err := neo4j.GetRecordValue[string](record, "id")
	if err != nil {
		return fmt.Errorf("upsert node %s get id: %w", n.Label, err)
	}
	n.ID = id
	return nil
}

// FindNodeByProperty returns the first node with the given label whose
// property key matches value, scoped to this client's project_id.
// Returns (nil, nil) when not found.
func (c *Client) FindNodeByProperty(ctx context.Context, label, key string, value any) (*Node, error) {
	result, cleanup, err := c.runQuery(ctx,
		fmt.Sprintf(
			"MATCH (n:%s {%s: $val, project_id: $pid}) RETURN toString(id(n)) AS id, properties(n) AS props LIMIT 1",
			label, key,
		),
		map[string]any{"val": value},
	)
	if err != nil {
		return nil, fmt.Errorf("find node %s.%s: %w", label, key, err)
	}
	defer cleanup()

	record, err := result.Single(ctx)
	if err == nil {
		id, _, _ := neo4j.GetRecordValue[string](record, "id")
		props, _, _ := neo4j.GetRecordValue[map[string]any](record, "props")
		return &Node{ID: id, Label: label, Properties: props}, nil
	}
	// No results is not an error in our contract.
	_, _ = result.Consume(ctx)
	return nil, nil
}

// DeleteNode removes the node with the given element ID — but ONLY if it
// belongs to this client's project. Cross-project deletion is impossible:
// a request matching a node in a different project becomes a no-op.
func (c *Client) DeleteNode(ctx context.Context, elementID string) error {
	if elementID == "" {
		return fmt.Errorf("elementID is required")
	}
	_, cleanup, err := c.runQuery(ctx,
		"MATCH (n) WHERE toString(id(n)) = $id AND n.project_id = $pid DETACH DELETE n",
		map[string]any{"id": elementID},
	)
	if err != nil {
		return fmt.Errorf("delete node %s: %w", elementID, err)
	}
	cleanup()
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
	// Step 1: delete all Symbol nodes produced from this file.
	_, cleanup1, err := c.runQuery(ctx,
		"MATCH (n:Symbol {source_file: $path, project_id: $pid}) DETACH DELETE n",
		map[string]any{"path": filePath},
	)
	if err != nil {
		return fmt.Errorf("invalidate symbols for %s: %w", filePath, err)
	}
	cleanup1()

	// Step 2: delete the File node itself.
	_, cleanup2, err := c.runQuery(ctx,
		"MATCH (f:File {path: $path, project_id: $pid}) DETACH DELETE f",
		map[string]any{"path": filePath},
	)
	if err != nil {
		return fmt.Errorf("invalidate file node for %s: %w", filePath, err)
	}
	cleanup2()
	return nil
}

// DependentsOf returns the paths of files that directly import the given file
// (1-hop dependent lookup, CHANGED_CONTRACT.md §2). Used to expand the
// invalidation set during --changed runs.
func (c *Client) DependentsOf(ctx context.Context, filePath string) ([]string, error) {
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (d:File {project_id: $pid})-[:IMPORTS]->(f:File {path: $path, project_id: $pid}) RETURN d.path AS dep ORDER BY dep",
		map[string]any{"path": filePath},
	)
	if err != nil {
		return nil, fmt.Errorf("dependents of %s: %w", filePath, err)
	}
	defer cleanup()

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

// FileSymbols returns all Symbol nodes whose source_file matches the given
// path, scoped to this project. Used by `gg impact` to show what symbols
// a changed file exports.
func (c *Client) FileSymbols(ctx context.Context, filePath string) ([]*Node, error) {
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (n:Symbol {source_file: $path, project_id: $pid}) RETURN toString(id(n)) AS id, properties(n) AS props",
		map[string]any{"path": filePath},
	)
	if err != nil {
		return nil, fmt.Errorf("file symbols %s: %w", filePath, err)
	}
	defer cleanup()

	var nodes []*Node
	for result.Next(ctx) {
		record := result.Record()
		id, _, _ := neo4j.GetRecordValue[string](record, "id")
		props, _, _ := neo4j.GetRecordValue[map[string]any](record, "props")
		nodes = append(nodes, &Node{ID: id, Label: LabelSymbol, Properties: props})
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("file symbols %s iterate: %w", filePath, err)
	}
	return nodes, nil
}

// CountFileNodes returns the number of File nodes indexed for this project.
// Returns 0 when the graph is empty (not yet indexed).
func (c *Client) CountFileNodes(ctx context.Context) (int64, error) {
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (f:File {project_id: $pid}) RETURN count(f) AS n",
		nil,
	)
	if err != nil {
		return 0, fmt.Errorf("count file nodes: %w", err)
	}
	defer cleanup()

	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("count file nodes single: %w", err)
	}
	n, _, err := neo4j.GetRecordValue[int64](record, "n")
	if err != nil {
		return 0, fmt.Errorf("count file nodes get value: %w", err)
	}
	return n, nil
}

// SweepProject removes ALL nodes (and their edges) belonging to this project.
// Call this before a full re-index to ensure that nodes from a previous branch
// or rebase don't survive as ghost symbols in the graph.
//
// The operation is idempotent and safe to call on an empty project.
func (c *Client) SweepProject(ctx context.Context) error {
	_, cleanup, err := c.runQuery(ctx,
		"MATCH (n {project_id: $pid}) DETACH DELETE n",
		nil,
	)
	if err != nil {
		return fmt.Errorf("sweep project: %w", err)
	}
	cleanup()
	return nil
}

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
