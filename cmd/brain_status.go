package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

// brainStatusReport is the structured output for 'gg brain status --json'.
type brainStatusReport struct {
	Snapshot   string         `json:"snapshot"`              // "none" | "present"
	ExportedAt string         `json:"exported_at,omitempty"`
	GGVersion  string         `json:"gg_version,omitempty"`
	Schema     int            `json:"schema,omitempty"`
	Checksums  string         `json:"checksums,omitempty"`   // "ok" | "mismatch"
	Drift      string         `json:"drift,omitempty"`       // "in_sync" | "drifted" | "unknown"
	DriftDelta map[string]int `json:"drift_delta,omitempty"` // kind → live-snapshot delta
	Counts     map[string]int `json:"counts,omitempty"`
}

// brainLiveCounter is the minimal interface needed by computeDrift.
// *store.Client satisfies it; tests can inject a fake.
type brainLiveCounter interface {
	ExportBrainCollection(ctx context.Context, kind string) ([]store.BrainRecord, error)
}

// computeDrift compares live Qdrant counts against the snapshot manifest.
// Returns ("in_sync", delta), ("drifted", delta), or ("unknown", nil) on scroll error.
func computeDrift(ctx context.Context, counter brainLiveCounter, manifest brainManifest) (string, map[string]int) {
	delta := make(map[string]int)
	hasDrift := false
	for _, kind := range store.BrainKind {
		live, err := counter.ExportBrainCollection(ctx, kind)
		if err != nil {
			return "unknown", nil
		}
		if d := len(live) - manifest.Counts[kind]; d != 0 {
			delta[kind] = d
			hasDrift = true
		}
	}
	if hasDrift {
		return "drifted", delta
	}
	return "in_sync", delta
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
	if err := brainUnmarshalManifest(data, &manifest); err != nil {
		return err
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
				driftStatus, driftDelta = computeDrift(liveCtx, sc, manifest)
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
	kindOrder := append(append([]string(nil), store.BrainKind...), "chunks", "edges")
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
