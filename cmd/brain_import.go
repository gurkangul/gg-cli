package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

// checkManifestProjectID returns an error when the snapshot project_id does not
// match the current project's ID, unless force is true. An empty manifestPID
// (old snapshot without the field) is always accepted.
func checkManifestProjectID(manifestPID, currentPID string, force bool) error {
	if manifestPID == "" || manifestPID == currentPID || force {
		return nil
	}
	return fmt.Errorf(
		"brain import: project_id mismatch\n  snapshot: %s\n  current:  %s\n"+
			"  Use --force-project-mismatch to import across projects",
		manifestPID, currentPID,
	)
}

func runBrainImport(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ggDir, err := config.GGDir()
	if err != nil {
		return err
	}

	if !jsonOutput {
		fmt.Fprintf(os.Stderr, "Project: %s\n", cfg.ProjectID)
	}

	brainDir := filepath.Join(ggDir, brainDirName)

	// 1. Read and validate manifest.
	manifest, err := readAndValidateManifest(brainDir, cfg)
	if err != nil {
		return err
	}

	// 2. Embedding model check.
	if !brainImportSkipEmbedCheck && manifest.EmbeddingModel != "" {
		embMeta, readErr := embedding.ReadMeta(ggDir)
		if readErr == nil && embMeta != nil && embMeta.ModelName != manifest.EmbeddingModel {
			return fmt.Errorf(
				"brain import: embedding model mismatch\n  export used: %s (dim %d)\n  this machine: %s (dim %d)\n"+
					"  Run: gg brain import --skip-embed-check   # import payload only, vectors rebuilt on reindex\n"+
					"    or: configure the same model in .gg/config.yaml, then retry",
				manifest.EmbeddingModel, manifest.EmbeddingDim,
				embMeta.ModelName, embMeta.Dim,
			)
		}
	}

	// 3. Dry run — just report.
	if brainImportDryRun {
		fmt.Println("Dry run — would import:")
		kindOrder := append(append([]string(nil), store.BrainKind...), "chunks", "edges", "task-events")
		for _, k := range kindOrder {
			if n := manifest.Counts[k]; n > 0 {
				fmt.Printf("  %-20s %d records\n", k+".jsonl", n)
			}
		}
		return nil
	}

	// 4. --wipe confirmation.
	if brainImportWipe && !brainImportYes {
		fmt.Fprintf(os.Stderr,
			"⚠ --wipe will delete all existing project data for project %s.\n"+
				"  Re-run with --yes to confirm.\n", cfg.ProjectID)
		return fmt.Errorf("--wipe requires --yes confirmation")
	}

	// Brain import can process thousands of graph nodes — use a generous timeout.
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
	defer cancel()

	// 5. Qdrant import.
	totalQdrant, err := importQdrant(ctx, cfg, ggDir, brainDir)
	if err != nil {
		return err
	}

	// 6. Memgraph import (optional — skip when graph files are absent/empty).
	totalChunks, totalEdges, skippedChunks, skippedEdges, err := importMemgraph(ctx, cfg, brainDir, manifest)
	if err != nil {
		return err
	}

	// 7. Auto re-embed missing vectors (TASK-136).
	totalEmbedded, err := reembedMissing(cmd, cfg, ggDir, manifest)
	if err != nil {
		return err
	}

	// 8. Report.
	fmt.Printf("✓ Brain imported from %s\n", brainDir)
	fmt.Printf("  Vector store: %d records\n", totalQdrant)
	if totalChunks+skippedChunks > 0 {
		fmt.Printf("  Nodes:   %d imported, %d skipped\n", totalChunks, skippedChunks)
		fmt.Printf("  Edges:   %d imported, %d skipped\n", totalEdges, skippedEdges)
	}
	if n := manifest.Counts["task-events"]; n > 0 {
		fmt.Printf("  Events:  %d task lifecycle events preserved\n", n)
	}
	if totalEmbedded > 0 {
		fmt.Printf("  Vectors: %d embedded\n", totalEmbedded)
	}
	return nil
}

// readAndValidateManifest reads, parses, and validates the brain manifest.
func readAndValidateManifest(brainDir string, cfg *config.Config) (brainManifest, error) {
	manifestPath := filepath.Join(brainDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return brainManifest{}, fmt.Errorf("no brain snapshot found at %s (run: gg brain export)", brainDir)
		}
		return brainManifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest brainManifest
	if err := brainUnmarshalManifest(manifestData, &manifest); err != nil {
		return brainManifest{}, err
	}

	if err := checkManifestProjectID(manifest.ProjectID, cfg.ProjectID, brainImportForceProjectMismatch); err != nil {
		return brainManifest{}, err
	}

	if manifest.SchemaVersion != 1 {
		return brainManifest{}, fmt.Errorf(
			"brain import: unsupported schema version %d (this binary supports up to v1) — update gg to the latest version",
			manifest.SchemaVersion,
		)
	}

	// SHA-256 checksum verification.
	for fname, expected := range manifest.SHA256 {
		actual, checksumErr := fileChecksum(filepath.Join(brainDir, fname))
		if checksumErr != nil {
			return brainManifest{}, fmt.Errorf("brain import: checksum error for %s: %w", fname, checksumErr)
		}
		if actual != expected {
			return brainManifest{}, fmt.Errorf(
				"brain import: checksum mismatch for %s\n  expected: %s\n  actual:   %s\nRe-run 'gg brain export' to regenerate a clean snapshot",
				fname, expected, actual,
			)
		}
	}
	return manifest, nil
}

