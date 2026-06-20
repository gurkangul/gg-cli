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
	Short: "Rebuild the embedded vector index (.gg/vectorstore.db) from .gg/brain/*.jsonl",
	Long: `Drops and recreates the local vector index (.gg/vectorstore.db) at the
dimension of the currently configured embedding model, then re-embeds every
record from the durable brain (.gg/brain/*.jsonl).

Use this when you change the embedding model in .gg/config.yaml. Without
re-embedding, vectors from different models will be mixed in the same
collections, which breaks semantic search recall.

This rebuilds the local vector index from .gg/brain/*.jsonl (the durable source
of truth, which is never modified). Safe to re-run.

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
		fmt.Println("  1. Drop the local vector index collections (.gg/vectorstore.db)")
		fmt.Println("  2. Recreate them with the new embedding model's vector size")
		fmt.Println("  3. Re-embed every record from .gg/brain/*.jsonl using the new model")
		fmt.Println()
		fmt.Println("This rebuilds the local vector index from .gg/brain/*.jsonl (the durable")
		fmt.Println("source of truth, which is never modified). Safe to re-run.")
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
	// Use the backend-qualified effective identity (bare model for Ollama,
	// "voyage:<model>" for Voyage) so the console shows the real model — not the
	// ignored leftover cfg.Embedding.Model under a Voyage config.
	effectiveModel := embedding.EffectiveModelIdentity(&cfg.Embedding)
	fmt.Printf("Detecting vector size for model %q ...\n", effectiveModel)
	gen := embedding.New(&cfg.Embedding, 0) // 0 = no dim validation
	probeCtx, probeCancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer probeCancel()
	probeVec, err := gen.Generate(probeCtx, "dimension probe")
	if err != nil {
		return fmt.Errorf("probe embedding dim: %w", err)
	}
	newDim := len(probeVec)
	fmt.Printf("Model %q returns %d-dim vectors.\n\n", effectiveModel, newDim)

	// Open the embedded vector store.
	sc, err := store.New(ggDir, cfg.ProjectID)
	if err != nil {
		return serviceErr(fmt.Sprintf("vector store: %v", err))
	}
	defer func() { _ = sc.Close() }()

	hctx, hcancel := context.WithTimeout(cmd.Context(), healthCheckTimeout)
	defer hcancel()
	if hErr := sc.HealthCheck(hctx); hErr != nil {
		return fmt.Errorf("vector store health check failed: %w", hErr)
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

	// Update the embedding meta to record the new model. Use the backend-qualified
	// identity (bare model for Ollama, "voyage:<model>" for Voyage) so a later
	// backend switch is detected by CheckMeta as a mismatch.
	if writeErr := embedding.WriteMeta(ggDir, &embedding.Meta{
		ModelName: effectiveModel,
		Dim:       newDim,
	}); writeErr != nil {
		fmt.Printf("warning: could not update embedding-meta.json: %v\n", writeErr)
	}

	// Print summary.
	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println("Migration complete:")
	total := 0
	failed := 0
	for _, r := range results {
		if r.Migrated > 0 || r.Skipped > 0 || r.Failed > 0 {
			line := fmt.Sprintf("  %-40s  migrated=%d  skipped=%d", r.Collection, r.Migrated, r.Skipped)
			if r.Failed > 0 {
				line += fmt.Sprintf("  failed=%d", r.Failed)
			}
			fmt.Println(line)
		}
		total += r.Migrated
		failed += r.Failed
	}
	fmt.Printf("\nTotal re-embedded: %d point(s). Model: %s (%d-dim)\n", total, effectiveModel, newDim)
	if failed > 0 {
		return fmt.Errorf("%d point(s) could not be embedded — still in JSONL; re-run `gg reembed` to retry", failed)
	}
	return nil
}
