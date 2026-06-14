package graph_test

// Memgraph brain export/import round-trip integration test.
//
// Requires a live Memgraph. Skipped unless GG_INTEGRATION_TEST=1 and
// Memgraph is reachable at bolt://localhost:7687.
//
// Usage:
//
//	GG_INTEGRATION_TEST=1 go test ./internal/graph/ -run TestMemgraphBrainRoundTrip -v -count=1 -timeout 120s
//
// Covers:
//   - seed → export → wipe → import → verify counts (full round-trip)
//   - re-export byte-equal to first export (BUG-007 canlı determinism kanıtı)

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

func TestMemgraphBrainRoundTrip(t *testing.T) {
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 to run Memgraph brain round-trip test")
	}
	t.Setenv(graph.GraphBackendEnv, "memgraph") // pin Memgraph; else the sqlite default (TASK-496) breaks New() → silent t.Skipf below (RULES.md Rule 7)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	projectID := "test-brain-rt-" + uuid.New().String()[:8]
	cfg := &config.MemgraphConfig{URI: "bolt://localhost:7687"}

	c, err := graph.New(cfg, projectID)
	if err != nil {
		t.Skipf("Memgraph not reachable: %v", err)
	}
	defer func() { _ = c.Close(ctx) }()

	hctx, hcancel := context.WithTimeout(ctx, 5*time.Second)
	defer hcancel()
	if err := c.HealthCheck(hctx); err != nil {
		t.Skipf("Memgraph health check failed: %v", err)
	}

	// Always clean up on exit so re-runs start fresh.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		_ = c.SweepProject(cctx)
	})

	// ── Seed 3 File + 5 Symbol + 4 edges ──────────────────────────────────
	filePaths := []string{"cmd/a.go", "cmd/b.go", "internal/c.go"}
	for _, p := range filePaths {
		n := &graph.Node{
			Label: graph.LabelFile,
			Properties: map[string]any{
				"path":       p,
				"project_id": projectID,
			},
		}
		if err := c.UpsertNode(ctx, n, []string{"path"}); err != nil {
			t.Fatalf("seed file %s: %v", p, err)
		}
	}

	symbols := []struct{ sourceFile, name string }{
		{"cmd/a.go", "Run"},
		{"cmd/a.go", "Helper"},
		{"cmd/b.go", "Init"},
		{"internal/c.go", "Process"},
		{"internal/c.go", "Cleanup"},
	}
	for _, s := range symbols {
		n := &graph.Node{
			Label: graph.LabelSymbol,
			Properties: map[string]any{
				"name":        s.name,
				"source_file": s.sourceFile,
				"project_id":  projectID,
			},
		}
		if err := c.UpsertNode(ctx, n, []string{"name", "source_file"}); err != nil {
			t.Fatalf("seed symbol %s/%s: %v", s.sourceFile, s.name, err)
		}
	}

	// 4 edges: each symbol → its file (DEFINED_IN), minus one to keep count==4.
	edgeSpecs := []struct {
		srcLabel string
		srcMerge map[string]any
		dstLabel string
		dstMerge map[string]any
		edgeType string
	}{
		{
			srcLabel: graph.LabelSymbol,
			srcMerge: map[string]any{"name": "Run", "source_file": "cmd/a.go"},
			dstLabel: graph.LabelFile,
			dstMerge: map[string]any{"path": "cmd/a.go"},
			edgeType: "DEFINED_IN",
		},
		{
			srcLabel: graph.LabelSymbol,
			srcMerge: map[string]any{"name": "Helper", "source_file": "cmd/a.go"},
			dstLabel: graph.LabelFile,
			dstMerge: map[string]any{"path": "cmd/a.go"},
			edgeType: "DEFINED_IN",
		},
		{
			srcLabel: graph.LabelSymbol,
			srcMerge: map[string]any{"name": "Init", "source_file": "cmd/b.go"},
			dstLabel: graph.LabelFile,
			dstMerge: map[string]any{"path": "cmd/b.go"},
			edgeType: "DEFINED_IN",
		},
		{
			srcLabel: graph.LabelSymbol,
			srcMerge: map[string]any{"name": "Process", "source_file": "internal/c.go"},
			dstLabel: graph.LabelFile,
			dstMerge: map[string]any{"path": "internal/c.go"},
			edgeType: "DEFINED_IN",
		},
	}
	for i, e := range edgeSpecs {
		if err := c.UpsertEdgeByKey(ctx, e.srcLabel, e.srcMerge, e.dstLabel, e.dstMerge, e.edgeType, nil); err != nil {
			t.Fatalf("seed edge %d: %v", i, err)
		}
	}
	t.Log("Step 1: seeded 3 files + 5 symbols + 4 edges")

	// ── Step 2: Export ────────────────────────────────────────────────────
	nodes1, err := c.ExportNodes(ctx)
	if err != nil {
		t.Fatalf("export nodes: %v", err)
	}
	edges1, err := c.ExportEdges(ctx)
	if err != nil {
		t.Fatalf("export edges: %v", err)
	}
	if len(nodes1) != 8 {
		t.Fatalf("want 8 nodes, got %d", len(nodes1))
	}
	if len(edges1) != 4 {
		t.Fatalf("want 4 edges, got %d", len(edges1))
	}

	dir1 := t.TempDir()
	sum1Nodes := writeNodesJSONL(t, filepath.Join(dir1, "chunks.jsonl"), nodes1)
	sum1Edges := writeEdgesJSONL(t, filepath.Join(dir1, "edges.jsonl"), edges1)
	t.Logf("Step 2: exported 8 nodes + 4 edges; chunks sha=%s edges sha=%s", sum1Nodes[:12], sum1Edges[:12])

	// ── Step 3: Wipe ──────────────────────────────────────────────────────
	if err := c.SweepProject(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	afterWipeNodes, _ := c.ExportNodes(ctx)
	afterWipeEdges, _ := c.ExportEdges(ctx)
	if len(afterWipeNodes) != 0 || len(afterWipeEdges) != 0 {
		t.Fatalf("wipe left nodes=%d edges=%d (want 0/0)", len(afterWipeNodes), len(afterWipeEdges))
	}
	t.Log("Step 3: Memgraph wiped for test project")

	// ── Step 4: Import ────────────────────────────────────────────────────
	if _, err := c.ImportChunks(ctx, filepath.Join(dir1, "chunks.jsonl")); err != nil {
		t.Fatalf("import chunks: %v", err)
	}
	if _, _, err := c.ImportEdges(ctx, filepath.Join(dir1, "edges.jsonl")); err != nil {
		t.Fatalf("import edges: %v", err)
	}
	t.Log("Step 4: import complete")

	// ── Step 5: Verify counts ─────────────────────────────────────────────
	nodes2, err := c.ExportNodes(ctx)
	if err != nil {
		t.Fatalf("re-export nodes: %v", err)
	}
	edges2, err := c.ExportEdges(ctx)
	if err != nil {
		t.Fatalf("re-export edges: %v", err)
	}
	if len(nodes2) != 8 {
		t.Errorf("post-import nodes = %d, want 8", len(nodes2))
	}
	if len(edges2) != 4 {
		t.Errorf("post-import edges = %d, want 4", len(edges2))
	}

	// ── Step 6: Byte-equal re-export (BUG-007 canlı determinism kanıtı) ──
	dir2 := t.TempDir()
	sum2Nodes := writeNodesJSONL(t, filepath.Join(dir2, "chunks.jsonl"), nodes2)
	sum2Edges := writeEdgesJSONL(t, filepath.Join(dir2, "edges.jsonl"), edges2)

	if sum1Nodes != sum2Nodes {
		t.Errorf("chunks.jsonl SHA changed across round-trip:\n  before: %s\n  after:  %s", sum1Nodes, sum2Nodes)
	}
	if sum1Edges != sum2Edges {
		t.Errorf("edges.jsonl SHA changed across round-trip:\n  before: %s\n  after:  %s", sum1Edges, sum2Edges)
	}
	t.Log("Step 6: round-trip byte-equality verified (BUG-007 kanıtı)")
}

