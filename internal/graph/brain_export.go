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

// ExportNodes returns all nodes belonging to this project, sorted by ID.
// Each node's label is the first label assigned (Memgraph nodes may have multiple labels).
func (c *Client) ExportNodes(ctx context.Context) ([]BrainNode, error) {
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (n {project_id: $pid}) RETURN toString(id(n)) AS id, labels(n) AS lbls, properties(n) AS props",
		map[string]any{"pid": c.projectID},
	)
	if err != nil {
		return nil, fmt.Errorf("brain export nodes: %w", err)
	}
	defer cleanup()

	var nodes []BrainNode
	for result.Next(ctx) {
		record := result.Record()
		id, _, _ := neo4j.GetRecordValue[string](record, "id")
		props, _, _ := neo4j.GetRecordValue[map[string]any](record, "props")
		lbls, _, _ := neo4j.GetRecordValue[[]any](record, "lbls")
		if id == "" {
			continue
		}
		label := ""
		for _, l := range lbls {
			if s, ok := l.(string); ok && s != "" {
				label = s
				break
			}
		}
		nodes = append(nodes, BrainNode{
			ID:         id,
			Label:      label,
			Properties: cleanProps(props),
		})
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("brain export nodes iterate: %w", err)
	}

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, nil
}

// ExportEdges returns all relationships where both endpoints belong to this
// project, sorted by (src, dst, type).
func (c *Client) ExportEdges(ctx context.Context) ([]BrainEdge, error) {
	result, cleanup, err := c.runQuery(ctx,
		`MATCH (src {project_id: $pid})-[r]->(dst {project_id: $pid})
		 RETURN toString(id(src)) AS src, toString(id(dst)) AS dst,
		        type(r) AS rel_type, properties(r) AS props`,
		map[string]any{"pid": c.projectID},
	)
	if err != nil {
		return nil, fmt.Errorf("brain export edges: %w", err)
	}
	defer cleanup()

	var edges []BrainEdge
	for result.Next(ctx) {
		record := result.Record()
		src, _, _ := neo4j.GetRecordValue[string](record, "src")
		dst, _, _ := neo4j.GetRecordValue[string](record, "dst")
		relType, _, _ := neo4j.GetRecordValue[string](record, "rel_type")
		props, _, _ := neo4j.GetRecordValue[map[string]any](record, "props")
		if src == "" || dst == "" {
			continue
		}
		edges = append(edges, BrainEdge{
			Src:        src,
			Dst:        dst,
			Type:       relType,
			Properties: cleanProps(props),
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
