package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/store"
)

var reembedCmd = &cobra.Command{
	Use:   "reembed",
	Short: "Migrate all Qdrant collections to the currently configured embedding model",
	Long: `Drops and recreates all project Qdrant collections at the dimension of
the currently configured embedding model, then re-embeds every stored point.

Use this when you change the embedding model in .gg/config.yaml. Without
re-embedding, vectors from different models will be mixed in the same
collections, which breaks semantic search recall.

WARNING: This operation drops all collections before recreating them.
If the process is interrupted after the drop, stored knowledge (decisions,
tasks, notes, etc.) will be lost. Back up your Qdrant data if it matters.

Requires --confirm to proceed.`,
	RunE: runReembed,
}

var (
	reembedConfirm bool
	reembedYes     bool
)

func init() {
	reembedCmd.Flags().BoolVar(&reembedConfirm, "confirm", false,
		"required: confirms you understand existing vector data will be dropped and rebuilt")
	// --yes is the canonical destructive-confirm flag across gg; --confirm is kept
	// as a backward-compatible alias so existing scripts keep working.
	reembedCmd.Flags().BoolVar(&reembedYes, "yes", false,
		"alias for --confirm (canonical destructive-confirm flag)")
	rootCmd.AddCommand(reembedCmd)
}

func runReembed(cmd *cobra.Command, _ []string) error {
	if !reembedConfirm && !reembedYes {
		fmt.Println("gg reembed — Embedding model migration")
		fmt.Println(strings.Repeat("─", 50))
		fmt.Println()
		fmt.Println("This command will:")
		fmt.Println("  1. Read all stored points (decisions, tasks, notes, etc.) from Qdrant")
		fmt.Println("  2. Drop all project collections")
		fmt.Println("  3. Recreate them with the new embedding model's vector size")
		fmt.Println("  4. Re-embed every point using the new model")
		fmt.Println()
		fmt.Println("WARNING: If interrupted between steps 2 and 4, stored knowledge will be lost.")
		fmt.Println("         Back up your Qdrant data before proceeding.")
		fmt.Println()
		fmt.Println("To proceed: gg reembed --yes  (or --confirm)")
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ggDir, err := config.GGDir()
	if err != nil {
		return err
	}

	// Determine the new vector size by calling the embedding model once.
	fmt.Printf("Detecting vector size for model %q ...\n", cfg.Embedding.Model)
	gen := embedding.New(&cfg.Embedding, 0) // 0 = no dim validation
	probeCtx, probeCancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer probeCancel()
	probeVec, err := gen.Generate(probeCtx, "dimension probe")
	if err != nil {
		return fmt.Errorf("probe embedding dim: %w", err)
	}
	newDim := len(probeVec)
	fmt.Printf("Model %q returns %d-dim vectors.\n\n", cfg.Embedding.Model, newDim)

	// Connect to Qdrant.
	sc, err := store.New(&cfg.Qdrant, ggDir, cfg.ProjectID)
	if err != nil {
		return serviceErr(fmt.Sprintf("qdrant client: %v", err))
	}
	defer func() { _ = sc.Close() }()

	hctx, hcancel := context.WithTimeout(cmd.Context(), healthCheckTimeout)
	defer hcancel()
	if hErr := sc.HealthCheck(hctx); hErr != nil {
		return fmt.Errorf("qdrant health check failed (is Qdrant running?): %w", hErr)
	}

	// Use the full configured dim as expectedDim for the production embedder.
	prodGen := embedding.New(&cfg.Embedding, newDim)

	fmt.Println("Starting migration ...")
	migrateCtx, migrateCancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
	defer migrateCancel()

	results, err := sc.ReembedAll(migrateCtx, prodGen, uint64(newDim))
	if err != nil {
		return fmt.Errorf("reembed: %w", err)
	}

	// Update the embedding meta to record the new model.
	if writeErr := embedding.WriteMeta(ggDir, &embedding.Meta{
		ModelName: cfg.Embedding.Model,
		Dim:       newDim,
	}); writeErr != nil {
		fmt.Printf("warning: could not update embedding-meta.json: %v\n", writeErr)
	}

	// Print summary.
	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println("Migration complete:")
	total := 0
	for _, r := range results {
		if r.Migrated > 0 || r.Skipped > 0 {
			fmt.Printf("  %-40s  migrated=%d  skipped=%d\n", r.Collection, r.Migrated, r.Skipped)
		}
		total += r.Migrated
	}
	fmt.Printf("\nTotal re-embedded: %d point(s). Model: %s (%d-dim)\n", total, cfg.Embedding.Model, newDim)
	return nil
}