func writeNodesJSONL(t *testing.T, path string, nodes []graph.BrainNode) string {
	t.Helper()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	f, err := os.Create(path) //nolint:gosec
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	w := bufio.NewWriter(f)
	for _, n := range nodes {
		obj := map[string]any{"id": n.ID, "label": n.Label, "properties": n.Properties}
		line, err := store.CanonicalJSON(obj)
		if err != nil {
			t.Fatalf("encode node %s: %v", n.ID, err)
		}
		line = append(line, '\n')
		_, _ = w.Write(line)
		h.Write(line)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeEdgesJSONL(t *testing.T, path string, edges []graph.BrainEdge) string {
	t.Helper()
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Src != edges[j].Src {
			return edges[i].Src < edges[j].Src
		}
		if edges[i].Dst != edges[j].Dst {
			return edges[i].Dst < edges[j].Dst
		}
		return edges[i].Type < edges[j].Type
	})
	f, err := os.Create(path) //nolint:gosec
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	w := bufio.NewWriter(f)
	for _, e := range edges {
		obj := map[string]any{"src": e.Src, "dst": e.Dst, "type": e.Type, "properties": e.Properties}
		line, err := store.CanonicalJSON(obj)
		if err != nil {
			t.Fatalf("encode edge %s→%s: %v", e.Src, e.Dst, err)
		}
		line = append(line, '\n')
		_, _ = w.Write(line)
		h.Write(line)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return hex.EncodeToString(h.Sum(nil))
}
