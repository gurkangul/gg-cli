package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var inboxArchiveOlderThan string

var inboxArchiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Archive stale agent-to-agent status broadcasts (out of inbox, kept in JSONL)",
	Long: `Archive ephemeral audience=agents status broadcasts ("TASK-N started/done"…)
older than a cutoff. Archived messages drop out of the default inbox so it stops
bloating, but stay in JSONL (forward-only — never deleted), recoverable via the
durable brain. Consolidation layer (TASK-470).`,
	Args: cobra.NoArgs,
	RunE: runInboxArchive,
}

func init() {
	inboxArchiveCmd.Flags().StringVar(&inboxArchiveOlderThan, "older-than", "720h",
		"archive audience=agents broadcasts older than this duration (default 720h = 30 days)")
	inboxCmd.AddCommand(inboxArchiveCmd)
}

func runInboxArchive(cmd *cobra.Command, _ []string) error {
	dur, err := parseDuration(inboxArchiveOlderThan)
	if err != nil {
		return fmt.Errorf("--older-than: %w", err)
	}
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()
	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()
	n, err := d.store.ArchiveAgentBroadcasts(ctx, time.Now().UTC().Add(-dur))
	if err != nil {
		return fmt.Errorf("archive broadcasts: %w", err)
	}
	fmt.Printf("✓ Archived %d stale agent broadcast(s) older than %s — out of inbox, kept in JSONL.\n", n, inboxArchiveOlderThan)
	return nil
}
