package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

const brainDirName = "brain"
const brainPartialDirName = "brain.partial"

var brainCmd = &cobra.Command{
	Use:   "brain",
	Short: "Portable brain snapshot (export / import / status)",
	Long: `Manage a git-trackable snapshot of gg's shared brain.

  gg brain export   — write .gg/brain/ from current Qdrant + Memgraph state
  gg brain import   — restore Qdrant + Memgraph from .gg/brain/
  gg brain status   — show snapshot metadata and checksum status`,
}

var brainExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Serialize project brain to .gg/brain/ (JSONL, payload-only)",
	Long: `Export all Qdrant collections and Memgraph graph data to deterministic
JSONL files under .gg/brain/. Vectors are excluded by default — run
'gg reindex --embed' after import to rebuild them.

The output is git-trackable and byte-deterministic: identical store
state always produces identical files.`,
	RunE: runBrainExport,
}

var brainStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show brain snapshot metadata and verify checksums",
	RunE:  runBrainStatus,
}

var brainExportDryRun bool

var brainImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Restore Qdrant + Memgraph from .gg/brain/ (idempotent)",
	Long: `Import a brain snapshot from .gg/brain/ into the local Qdrant and Memgraph stores.

Validates manifest SHA-256 checksums and embedding model compatibility before writing.
By default uses upsert semantics — safe to run multiple times.

Use --wipe to drop all data before importing (destructive, requires --yes).`,
	RunE: runBrainImport,
}

var (
	brainImportDryRun        bool
	brainImportSkipEmbedCheck bool
	brainImportNoReindex     bool
	brainImportWipe          bool
	brainImportYes           bool
)

func init() {
	brainExportCmd.Flags().BoolVar(&brainExportDryRun, "dry-run", false, "print what would be written without writing")

	brainImportCmd.Flags().BoolVar(&brainImportDryRun, "dry-run", false, "report counts without writing")
	brainImportCmd.Flags().BoolVar(&brainImportSkipEmbedCheck, "skip-embed-check", false, "bypass embedding model mismatch check")
	brainImportCmd.Flags().BoolVar(&brainImportNoReindex, "no-reindex", false, "skip gg reindex --embed trigger after import")
	brainImportCmd.Flags().BoolVar(&brainImportWipe, "wipe", false, "drop all project data before importing (destructive)")
	brainImportCmd.Flags().BoolVar(&brainImportYes, "yes", false, "confirm destructive --wipe without interactive prompt")

	brainCmd.AddCommand(brainExportCmd, brainImportCmd, brainStatusCmd)
	rootCmd.AddCommand(brainCmd)
}

// brainManifest is the manifest.json schema (v1).
type brainManifest struct {
	SchemaVersion  int            `json:"schema_version"`
	GGVersion      string         `json:"gg_version"`
	ProjectID      string         `json:"project_id"`
	ExportedAt     string         `json:"exported_at"`
	EmbeddingModel string         `json:"embedding_model"`
	EmbeddingDim   int            `json:"embedding_dim"`
	Counts         map[string]int `json:"counts"`
	SHA256         map[string]string `json:"sha256"`
}

