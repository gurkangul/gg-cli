package cmd

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

var importCmd = &cobra.Command{
	Use:   "import <bundle.json.gz>",
	Short: "Import a project bundle exported by 'gg export'",
	Long: `Restore a project from a gzip-compressed JSON bundle.

By default the project ID from the bundle is preserved, meaning the data is
restored into the same logical project. Use --as to import under a new project
ID (useful when migrating to a new machine with a fresh config).

Note: Memgraph graph data is not included in bundles. Run 'gg index' after
import to rebuild the code intelligence graph.

Examples:
  gg import gg-export-2026-01-01.json.gz
  gg import gg-export-2026-01-01.json.gz --as new-project-uuid`,
	Args: cobra.ExactArgs(1),
	RunE: runImport,
}

var importAs string

func init() {
	importCmd.Flags().StringVar(&importAs, "as", "", "import under a different project ID (UUID)")
	rootCmd.AddCommand(importCmd)
}

func runImport(cmd *cobra.Command, args []string) error {
	bundlePath := args[0]

	f, err := os.Open(bundlePath) //nolint:gosec // path comes from user CLI argument
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read gzip: %w (is this a valid gg export bundle?)", err)
	}
	defer func() { _ = gz.Close() }()

	data, err := io.ReadAll(gz)
	if err != nil {
		return fmt.Errorf("decompress bundle: %w", err)
	}

	var bundle store.Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return fmt.Errorf("parse bundle: %w", err)
	}

	if bundle.Version != "1" {
		return fmt.Errorf("unsupported bundle version %q — this gg binary can only import version 1 bundles", bundle.Version)
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	total := 0
	for _, pts := range bundle.Collections {
		total += len(pts)
	}

	fmt.Fprintf(os.Stderr, "Importing %d points from bundle (source: %s)…\n", total, bundle.SourceProjectID)
	if err := d.store.ImportBundle(ctx, &bundle); err != nil {
		return fmt.Errorf("import: %w", err)
	}

	collCounts := make(map[string]int, len(bundle.Collections))
	for kind, pts := range bundle.Collections {
		collCounts[kind] = len(pts)
	}

	return printJSON(map[string]any{
		"source_project_id": bundle.SourceProjectID,
		"exported_at":       bundle.ExportedAt,
		"total_imported":    total,
		"collections":       collCounts,
	}, func() {
		fmt.Printf("✓ Imported %d points from %s\n", total, bundlePath)
		for kind, n := range collCounts {
			if n > 0 {
				fmt.Printf("  %-16s %d\n", kind, n)
			}
		}
		fmt.Println("\nRun 'gg index' to rebuild the code intelligence graph.")
	})
}
