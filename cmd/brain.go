package cmd

import (
	"encoding/json"
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
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

var (
	brainExportDryRun  bool
	brainExportStrict  bool
	brainExportIfStale string // "" means no staleness check; non-empty is a Go duration string
	brainExportVerbose bool
)

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
	brainImportDryRun               bool
	brainImportSkipEmbedCheck       bool
	brainImportNoReindex            bool
	brainImportWipe                 bool
	brainImportYes                  bool
	brainImportForceProjectMismatch bool
)

func init() {
	brainExportCmd.Flags().BoolVar(&brainExportDryRun, "dry-run", false, "print what would be written without writing")
	brainExportCmd.Flags().BoolVar(&brainExportStrict, "strict", false, "exit 1 if any secret pattern is found, write nothing")
	brainExportCmd.Flags().StringVar(&brainExportIfStale, "if-stale", "", "only export when snapshot is older than DURATION (e.g. 24h); exit 0 when fresh")
	brainExportCmd.Flags().BoolVar(&brainExportVerbose, "verbose", false, "show skip reason when --if-stale skips the export")

	brainImportCmd.Flags().BoolVar(&brainImportDryRun, "dry-run", false, "report counts without writing")
	brainImportCmd.Flags().BoolVar(&brainImportSkipEmbedCheck, "skip-embed-check", false, "bypass embedding model mismatch check")
	brainImportCmd.Flags().BoolVar(&brainImportNoReindex, "no-reindex", false, "skip gg reindex --embed trigger after import")
	brainImportCmd.Flags().BoolVar(&brainImportWipe, "wipe", false, "drop all project data before importing (destructive)")
	brainImportCmd.Flags().BoolVar(&brainImportYes, "yes", false, "confirm destructive --wipe without interactive prompt")
	brainImportCmd.Flags().BoolVar(&brainImportForceProjectMismatch, "force-project-mismatch", false, "allow importing a snapshot from a different project_id")

	brainCmd.AddCommand(brainExportCmd, brainImportCmd, brainStatusCmd, brainBackfillCmd)
	rootCmd.AddCommand(brainCmd)
}

// brainManifest is the manifest.json schema (v1).
type brainManifest struct {
	SchemaVersion  int               `json:"schema_version"`
	GGVersion      string            `json:"gg_version"`
	ProjectID      string            `json:"project_id"`
	ExportedAt     string            `json:"exported_at"`
	EmbeddingModel string            `json:"embedding_model"`
	EmbeddingDim   int               `json:"embedding_dim"`
	Counts         map[string]int    `json:"counts"`
	SHA256         map[string]string `json:"sha256"`
	Scrubbed       int               `json:"scrubbed"`
}

func brainMarshalManifest(m brainManifest) ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	return b, nil
}

func brainUnmarshalManifest(data []byte, m *brainManifest) error {
	if err := json.Unmarshal(data, m); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
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
