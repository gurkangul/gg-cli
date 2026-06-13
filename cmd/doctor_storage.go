package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
)

// doctorCheckVectorStore is the backend-aware vector-store check. For the
// embedded SQLite default it verifies the DB is openable and the collections
// exist (hinting `gg reembed` when they are missing/empty) — it never probes
// localhost:6334. Only an explicit qdrant server backend dials the server.
func doctorCheckVectorStore(cmd *cobra.Command, cfg *config.Config, report *doctorReport) {
	if cfg.Qdrant.UsesEmbedded() {
		doctorCheckEmbeddedVector(cmd, cfg, report)
		return
	}
	doctorCheckQdrantServer(cmd, cfg, report)
}

// doctorCheckGraphStore is the backend-aware graph-store check. For the embedded
// SQLite default it verifies graph.db is openable; only an explicit memgraph
// server backend dials the Bolt endpoint.
func doctorCheckGraphStore(cmd *cobra.Command, cfg *config.Config, report *doctorReport) {
	if cfg.Memgraph.UsesEmbedded() {
		doctorCheckEmbeddedGraph(cmd, cfg, report)
		return
	}
	doctorCheckMemgraphServer(cmd, cfg, report)
}

// doctorCheckEmbeddedVector verifies the embedded SQLite vector store. The store
// is always "up" (a local file), so the meaningful signals are: DB openable,
// collections materialized, and whether any vectors have been embedded yet. An
// empty store on a fresh cutover is a HINT (run `gg reembed`), not a failure.
func doctorCheckEmbeddedVector(cmd *cobra.Command, cfg *config.Config, report *doctorReport) {
	ggDir, _ := config.GGDir()
	c, err := doctorQdrantNewClient(cfg, ggDir)
	if err != nil {
		report.fail("vector store", fmt.Sprintf("embedded sqlite init: %v", err))
		return
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := c.HealthCheck(ctx); err != nil {
		report.fail("vector store", fmt.Sprintf("embedded sqlite (.gg/vectorstore.db) not openable: %v", err))
		return
	}
	report.ok("vector store", "embedded sqlite (.gg/vectorstore.db)")

	present, missing, err := c.CollectionStatus(ctx)
	if err != nil {
		report.fail("vector collections", err.Error())
		return
	}
	switch {
	case len(present) == 0:
		// Fresh cutover or never-populated: guide the one-time migration.
		report.warn("vector collections", "empty — run `gg reembed` to build .gg/vectorstore.db from .gg/brain/*.jsonl")
		return
	case len(missing) > 0:
		report.warn("vector collections", fmt.Sprintf("%d of 7 missing: %s — run `gg reembed` to (re)build", len(missing), strings.Join(missing, ", ")))
	default:
		report.ok("vector collections", "all 7 collections present")
	}
	doctorReportVectorCounts(ctx, c, report)
}

// doctorReportVectorCounts reports per-collection payload counts and flags an
// all-empty store with a reembed hint. Shared by the embedded check; the count
// methods are backend-agnostic (implemented on *store.Client).
func doctorReportVectorCounts(ctx context.Context, c qdrantHealthChecker, report *doctorReport) {
	counter, ok := c.(interface {
		CollectionPayloadCounts(context.Context) (map[string]uint64, error)
	})
	if !ok {
		return
	}
	counts, err := counter.CollectionPayloadCounts(ctx)
	if err != nil {
		report.warn("vector payload counts", fmt.Sprintf("could not count payloads: %v", err))
		return
	}
	var parts []string
	var total uint64
	for _, kind := range brainKinds {
		if n, exists := counts[kind]; exists {
			parts = append(parts, fmt.Sprintf("%s=%d", kind, n))
			total += n
		}
	}
	if total == 0 {
		report.warn("vector payload counts", "0 vectors — run `gg reembed` to populate from .gg/brain/*.jsonl")
		return
	}
	report.ok("vector payload counts", strings.Join(parts, ", "))
}

// doctorCheckEmbeddedGraph verifies the embedded SQLite graph store. graph.db is
// always openable; an empty graph is a HINT (run `gg index`), not a failure. The
// detailed freshness/staleness vs HEAD is reported separately by the code-graph
// freshness check, so here we only confirm openability + emptiness.
func doctorCheckEmbeddedGraph(cmd *cobra.Command, cfg *config.Config, report *doctorReport) {
	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		report.fail("graph store", fmt.Sprintf("embedded sqlite init: %v", err))
		return
	}
	defer func() { _ = gc.Close(cmd.Context()) }()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := gc.HealthCheck(ctx); err != nil {
		report.fail("graph store", fmt.Sprintf("embedded sqlite (.gg/graph.db) not openable: %v", err))
		return
	}
	report.ok("graph store", "embedded sqlite (.gg/graph.db)")

	stats, err := gc.Stats(ctx)
	if err != nil {
		report.warn("graph contents", fmt.Sprintf("could not read graph stats: %v", err))
		return
	}
	if stats.Files == 0 && stats.Symbols == 0 && stats.Edges == 0 {
		report.warn("graph contents", "empty — run `gg index --lang <go|typescript|python|swift>` to build the code graph")
		return
	}
	report.ok("graph contents", fmt.Sprintf("files=%d symbols=%d edges=%d", stats.Files, stats.Symbols, stats.Edges))
}
