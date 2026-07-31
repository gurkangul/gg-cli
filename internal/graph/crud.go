package graph

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	id, _, err := recordValue[string](record, "id")
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
	id, _, err := recordValue[string](record, "id")
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
		id, _, _ := recordValue[string](record, "id")
		props, _, _ := recordValue[map[string]any](record, "props")
		return &Node{ID: id, Label: label, Properties: props}, nil
	}
	// No results is not an error in our contract.
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
		dep, _, _ := recordValue[string](result.Record(), "dep")
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
		id, _, _ := recordValue[string](record, "id")
		props, _, _ := recordValue[map[string]any](record, "props")
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
	n, _, err := recordValue[int64](record, "n")
	if err != nil {
		return 0, fmt.Errorf("count file nodes get value: %w", err)
	}
	return n, nil
}

// FileNodeExists reports whether a File node for the given project-relative path
// exists in the graph. BUG-105: `gg impact` needs to distinguish a file that has
// zero dependents because it is a genuine leaf from a file that has zero
// dependents because it is ABSENT from the graph (build-tagged and excluded from
// the host-platform index, skipped by the indexer, or a path mismatch). Both
// look identical in DependentsOf/FileSymbols output, and the git-sha freshness
// contract reports "fresh" either way, so without this check an authoritative-
// looking empty blast radius can be a false negative.
func (c *Client) FileNodeExists(ctx context.Context, filePath string) (bool, error) {
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (f:File {path: $path, project_id: $pid}) RETURN count(f) AS n",
		map[string]any{"path": filePath},
	)
	if err != nil {
		return false, fmt.Errorf("file node exists %s: %w", filePath, err)
	}
	defer cleanup()

	record, err := result.Single(ctx)
	if err != nil {
		return false, fmt.Errorf("file node exists %s single: %w", filePath, err)
	}
	n, _, err := recordValue[int64](record, "n")
	if err != nil {
		return false, fmt.Errorf("file node exists %s get value: %w", filePath, err)
	}
	return n > 0, nil
}

// SweepProject removes ALL nodes (and their edges) belonging to this project.
// Call this only when intentionally rebuilding the entire project graph.
// Language-specific indexing should use SweepProjectLang so Go/Python/TS graph
// slices can coexist in multi-language projects.
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

// SweepProjectLang removes all nodes for a single indexed language in this
// project. Code graph nodes (File/Symbol/Package) all carry a lang property;
// deleting by lang prevents a TypeScript full-index from wiping a Python graph
// slice, while still removing ghost nodes for the language being rebuilt.
func (c *Client) SweepProjectLang(ctx context.Context, lang string) error {
	if lang == "" {
		return fmt.Errorf("lang is required")
	}
	_, cleanup, err := c.runQuery(ctx,
		"MATCH (n {project_id: $pid, lang: $lang}) DETACH DELETE n",
		map[string]any{"lang": lang},
	)
	if err != nil {
		return fmt.Errorf("sweep project lang %s: %w", lang, err)
	}
	cleanup()
	return nil
}
