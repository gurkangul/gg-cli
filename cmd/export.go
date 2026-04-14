package cmd

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export [output.json.gz]",
	Short: "Export all project data to a portable bundle",
	Long: `Export all Qdrant data (decisions, tasks, notes, discussions, messages,
rejections, bugs) into a gzip-compressed JSON bundle.

The bundle includes embedding vectors so that 'gg import' can restore the
project without re-embedding on the target machine.

Note: Memgraph graph data (gg index) is not included in the bundle.
Run 'gg index' after import to rebuild the code graph.

Examples:
  gg export                         # writes gg-export-<date>.json.gz
  gg export my-project.json.gz      # writes to the given path`,
	Args: cobra.MaximumNArgs(1),
	RunE: runExport,
}

func init() {
	rootCmd.AddCommand(exportCmd)
}

func runExport(cmd *cobra.Command, args []string) error {
	outPath := fmt.Sprintf("gg-export-%s.json.gz", time.Now().UTC().Format("2006-01-02"))
	if len(args) == 1 {
		outPath = args[0]
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	fmt.Fprintln(os.Stderr, "Exporting project data…")
	bundle, err := d.store.ExportBundle(ctx)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	total := 0
	collCounts := make(map[string]int, len(bundle.Collections))
	for kind, pts := range bundle.Collections {
		collCounts[kind] = len(pts)
		total += len(pts)
	}

	f, err := os.Create(filepath.Clean(outPath))
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}

	gz := gzip.NewWriter(f)
	if err := json.NewEncoder(gz).Encode(bundle); err != nil {
		_ = gz.Close()
		_ = f.Close()
		return fmt.Errorf("encode bundle: %w", err)
	}
	if err := gz.Close(); err != nil {
		_ = f.Close()
		return fmt.Errorf("close gzip: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}

	size := int64(0)
	if fi, statErr := os.Stat(outPath); statErr == nil {
		size = fi.Size()
	}

	return printJSON(map[string]any{
		"output":       outPath,
		"total_points": total,
		"size_bytes":   size,
		"collections":  collCounts,
	}, func() {
		fmt.Printf("✓ Exported %d points → %s (%s)\n", total, outPath, humanFileSize(size))
		for kind, n := range collCounts {
			if n > 0 {
				fmt.Printf("  %-16s %d\n", kind, n)
			}
		}
	})
}

func humanFileSize(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