// importQdrant upserts Qdrant records from the brain snapshot.
func importQdrant(ctx context.Context, cfg *config.Config, ggDir, brainDir string) (int, error) {
	storeClient, err := store.New(ggDir, cfg.ProjectID)
	if err != nil {
		return 0, fmt.Errorf("store init: %w", err)
	}
	defer func() { _ = storeClient.Close() }()

	hctx, hcancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer hcancel()
	if hErr := storeClient.HealthCheck(hctx); hErr != nil {
		return 0, storeDownErr()
	}

	if err := storeClient.EnsureCollections(ctx, uint64(store.VectorSize)); err != nil {
		return 0, fmt.Errorf("ensure collections: %w", err)
	}

	if brainImportWipe {
		if dropErr := storeClient.DropAllCollections(ctx); dropErr != nil {
			return 0, fmt.Errorf("drop collections: %w", dropErr)
		}
		if recreateErr := storeClient.EnsureCollections(ctx, uint64(store.VectorSize)); recreateErr != nil {
			return 0, fmt.Errorf("recreate collections: %w", recreateErr)
		}
		fmt.Fprintln(os.Stderr, "✓ vector collections wiped and recreated")
	}

	total := 0
	for _, kind := range store.BrainKind {
		fname := filepath.Join(brainDir, kind+".jsonl")
		records, readErr := store.ReadBrainJSONL(fname)
		if readErr != nil {
			return 0, fmt.Errorf("read %s.jsonl: %w", kind, readErr)
		}
		if upsertErr := storeClient.UpsertBrainRecords(ctx, kind, records); upsertErr != nil {
			return 0, fmt.Errorf("upsert %s: %w", kind, upsertErr)
		}
		total += len(records)
	}
	return total, nil
}

// importMemgraph imports graph chunks and edges from the brain snapshot.
func importMemgraph(ctx context.Context, cfg *config.Config, brainDir string, manifest brainManifest) (int, int, int, int, error) {
	chunksPath := filepath.Join(brainDir, "chunks.jsonl")
	edgesPath := filepath.Join(brainDir, "edges.jsonl")

	if manifest.Counts["chunks"] == 0 {
		return 0, 0, 0, 0, nil
	}

	gc, gcErr := graph.New(cfg.DataDir, cfg.ProjectID)
	if gcErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ code graph init failed (%v) — skipping graph import\n", gcErr)
		return 0, 0, 0, 0, nil
	}
	defer func() { _ = gc.Close(ctx) }()

	if brainImportWipe {
		if sweepErr := gc.SweepProject(ctx); sweepErr != nil {
			return 0, 0, 0, 0, fmt.Errorf("sweep graph: %w", sweepErr)
		}
		fmt.Fprintln(os.Stderr, "✓ code graph project swept")
	}

	chunkResult, chunkErr := gc.ImportChunks(ctx, chunksPath)
	if chunkErr != nil {
		return 0, 0, 0, 0, fmt.Errorf("import chunks: %w", chunkErr)
	}

	imp, skip, edgeErr := gc.ImportEdges(ctx, edgesPath)
	if edgeErr != nil {
		return 0, 0, 0, 0, fmt.Errorf("import edges: %w", edgeErr)
	}
	return chunkResult.Imported, imp, chunkResult.Skipped, skip, nil
}

// reembedMissing triggers embedding for records without vectors after import.
func reembedMissing(cmd *cobra.Command, cfg *config.Config, ggDir string, manifest brainManifest) (int, error) {
	if brainImportNoReindex || manifest.EmbeddingModel == "" {
		return 0, nil
	}

	embMeta, metaErr := embedding.ReadMeta(ggDir)
	modelMatch := metaErr == nil && embMeta != nil && embMeta.ModelName == manifest.EmbeddingModel

	if !modelMatch && !brainImportSkipEmbedCheck {
		fmt.Fprintf(os.Stderr,
			"\nVectors not restored (model mismatch: snapshot=%s, this machine=%s).\n"+
				"  Run: gg reindex --embed --force   or   gg brain import --skip-embed-check\n",
			manifest.EmbeddingModel, func() string {
				if embMeta != nil {
					return embMeta.ModelName
				}
				return "unknown"
			}(),
		)
		return 0, nil
	}

	fmt.Fprintln(os.Stderr, "Generating missing vectors…")
	embedCtx, embedCancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
	defer embedCancel()

	storeClient, err := store.New(ggDir, cfg.ProjectID)
	if err != nil {
		return 0, fmt.Errorf("store init for embed: %w", err)
	}
	defer func() { _ = storeClient.Close() }()

	dim := store.VectorSize
	if embMeta != nil {
		dim = embMeta.Dim
	}
	gen := embedding.New(&cfg.Embedding, dim)
	embedResults, embedErr := storeClient.EmbedMissing(embedCtx, gen, os.Stderr)
	if embedErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ re-embed interrupted: %v\n  Re-run 'gg brain import' to resume.\n", embedErr)
		return 0, nil
	}
	total := 0
	for _, r := range embedResults {
		total += r.Embedded
	}
	return total, nil
}
