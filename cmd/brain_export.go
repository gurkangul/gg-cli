package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/scrub"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

func runBrainExport(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ggDir, err := config.GGDir()
	if err != nil {
		return err
	}

	// --if-stale check: read manifest, compare ExportedAt to now.
	if brainExportIfStale != "" {
		threshold, parseErr := time.ParseDuration(brainExportIfStale)
		if parseErr != nil {
			return fmt.Errorf("--if-stale: invalid duration %q: %w", brainExportIfStale, parseErr)
		}
		skip, age, checkErr := brainSnapshotFresh(ggDir, threshold)
		if checkErr == nil && skip {
			if brainExportVerbose {
				fmt.Printf("skipped: snapshot fresh (%s old)\n", age.Truncate(time.Minute))
			}
			return nil
		}
	}

	if !jsonOutput {
		fmt.Fprintf(os.Stderr, "Project: %s\n", cfg.ProjectID)
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
		fmt.Fprintf(os.Stderr, "⚠ code graph unavailable (%v) — exporting vector store only\n", err)
		graphNodes = nil
		graphEdges = nil
	}

	// Scrub secrets from all collected payloads.
	totalScrubbed := 0
	for kind, items := range qdrantData {
		for i, item := range items {
			scrubbed, n := scrub.Any(item)
			if n > 0 {
				items[i] = scrubbed
				totalScrubbed += n
				fmt.Fprintf(os.Stderr, "⚠ secrets scrubbed from %s record %d (%d match(es))\n", kind, i, n)
			}
		}
		qdrantData[kind] = items
	}
	for i, node := range graphNodes {
		scrubbed, n := scrub.Any(node)
		if n > 0 {
			graphNodes[i] = scrubbed
			totalScrubbed += n
			fmt.Fprintf(os.Stderr, "⚠ secrets scrubbed from graph node %d (%d match(es))\n", i, n)
		}
	}
	for i, edge := range graphEdges {
		scrubbed, n := scrub.Any(edge)
		if n > 0 {
			graphEdges[i] = scrubbed
			totalScrubbed += n
			fmt.Fprintf(os.Stderr, "⚠ secrets scrubbed from graph edge %d (%d match(es))\n", i, n)
		}
	}
	if brainExportStrict && totalScrubbed > 0 {
		return fmt.Errorf("brain export --strict: %d secret pattern(s) detected — export aborted", totalScrubbed)
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

	// Preserve append-only local task lifecycle events. They are not a Qdrant
	// collection, so a snapshot export must carry the existing JSONL file
	// forward before atomically replacing .gg/brain/.
	{
		sum, n, writeErr := copyExistingBrainJSONL(finalDir, partialDir, "task-events.jsonl")
		if writeErr != nil {
			_ = os.RemoveAll(partialDir)
			return fmt.Errorf("write task-events.jsonl: %w", writeErr)
		}
		checksums["task-events.jsonl"] = sum
		counts["task-events"] = n
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
		Scrubbed:       totalScrubbed,
	}
	manifestBytes, encErr := brainMarshalManifest(manifest)
	if encErr != nil {
		_ = os.RemoveAll(partialDir)
		return encErr
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

	// AC-4: when called via --if-stale (auto-backup mode), use the compact one-liner.
	if brainExportIfStale != "" {
		fmt.Printf("✓ brain auto-snapshotted (%d records)\n", total)
	} else {
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
	}

	// AC-6: warn if brain directory size exceeds 200MB.
	warnBrainSizeIfLarge(finalDir)
	return nil
}

// collectQdrantData scrolls all Qdrant collections for the project.
func collectQdrantData(ctx context.Context, cfg *config.Config, ggDir string) (map[string][]any, error) {
	client, err := store.New(ggDir, cfg.ProjectID)
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
	gc, err := graph.New(cfg.DataDir, cfg.ProjectID)
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
	kindOrder := append(append([]string(nil), store.BrainKind...), "chunks", "edges", "task-events")
	for _, k := range kindOrder {
		var n int
		switch k {
		case "chunks":
			n = len(nodes)
		case "edges":
			n = len(edges)
		case "task-events":
			fmt.Printf("  %-20s preserve existing file if present\n", k+".jsonl")
			continue
		default:
			n = len(qdrantData[k])
		}
		fmt.Printf("  %-20s %d records\n", k+".jsonl", n)
	}
	fmt.Println("  manifest.json")
	return nil
}

// brainSnapshotFresh reports whether the existing snapshot is newer than threshold.
// Returns (true, age, nil) when fresh; (false, age, nil) when stale; (false, 0, err) when manifest is missing or unreadable.
func brainSnapshotFresh(ggDir string, threshold time.Duration) (bool, time.Duration, error) {
	manifestPath := filepath.Join(ggDir, brainDirName, "manifest.json")
	data, err := os.ReadFile(manifestPath) //nolint:gosec
	if err != nil {
		return false, 0, err
	}
	var m brainManifest
	if err := brainUnmarshalManifest(data, &m); err != nil {
		return false, 0, err
	}
	exportedAt, err := time.Parse(time.RFC3339, m.ExportedAt)
	if err != nil {
		return false, 0, fmt.Errorf("parse ExportedAt: %w", err)
	}
	age := time.Since(exportedAt)
	return age < threshold, age, nil
}

const defaultBrainSizeWarnBytes = 200 * 1024 * 1024 // 200 MB

func brainSizeWarnThreshold() int64 {
	if v := os.Getenv("GG_BRAIN_SIZE_WARN_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return defaultBrainSizeWarnBytes
}

// warnBrainSizeIfLarge prints a single warning line when the brain directory
// exceeds the configured size threshold. Non-fatal — any error is silently ignored.
func warnBrainSizeIfLarge(dir string) {
	var total int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, infoErr := e.Info(); infoErr == nil {
			total += info.Size()
		}
	}
	if total > brainSizeWarnThreshold() {
		fmt.Fprintf(os.Stderr, "⚠ brain snapshot is %dMB — consider pruning older data. See: gg brain export docs\n", total/(1024*1024))
	}
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
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintln(f, "brain.partial/")
}