func runBrainExport(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ggDir, err := config.GGDir()
	if err != nil {
		return err
	}

	// Read embedding meta for manifest.
	embMeta, err := embedding.ReadMeta(ggDir)
	if err != nil {
		return fmt.Errorf("read embedding meta: %w", err)
	}
	embModel := ""
	embDim := store.VectorSize
	if embMeta != nil {
		embModel = embMeta.ModelName
		embDim = embMeta.Dim
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// Collect all data before touching the filesystem.
	qdrantData, err := collectQdrantData(ctx, cfg, ggDir)
	if err != nil {
		return err
	}
	graphNodes, graphEdges, err := collectGraphData(ctx, cfg)
	if err != nil {
		// Memgraph optional — warn and continue with empty graph.
		fmt.Fprintf(os.Stderr, "⚠ Memgraph unavailable (%v) — exporting Qdrant only\n", err)
		graphNodes = nil
		graphEdges = nil
	}

	if brainExportDryRun {
		return printBrainDryRun(qdrantData, graphNodes, graphEdges)
	}

	partialDir := filepath.Join(ggDir, brainPartialDirName)
	finalDir := filepath.Join(ggDir, brainDirName)

	// Clean up any previous partial attempt.
	_ = os.RemoveAll(partialDir)
	if err := os.MkdirAll(partialDir, 0o755); err != nil {
		return fmt.Errorf("create brain.partial dir: %w", err)
	}

	checksums := make(map[string]string)
	counts := make(map[string]int)

	// Write Qdrant JSONL files.
	for _, kind := range store.BrainKind {
		records := qdrantData[kind]
		fname := kind + ".jsonl"
		sum, n, writeErr := writeBrainJSONL(partialDir, fname, records)
		if writeErr != nil {
			_ = os.RemoveAll(partialDir)
			return fmt.Errorf("write %s: %w", fname, writeErr)
		}
		checksums[fname] = sum
		counts[kind] = n
	}

	// Write chunks.jsonl (Memgraph nodes).
	{
		sum, n, writeErr := writeBrainJSONL(partialDir, "chunks.jsonl", graphNodes)
		if writeErr != nil {
			_ = os.RemoveAll(partialDir)
			return fmt.Errorf("write chunks.jsonl: %w", writeErr)
		}
		checksums["chunks.jsonl"] = sum
		counts["chunks"] = n
	}

	// Write edges.jsonl (Memgraph edges).
	{
		sum, n, writeErr := writeBrainJSONL(partialDir, "edges.jsonl", graphEdges)
		if writeErr != nil {
			_ = os.RemoveAll(partialDir)
			return fmt.Errorf("write edges.jsonl: %w", writeErr)
		}
		checksums["edges.jsonl"] = sum
		counts["edges"] = n
	}

	// Write manifest.json.
	manifest := brainManifest{
		SchemaVersion:  1,
		GGVersion:      buildVersion(),
		ProjectID:      cfg.ProjectID,
		ExportedAt:     time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		EmbeddingModel: embModel,
		EmbeddingDim:   embDim,
		Counts:         counts,
		SHA256:         checksums,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = os.RemoveAll(partialDir)
		return fmt.Errorf("marshal manifest: %w", err)
	}
	manifestPath := filepath.Join(partialDir, "manifest.json")
	if err := os.WriteFile(manifestPath, append(manifestBytes, '\n'), 0o644); err != nil {
		_ = os.RemoveAll(partialDir)
		return fmt.Errorf("write manifest.json: %w", err)
	}

	// Atomic rename: partial → final.
	_ = os.RemoveAll(finalDir)
	if err := os.Rename(partialDir, finalDir); err != nil {
		return fmt.Errorf("rename brain.partial → brain: %w", err)
	}

	// Ensure .gg/.gitignore covers brain.partial/.
	ensureBrainPartialIgnored(ggDir)

	total := 0
	for _, n := range counts {
		total += n
	}
	fmt.Printf("✓ Brain exported → %s (%d records)\n", finalDir, total)
	for _, kind := range store.BrainKind {
		if n := counts[kind]; n > 0 {
			fmt.Printf("  %-16s %d\n", kind, n)
		}
	}
	if n := counts["chunks"]; n > 0 {
		fmt.Printf("  %-16s %d\n", "chunks (graph)", n)
	}
	if n := counts["edges"]; n > 0 {
		fmt.Printf("  %-16s %d\n", "edges (graph)", n)
	}
	return nil
}

// brainStatusReport is the structured output for 'gg brain status --json'.
type brainStatusReport struct {
	Snapshot   string            `json:"snapshot"`    // "none" | "present"
	ExportedAt string            `json:"exported_at,omitempty"`
	GGVersion  string            `json:"gg_version,omitempty"`
	Schema     int               `json:"schema,omitempty"`
	Checksums  string            `json:"checksums,omitempty"` // "ok" | "mismatch"
	Drift      string            `json:"drift,omitempty"`     // "in_sync" | "drifted" | "unknown"
	DriftDelta map[string]int    `json:"drift_delta,omitempty"` // kind → live-snapshot delta
	Counts     map[string]int    `json:"counts,omitempty"`
}

func runBrainStatus(cmd *cobra.Command, _ []string) error {
	ggDir, err := config.GGDir()
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(ggDir, brainDirName, "manifest.json")
	data, err := os.ReadFile(manifestPath) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			if jsonOutput {
				return printJSON(brainStatusReport{Snapshot: "none"}, nil)
			}
			fmt.Println("Brain snapshot: none  (run: gg brain export)")
			return nil
		}
		return fmt.Errorf("read manifest: %w", err)
	}

	var manifest brainManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	// Verify file checksums.
	brainDir := filepath.Join(ggDir, brainDirName)
	allOK := true
	for fname, expected := range manifest.SHA256 {
		actual, checksumErr := fileChecksum(filepath.Join(brainDir, fname))
		if checksumErr != nil || actual != expected {
			allOK = false
		}
	}
	checksumStatus := "ok"
	if !allOK {
		checksumStatus = "mismatch"
	}

	// Live drift detection: compare manifest counts with live Qdrant counts.
	driftStatus := "unknown"
	driftDelta := make(map[string]int)
	cfg, cfgErr := config.Load()
	if cfgErr == nil {
		liveCtx, liveCancel := context.WithTimeout(cmd.Context(), healthCheckTimeout)
		defer liveCancel()

		sc, scErr := store.New(&cfg.Qdrant, ggDir, cfg.ProjectID)
		if scErr == nil {
			defer func() { _ = sc.Close() }()
			if hErr := sc.HealthCheck(liveCtx); hErr == nil {
				hasDrift := false
				for _, kind := range store.BrainKind {
					live, scrollErr := sc.ExportBrainCollection(liveCtx, kind)
					if scrollErr != nil {
						driftStatus = "unknown"
						hasDrift = false
						break
					}
					delta := len(live) - manifest.Counts[kind]
					if delta != 0 {
						driftDelta[kind] = delta
						hasDrift = true
					}
				}
				if driftStatus != "unknown" {
					if hasDrift {
						driftStatus = "drifted"
					} else {
						driftStatus = "in_sync"
					}
				}
			}
		}
	}

	report := brainStatusReport{
		Snapshot:   "present",
		ExportedAt: manifest.ExportedAt,
		GGVersion:  manifest.GGVersion,
		Schema:     manifest.SchemaVersion,
		Checksums:  checksumStatus,
		Drift:      driftStatus,
		Counts:     manifest.Counts,
	}
	if len(driftDelta) > 0 {
		report.DriftDelta = driftDelta
	}

	if jsonOutput {
		return printJSON(report, nil)
	}

	fmt.Println("Brain snapshot: present")
	fmt.Printf("  Exported at:  %s\n", manifest.ExportedAt)
	fmt.Printf("  gg version:   %s\n", manifest.GGVersion)
	fmt.Printf("  Schema:       v%d\n", manifest.SchemaVersion)
	if manifest.EmbeddingModel != "" {
		fmt.Printf("  Embedding:    %s (dim %d)\n", manifest.EmbeddingModel, manifest.EmbeddingDim)
	}

	// Print snapshot counts.
	kindOrder := append(store.BrainKind, "chunks", "edges")
	countParts := []string{}
	for _, k := range kindOrder {
		if n := manifest.Counts[k]; n > 0 {
			countParts = append(countParts, fmt.Sprintf("%d %s", n, k))
		}
	}
	if len(countParts) > 0 {
		fmt.Printf("  Snapshot:     ")
		for i, p := range countParts {
			if i > 0 {
				fmt.Print(" · ")
			}
			fmt.Print(p)
		}
		fmt.Println()
	}

	checksumLine := "✓ all match"
	if !allOK {
		checksumLine = "✗ mismatch detected"
	}
	fmt.Printf("  Checksums:    %s\n", checksumLine)

	// Drift line.
	switch driftStatus {
	case "in_sync":
		fmt.Println("  Drift:        ✓ in sync with live stores")
	case "drifted":
		driftParts := []string{}
		for _, k := range kindOrder {
			if d := driftDelta[k]; d != 0 {
				sign := "+"
				if d < 0 {
					sign = ""
				}
				driftParts = append(driftParts, fmt.Sprintf("%s%d %s", sign, d, k))
			}
		}
		fmt.Printf("  Drift:        ⚠ %s — run: gg brain export\n", strings.Join(driftParts, ", "))
	default:
		fmt.Println("  Drift:        — (Qdrant unreachable)")
	}
	return nil
}

