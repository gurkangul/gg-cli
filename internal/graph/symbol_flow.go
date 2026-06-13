package graph

import (
	"context"
	"fmt"
	"sort"
)

type SymbolMatch struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	SourceFile string         `json:"source_file"`
	Kind       string         `json:"kind,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

type CallFlowNode struct {
	From  SymbolMatch `json:"from"`
	To    SymbolMatch `json:"to"`
	Depth int         `json:"depth"`
	Cycle bool        `json:"cycle,omitempty"`
}

type CallFlow struct {
	Root           SymbolMatch    `json:"root"`
	Edges          []CallFlowNode `json:"edges"`
	RequestedDepth int            `json:"requested_depth"`
	MaxDepth       int            `json:"max_depth"`
	Truncated      bool           `json:"truncated"`
	Cycles         []string       `json:"cycles,omitempty"`
}

func (c *Client) FindSymbols(ctx context.Context, name string) ([]SymbolMatch, error) {
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (s:Symbol {name: $name, project_id: $pid}) RETURN toString(id(s)) AS id, properties(s) AS props ORDER BY s.source_file, s.name",
		map[string]any{"name": name},
	)
	if err != nil {
		return nil, fmt.Errorf("find symbols %s: %w", name, err)
	}
	defer cleanup()

	var matches []SymbolMatch
	for result.Next(ctx) {
		match := symbolMatchFromRecord(result.Record())
		if match.Name != "" {
			matches = append(matches, match)
		}
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("find symbols %s iterate: %w", name, err)
	}
	return matches, nil
}

func (c *Client) ForwardCallFlow(ctx context.Context, root SymbolMatch, maxDepth int) (CallFlow, error) {
	if maxDepth < 1 {
		return CallFlow{}, fmt.Errorf("call flow %s: maxDepth must be >= 1", root.Name)
	}
	flow := CallFlow{Root: root, RequestedDepth: maxDepth, MaxDepth: maxDepth}
	seen := map[string]int{root.ID: 0}
	frontier := []SymbolMatch{root}
	cycleSeen := map[string]bool{}

	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		var next []SymbolMatch
		for _, from := range frontier {
			callees, err := c.callees(ctx, from.ID)
			if err != nil {
				return flow, err
			}
			for _, to := range callees {
				edge := CallFlowNode{From: from, To: to, Depth: depth}
				if priorDepth, ok := seen[to.ID]; ok {
					if priorDepth < depth {
						edge.Cycle = true
						if !cycleSeen[to.ID] {
							flow.Cycles = append(flow.Cycles, to.Name)
							cycleSeen[to.ID] = true
						}
					}
					flow.Edges = append(flow.Edges, edge)
					continue
				}
				seen[to.ID] = depth
				flow.Edges = append(flow.Edges, edge)
				next = append(next, to)
			}
		}
		frontier = next
	}
	if len(frontier) > 0 {
		flow.Truncated = true
	}
	sort.Strings(flow.Cycles)
	return flow, nil
}

func (c *Client) callees(ctx context.Context, symbolID string) ([]SymbolMatch, error) {
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (s:Symbol {project_id: $pid})-[:CALLS]->(to:Symbol {project_id: $pid}) "+
			"WHERE toString(id(s)) = $id RETURN toString(id(to)) AS id, properties(to) AS props ORDER BY to.source_file, to.name",
		map[string]any{"id": symbolID},
	)
	if err != nil {
		return nil, fmt.Errorf("symbol callees %s: %w", symbolID, err)
	}
	defer cleanup()
	var out []SymbolMatch
	for result.Next(ctx) {
		match := symbolMatchFromRecord(result.Record())
		if match.Name != "" {
			out = append(out, match)
		}
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("symbol callees %s iterate: %w", symbolID, err)
	}
	return out, nil
}

func symbolMatchFromRecord(record GraphRecord) SymbolMatch {
	id, _, _ := recordValue[string](record, "id")
	props, _, _ := recordValue[map[string]any](record, "props")
	name, _ := props["name"].(string)
	sourceFile, _ := props["source_file"].(string)
	kind, _ := props["kind"].(string)
	return SymbolMatch{ID: id, Name: name, SourceFile: sourceFile, Kind: kind, Properties: props}
}
