package cmd

import (
	"fmt"
	"strings"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
	graphstore "github.com/gurkangul/gg-cli/internal/graph"
)

// graph_export_memory.go — TASK-518 memory half of the offline graph export.
//
// `gg graph export` rendered only LabelFile/LabelSymbol: every memory node was
// filtered out, so the one visual gg shipped showed the code and none of the
// reasoning about it. You could see which files import which, never which
// decisions produced them.
//
// The memory nodes here are derived from the JSONL link graph rather than read
// back from Memgraph. That is the same source `gg backlinks` and `gg related`
// use, which matters for two reasons: the graph includes prose refs (Memgraph
// only ever held the opt-in --implements/--rejects edges), and it renders for a
// project that has never been code-indexed at all.
//
// --view defaults to code, so existing behaviour and existing scripts are
// unchanged; memory is strictly opt-in.

const (
	graphViewCode   = "code"
	graphViewMemory = "memory"
	graphViewAll    = "all"
)

func validGraphExportView(v string) bool {
	switch v {
	case graphViewCode, graphViewMemory, graphViewAll:
		return true
	default:
		return false
	}
}

// memoryNodeLabel maps a brain kind to the node label the viewer colours by.
func memoryNodeLabel(kind string) string {
	switch kind {
	case "decisions":
		return "Decision"
	case "rejections":
		return "Rejection"
	case "tasks":
		return "Task"
	case "bugs":
		return "Bug"
	case "notes":
		return "Note"
	case "messages":
		return "Message"
	default:
		return "Memory"
	}
}

// memoryGraphExportID namespaces memory ids so they can never collide with the
// code graph's file:/symbol: ids in the merged --view all payload.
func memoryGraphExportID(key string) string { return "mem:" + key }

// memoryGraphExport builds the memory half of the export from the derived link
// graph. Returns empty (not an error) for a project with no brain entries.
func memoryGraphExport() ([]graphstore.BrainNode, []graphstore.BrainEdge, error) {
	ggDir := config.GGDirOrEmpty()
	if ggDir == "" {
		return nil, nil, configErr("not inside a gg project — run gg init first")
	}
	g, err := brain.LoadLinkGraph(ggDir)
	if err != nil {
		return nil, nil, fmt.Errorf("load link graph: %w", err)
	}

	graphNodes := g.Nodes()
	nodes := make([]graphstore.BrainNode, 0, len(graphNodes))
	for _, n := range graphNodes {
		summary := brainEntrySummary(n.Entry)
		nodes = append(nodes, graphstore.BrainNode{
			ID:    memoryGraphExportID(n.Key),
			Label: memoryNodeLabel(n.Kind),
			Properties: map[string]any{
				// "name" is what the viewer's label() falls back to after "path".
				"name":       compactTrim(summary, 90),
				"id":         n.ID,
				"kind":       strings.TrimSuffix(n.Kind, "s"),
				"created_at": brainEntryDate(n.Entry),
			},
		})
	}

	graphEdges := g.Edges()
	edges := make([]graphstore.BrainEdge, 0, len(graphEdges))
	for _, e := range graphEdges {
		edges = append(edges, graphstore.BrainEdge{
			Src:        memoryGraphExportID(e.Src),
			Dst:        memoryGraphExportID(e.Dst),
			Type:       strings.ToUpper(e.Via),
			Properties: map[string]any{"via": e.Via},
		})
	}
	return nodes, edges, nil
}