// collectQdrantData scrolls all Qdrant collections for the project.
func collectQdrantData(ctx context.Context, cfg *config.Config, ggDir string) (map[string][]any, error) {
	client, err := store.New(&cfg.Qdrant, ggDir, cfg.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("store init: %w", err)
	}
	defer func() { _ = client.Close() }()

	hctx, hcancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer hcancel()
	if hErr := client.HealthCheck(hctx); hErr != nil {
		return nil, storeDownErr()
	}

	data := make(map[string][]any, len(store.BrainKind))
	for _, kind := range store.BrainKind {
		records, scrollErr := client.ExportBrainCollection(ctx, kind)
		if scrollErr != nil {
			return nil, fmt.Errorf("scroll %s: %w", kind, scrollErr)
		}
		items := make([]any, len(records))
		for i, r := range records {
			items[i] = map[string]any{
				"id":      r.ID,
				"payload": r.Payload,
			}
		}
		data[kind] = items
	}
	return data, nil
}

// collectGraphData queries Memgraph for nodes and edges.
// Returns (nil, nil, error) when Memgraph is unavailable.
func collectGraphData(ctx context.Context, cfg *config.Config) ([]any, []any, error) {
	if cfg.Memgraph.URI == "" {
		return nil, nil, fmt.Errorf("memgraph not configured")
	}
	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = gc.Close(ctx) }()

	nodes, err := gc.ExportNodes(ctx)
	if err != nil {
		return nil, nil, err
	}
	edges, err := gc.ExportEdges(ctx)
	if err != nil {
		return nil, nil, err
	}

	nodeItems := make([]any, len(nodes))
	for i, n := range nodes {
		nodeItems[i] = map[string]any{
			"id":         n.ID,
			"label":      n.Label,
			"properties": n.Properties,
		}
	}
	edgeItems := make([]any, len(edges))
	for i, e := range edges {
		edgeItems[i] = map[string]any{
			"dst":        e.Dst,
			"properties": e.Properties,
			"src":        e.Src,
			"type":       e.Type,
		}
	}
	return nodeItems, edgeItems, nil
}

