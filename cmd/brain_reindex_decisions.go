package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
)

var brainReindexDecisionsCmd = &cobra.Command{
	Use:   "reindex-decisions",
	Short: "Replay Decision nodes into Memgraph from the Qdrant decision store",
	Long: `Rebuild Decision nodes in Memgraph from the Qdrant decision store.

Symmetric with ` + "`gg task reindex`" + ` and ` + "`gg bug reindex`" + `. Lists every
decision in Qdrant and upserts a matching Decision node in Memgraph so
historical decisions (created before TASK-228 shipped, or when Memgraph
was unreachable) participate in graph traversal and ` + "`gg impact`" + ` queries.

Only node identity (qdrant_id + text) is mirrored. Status, author,
tags, and reasons remain in Qdrant — Memgraph Decision nodes exist to
anchor DECIDES / REJECTS / IMPLEMENTS edges.`,
	RunE: runBrainReindexDecisions,
}

const brainReindexDecisionsLimit = 10000

func init() {
	brainCmd.AddCommand(brainReindexDecisionsCmd)
}

func runBrainReindexDecisions(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.Memgraph.URI == "" {
		return fmt.Errorf("memgraph.uri not configured — run gg init first")
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	decisions, err := d.store.ListDecisions(ctx, brainReindexDecisionsLimit, true)
	if err != nil {
		return fmt.Errorf("list decisions: %w", err)
	}

	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		return fmt.Errorf("memgraph: %w", err)
	}
	defer func() { _ = gc.Close(ctx) }()

	var replayed, failed int
	for _, dec := range decisions {
		gctx, gcancel := withTimeout(cmd.Context())
		upErr := gc.UpsertDecisionNode(gctx, dec.ID, dec.Text)
		gcancel()
		if upErr != nil {
			fmt.Printf("~ %s: %v\n", dec.ID, upErr)
			failed++
		} else {
			replayed++
		}
	}

	return printJSON(map[string]any{
		"replayed": replayed,
		"failed":   failed,
	}, func() {
		fmt.Printf("decision reindex complete: %d replayed, %d failed\n", replayed, failed)
	})
}
