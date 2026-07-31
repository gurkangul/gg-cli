package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

var rejectCmd = &cobra.Command{
	Use:   `reject "approach"`,
	Short: "Record a rejected approach (deprecated: use gg record --decision-status=rejected)",
	Long: `Record a rejected approach.

DEPRECATED: use 'gg record --decision-status=rejected' instead.
Decision.status replaces the separate rejection primitive.
This command will be removed in a future release.

  gg record "approach" --decision-status=rejected --reason "why"
  gg record "use PostgreSQL" --rejected-alternatives "MySQL,SQLite" --reason "..."`,
	// Single runtime warning, emitted by cobra to stderr exactly once before
	// RunE. Wording sourced from the shared deprecationMessage helper. Do NOT
	// also Fprintln in runReject.
	Deprecated: deprecationMessage("gg record --decision-status=rejected"),
	Args:       cobra.ExactArgs(1),
	RunE:       runReject,
}

var (
	rejectReason string
	rejectTags   string
	rejectTask   string
)

func init() {
	rejectCmd.Flags().StringVar(&rejectReason, "reason", "", "why this approach was rejected")
	rejectCmd.Flags().StringVar(&rejectTags, "tags", "", "comma-separated tags")
	rejectCmd.Flags().StringVar(&rejectTask, "task", "", "related task ID")
	addFromFlag(rejectCmd)
	rootCmd.AddCommand(rejectCmd)
}

func runReject(cmd *cobra.Command, args []string) error {
	// Deprecation notice is emitted by cobra (see rejectCmd.Deprecated); do not
	// duplicate it here.
	approach, err := requireNonEmpty("approach", args[0])
	if err != nil {
		return err
	}
	reason := strings.TrimSpace(rejectReason)
	taskRef, err := normalizeTaskRef(rejectTask)
	if err != nil {
		return fmt.Errorf("--task: %w", err)
	}

	d, err := loadDepsOfflineSafe(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	embedText := approach
	if reason != "" {
		embedText = approach + " " + reason
	}
	var vector []float32
	if !d.qdrantDown {
		vector, err = d.embedder.Generate(ctx, embedText)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ embedding unavailable — JSONL write will continue and semantic indexing will be queued: %v\n", err)
		} else if promptIfDuplicate(ctx, d, "rejections", vector) {
			fmt.Println("Aborted — nothing recorded.")
			return nil
		}
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "⚠ vector store unavailable — read served from JSONL (may miss cross-project context)")
	}

	author, err := requireAuthor(cmd)
	if err != nil {
		return err
	}

	r := store.Rejection{
		Approach: approach,
		Reason:   reason,
		Tags:     parseTags(rejectTags),
		TaskID:   taskRef,
		Author:   author,
	}

	if err := d.store.AddRejection(ctx, r, vector); err != nil {
		var oq *store.OutboxQueued
		if errors.As(err, &oq) {
			queueBrainOutbox(oq, config.GGDirOrEmpty())
			warnBrainOutboxQueued(cmd.ErrOrStderr(), oq.Cause)
		} else {
			return fmt.Errorf("store rejection: %w", err)
		}
	}

	return printJSON(r, func() {
		fmt.Printf("✗ Rejection recorded: %s\n", approach)
		if r.Reason != "" {
			fmt.Printf("  Reason: %s\n", r.Reason)
		}
		if len(r.Tags) > 0 {
			fmt.Printf("  Tags: %s\n", strings.Join(r.Tags, ", "))
		}
	})
}
