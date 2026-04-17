package graph

import (
	"context"
	"fmt"
	"sort"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// BrainNode is a single exported Memgraph node.
type BrainNode struct {
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Properties map[string]any `json:"properties"`
}

// BrainEdge is a single exported Memgraph relationship.
type BrainEdge struct {
	Dst        string         `json:"dst"`
	Properties map[string]any `json:"properties"`
	Src        string         `json:"src"`
	Type       string         `json:"type"`
}

// stableDomainKey returns a byte-deterministic, machine-independent node ID
// derived from the node's domain properties. The key encodes both label and
// the merge-key values so it is meaningful on any machine that imports the graph.
//
// Format (mirrors mergeKeysForLabel):
//   - File    → "file:<path>"
//   - Symbol  → "symbol:<source_file>#<name>"
//   - Package → "package:<import_path>"
//   - unknown → "node:<label>:<name>"  (empty string if name is also missing)
func stableDomainKey(label string, props map[string]any) string {
	switch label {
	case LabelFile:
		if path, ok := props["path"].(string); ok && path != "" {
			return "file:" + path
		}
		return "" // path required
	case LabelSymbol:
		srcFile, _ := props["source_file"].(string)
		name, _ := props["name"].(string)
		if srcFile != "" && name != "" {
			return "symbol:" + srcFile + "#" + name
		}
		return "" // both source_file and name required
	case LabelPackage:
		if importPath, ok := props["import_path"].(string); ok && importPath != "" {
			return "package:" + importPath
		}
		return "" // import_path required
	default:
		// Unknown label: fallback to "node:<label>:<name>"; skip if name is missing.
		if name, ok := props["name"].(string); ok && name != "" {
			return "node:" + label + ":" + name
		}
		return "" // no usable key — caller must skip this node
	}
}

// firstLabel returns the first non-empty label from a Memgraph labels(n) result.
func firstLabel(lbls []any) string {
	for _, l := range lbls {
		if s, ok := l.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// ExportNodes returns all nodes belonging to this project, sorted by stable domain key ID.
// Each node's ID is a domain-key composite (e.g. "file:internal/graph/client.go") that is
// byte-identical across Memgraph restarts and machines — replacing the old internal element ID.
func (c *Client) ExportNodes(ctx context.Context) ([]BrainNode, error) {
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (n {project_id: $pid}) RETURN labels(n) AS lbls, properties(n) AS props",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("brain export nodes: %w", err)
	}
	defer cleanup()

	var nodes []BrainNode
	for result.Next(ctx) {
		record := result.Record()
		props, _, _ := neo4j.GetRecordValue[map[string]any](record, "props")
		lbls, _, _ := neo4j.GetRecordValue[[]any](record, "lbls")
		label := firstLabel(lbls)
		cleaned := cleanProps(props)
		id := stableDomainKey(label, cleaned)
		if id == "" {
			continue // skip nodes with no usable domain key
		}
		nodes = append(nodes, BrainNode{
			ID:         id,
			Label:      label,
			Properties: cleaned,
		})
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("brain export nodes iterate: %w", err)
	}

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, nil
}

// ExportEdges returns all relationships where both endpoints belong to this
// project, sorted by (src, dst, type). src/dst are stable domain keys, not
// internal Memgraph element IDs.
func (c *Client) ExportEdges(ctx context.Context) ([]BrainEdge, error) {
	result, cleanup, err := c.runQuery(ctx,
		`MATCH (src {project_id: $pid})-[r]->(dst {project_id: $pid})
		 RETURN labels(src) AS src_lbls, properties(src) AS src_props,
		        labels(dst) AS dst_lbls, properties(dst) AS dst_props,
		        type(r) AS rel_type, properties(r) AS rel_props`,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("brain export edges: %w", err)
	}
	defer cleanup()

	var edges []BrainEdge
	for result.Next(ctx) {
		record := result.Record()
		srcLbls, _, _ := neo4j.GetRecordValue[[]any](record, "src_lbls")
		srcProps, _, _ := neo4j.GetRecordValue[map[string]any](record, "src_props")
		dstLbls, _, _ := neo4j.GetRecordValue[[]any](record, "dst_lbls")
		dstProps, _, _ := neo4j.GetRecordValue[map[string]any](record, "dst_props")
		relType, _, _ := neo4j.GetRecordValue[string](record, "rel_type")
		relProps, _, _ := neo4j.GetRecordValue[map[string]any](record, "rel_props")

		srcKey := stableDomainKey(firstLabel(srcLbls), cleanProps(srcProps))
		dstKey := stableDomainKey(firstLabel(dstLbls), cleanProps(dstProps))
		if srcKey == "" || dstKey == "" {
			continue
		}
		edges = append(edges, BrainEdge{
			Src:        srcKey,
			Dst:        dstKey,
			Type:       relType,
			Properties: cleanProps(relProps),
		})
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("brain export edges iterate: %w", err)
	}

	// Stable sort: src → dst → type.
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Src != edges[j].Src {
			return edges[i].Src < edges[j].Src
		}
		if edges[i].Dst != edges[j].Dst {
			return edges[i].Dst < edges[j].Dst
		}
		return edges[i].Type < edges[j].Type
	})
	return edges, nil
}

// cleanProps converts a neo4j property map to a plain Go map[string]any,
// normalising integer types that the driver returns as int64.
func cleanProps(props map[string]any) map[string]any {
	if props == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(props))
	for k, v := range props {
		out[k] = v
	}
	return out
}