// writeBrainJSONL writes items as canonical JSONL to dir/filename, returns
// (sha256hex, count, error). Creates an empty file when items is nil/empty.
func writeBrainJSONL(dir, filename string, items []any) (string, int, error) {
	path := filepath.Join(dir, filename)
	f, err := os.Create(path) //nolint:gosec
	if err != nil {
		return "", 0, err
	}

	h := sha256.New()
	w := bufio.NewWriter(f)
	for _, item := range items {
		line, encErr := store.CanonicalJSON(item)
		if encErr != nil {
			_ = f.Close()
			return "", 0, fmt.Errorf("encode record: %w", encErr)
		}
		line = append(line, '\n')
		if _, wErr := w.Write(line); wErr != nil {
			_ = f.Close()
			return "", 0, wErr
		}
		h.Write(line)
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return "", 0, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return "", 0, err
	}
	if err := f.Close(); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), len(items), nil
}

// fileChecksum returns the hex SHA-256 of the file at path.
func fileChecksum(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// printBrainDryRun prints what would be written without touching the filesystem.
func printBrainDryRun(qdrantData map[string][]any, nodes, edges []any) error {
	fmt.Println("Dry run — would write .gg/brain/:")
	kindOrder := append(store.BrainKind, "chunks", "edges")
	for _, k := range kindOrder {
		var n int
		switch k {
		case "chunks":
			n = len(nodes)
		case "edges":
			n = len(edges)
		default:
			n = len(qdrantData[k])
		}
		fmt.Printf("  %-20s %d records\n", k+".jsonl", n)
	}
	fmt.Println("  manifest.json")
	return nil
}

// ensureBrainPartialIgnored appends brain.partial/ to .gg/.gitignore if missing.
func ensureBrainPartialIgnored(ggDir string) {
	gitignorePath := filepath.Join(ggDir, ".gitignore")
	data, err := os.ReadFile(gitignorePath) //nolint:gosec
	if err != nil {
		return
	}
	if strings.Contains(string(data), "brain.partial/") {
		return
	}
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintln(f, "brain.partial/")
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
	brainDir := filepath.Join(ggDir, brainDirName)

	// 1. Read and validate manifest.
	manifestPath := filepath.Join(brainDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no brain snapshot found at %s (run: gg brain export)", brainDir)
		}
		return fmt.Errorf("read manifest: %w", err)
	}
	var manifest brainManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	// Schema version check.
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf(
			"brain import: unsupported schema version %d (this binary supports up to v1)\nUpdate gg to the latest version.",
			manifest.SchemaVersion,
		)
	}

	// SHA-256 checksum verification.
	for fname, expected := range manifest.SHA256 {
		actual, checksumErr := fileChecksum(filepath.Join(brainDir, fname))
		if checksumErr != nil {
			return fmt.Errorf("brain import: checksum error for %s: %w", fname, checksumErr)
		}
		if actual != expected {
			return fmt.Errorf(
				"brain import: checksum mismatch for %s\n  expected: %s\n  actual:   %s\nRe-run 'gg brain export' to regenerate a clean snapshot.",
				fname, expected, actual,
			)
		}
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
		kindOrder := append(store.BrainKind, "chunks", "edges")
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

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// 5. Qdrant import.
	storeClient, err := store.New(&cfg.Qdrant, ggDir, cfg.ProjectID)
	if err != nil {
		return fmt.Errorf("store init: %w", err)
	}
	defer func() { _ = storeClient.Close() }()

	hctx, hcancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer hcancel()
	if hErr := storeClient.HealthCheck(hctx); hErr != nil {
		return storeDownErr()
	}

	if err := storeClient.EnsureCollections(ctx, uint64(store.VectorSize)); err != nil {
		return fmt.Errorf("ensure collections: %w", err)
	}

	if brainImportWipe {
		if dropErr := storeClient.DropAllCollections(ctx); dropErr != nil {
			return fmt.Errorf("drop collections: %w", dropErr)
		}
		if recreateErr := storeClient.EnsureCollections(ctx, uint64(store.VectorSize)); recreateErr != nil {
			return fmt.Errorf("recreate collections: %w", recreateErr)
		}
		fmt.Fprintln(os.Stderr, "✓ Qdrant collections wiped and recreated")
	}

	totalQdrant := 0
	for _, kind := range store.BrainKind {
		fname := filepath.Join(brainDir, kind+".jsonl")
		records, readErr := store.ReadBrainJSONL(fname)
		if readErr != nil {
			return fmt.Errorf("read %s.jsonl: %w", kind, readErr)
		}
		if upsertErr := storeClient.UpsertBrainRecords(ctx, kind, records); upsertErr != nil {
			return fmt.Errorf("upsert %s: %w", kind, upsertErr)
		}
		totalQdrant += len(records)
	}

	// 6. Memgraph import (optional — skip when graph files are absent/empty).
	totalChunks, totalEdges, skippedChunks, skippedEdges := 0, 0, 0, 0
	chunksPath := filepath.Join(brainDir, "chunks.jsonl")
	edgesPath := filepath.Join(brainDir, "edges.jsonl")

	if cfg.Memgraph.URI != "" && manifest.Counts["chunks"] > 0 {
		gc, gcErr := graph.New(&cfg.Memgraph, cfg.ProjectID)
		if gcErr != nil {
			fmt.Fprintf(os.Stderr, "⚠ Memgraph init failed (%v) — skipping graph import\n", gcErr)
		} else {
			defer func() { _ = gc.Close(ctx) }()

			if brainImportWipe {
				if sweepErr := gc.SweepProject(ctx); sweepErr != nil {
					return fmt.Errorf("sweep graph: %w", sweepErr)
				}
				fmt.Fprintln(os.Stderr, "✓ Memgraph project swept")
			}

			chunkResult, chunkErr := gc.ImportChunks(ctx, chunksPath)
			if chunkErr != nil {
				return fmt.Errorf("import chunks: %w", chunkErr)
			}
			totalChunks = chunkResult.Imported
			skippedChunks = chunkResult.Skipped

			imp, skip, edgeErr := gc.ImportEdges(ctx, edgesPath, chunkResult.OldToNew)
			if edgeErr != nil {
				return fmt.Errorf("import edges: %w", edgeErr)
			}
			totalEdges = imp
			skippedEdges = skip
		}
	}

	// 7. Auto re-embed missing vectors (TASK-136).
	totalEmbedded := 0
	if !brainImportNoReindex && manifest.EmbeddingModel != "" {
		// Check whether the configured model matches the snapshot's model.
		embMeta, metaErr := embedding.ReadMeta(ggDir)
		modelMatch := metaErr == nil && embMeta != nil && embMeta.ModelName == manifest.EmbeddingModel

		if modelMatch || brainImportSkipEmbedCheck {
			fmt.Fprintln(os.Stderr, "Generating missing vectors…")
			embedCtx, embedCancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
			defer embedCancel()

			dim := store.VectorSize
			if embMeta != nil {
				dim = embMeta.Dim
			}
			gen := embedding.New(&cfg.Embedding, dim)
			embedResults, embedErr := storeClient.EmbedMissing(embedCtx, gen, os.Stderr)
			if embedErr != nil {
				fmt.Fprintf(os.Stderr, "⚠ re-embed interrupted: %v\n  Re-run 'gg brain import' to resume.\n", embedErr)
			} else {
				for _, r := range embedResults {
					totalEmbedded += r.Embedded
				}
			}
		} else {
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
		}
	}

	// 8. Report.
	fmt.Printf("✓ Brain imported from %s\n", brainDir)
	fmt.Printf("  Qdrant:  %d records\n", totalQdrant)
	if totalChunks+skippedChunks > 0 {
		fmt.Printf("  Nodes:   %d imported, %d skipped\n", totalChunks, skippedChunks)
		fmt.Printf("  Edges:   %d imported, %d skipped\n", totalEdges, skippedEdges)
	}
	if totalEmbedded > 0 {
		fmt.Printf("  Vectors: %d embedded\n", totalEmbedded)
	}
	return nil
}

// buildVersion returns the gg binary version from ldflags or build info.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}
	return "dev"
}

