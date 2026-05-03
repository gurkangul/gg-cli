package cmd

import (
	"context"
	"strings"
	"testing"

	graphstore "github.com/gurkangul/gg-cli/internal/graph"
)

type fakeGraphExporter struct {
	nodes []graphstore.BrainNode
	edges []graphstore.BrainEdge
}

func (f fakeGraphExporter) ExportNodes(context.Context) ([]graphstore.BrainNode, error) {
	return f.nodes, nil
}

func (f fakeGraphExporter) ExportEdges(context.Context) ([]graphstore.BrainEdge, error) {
	return f.edges, nil
}

func TestGraphExportDataKeepsFileGraphWhenSymbolCapExceeded(t *testing.T) {
	exporter := fakeGraphExporter{
		nodes: []graphstore.BrainNode{
			{ID: "file:a.go", Label: graphstore.LabelFile, Properties: map[string]any{"path": "a.go"}},
			{ID: "file:b.go", Label: graphstore.LabelFile, Properties: map[string]any{"path": "b.go"}},
			{ID: "symbol:a.go#A", Label: graphstore.LabelSymbol, Properties: map[string]any{"source_file": "a.go", "name": "A"}},
			{ID: "symbol:b.go#B", Label: graphstore.LabelSymbol, Properties: map[string]any{"source_file": "b.go", "name": "B"}},
		},
		edges: []graphstore.BrainEdge{
			{Src: "file:a.go", Dst: "file:b.go", Type: "IMPORTS"},
			{Src: "symbol:a.go#A", Dst: "symbol:b.go#B", Type: "CALLS"},
		},
	}

	nodes, edges, err := exportGraphData(context.Background(), exporter, 1)
	if err != nil {
		t.Fatalf("exportGraphData: %v", err)
	}
	if len(nodes.files) != 2 {
		t.Fatalf("file nodes: got %d, want 2", len(nodes.files))
	}
	if len(nodes.symbols) != 0 {
		t.Fatalf("symbol nodes: got %d, want 0 when cap exceeded", len(nodes.symbols))
	}
	if len(edges) != 1 || edges[0].Type != "IMPORTS" {
		t.Fatalf("edges after cap: got %#v, want only file edge", edges)
	}
}

func TestGraphExportDataIncludesSymbolsUnderCap(t *testing.T) {
	exporter := fakeGraphExporter{
		nodes: []graphstore.BrainNode{
			{ID: "file:a.go", Label: graphstore.LabelFile, Properties: map[string]any{"path": "a.go"}},
			{ID: "symbol:a.go#A", Label: graphstore.LabelSymbol, Properties: map[string]any{"source_file": "a.go", "name": "A"}},
		},
		edges: []graphstore.BrainEdge{
			{Src: "symbol:a.go#A", Dst: "symbol:a.go#A", Type: "CALLS"},
		},
	}

	nodes, edges, err := exportGraphData(context.Background(), exporter, 1)
	if err != nil {
		t.Fatalf("exportGraphData: %v", err)
	}
	if len(nodes.symbols) != 1 {
		t.Fatalf("symbol nodes: got %d, want 1", len(nodes.symbols))
	}
	if len(edges) != 1 || edges[0].Type != "CALLS" {
		t.Fatalf("edges under cap: got %#v, want symbol edge", edges)
	}
}

func TestRenderGraphExportHTMLIsSelfContainedAndInteractive(t *testing.T) {
	html, err := renderGraphExportHTML(graphExportPayload{
		GeneratedAt: "2026-05-03T00:00:00Z",
		Nodes: []graphstore.BrainNode{
			{ID: "file:a.go", Label: graphstore.LabelFile, Properties: map[string]any{"path": "a.go"}},
		},
		Edges:      []graphstore.BrainEdge{},
		SymbolCap:  500,
		SymbolView: false,
	})
	if err != nil {
		t.Fatalf("renderGraphExportHTML: %v", err)
	}

	for _, want := range []string{"Search nodes", "Node details", "neighbor", "JSON.parse(atob", "DATA.symbols||[]"} {
		if !strings.Contains(html, want) {
			t.Fatalf("html missing %q", want)
		}
	}
	for _, forbidden := range []string{"https://", "//cdn", "unpkg.com", "jsdelivr.net"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("html contains network asset marker %q", forbidden)
		}
	}
}
