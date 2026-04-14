package cmd

import (
	"fmt"
	"strings"

	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

var rejectCmd = &cobra.Command{
	Use:   `reject "approach"`,
	Short: "Record a rejected approach (deprecated: use gg record --stance=reject)",
	Long: `Record a rejected approach.

DEPRECATED: use 'gg record --stance=reject' instead.
This command will be removed in a future release.

  gg record --stance=reject "approach" --reason "why"`,
	Args: cobra.ExactArgs(1),
	RunE: runReject,
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
	fmt.Fprintln(cmd.ErrOrStderr(), "warning: 'gg reject' is deprecated — use 'gg record --stance=reject' instead")

	approach, err := requireNonEmpty("approach", args[0])
	if err != nil {
		return err
	}
	reason := strings.TrimSpace(rejectReason)
	taskRef, err := normalizeTaskRef(rejectTask)
	if err != nil {
		return fmt.Errorf("--task: %w", err)
	}

	d, err := loadDeps(true)
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
	vector, err := d.embedder.Generate(ctx, embedText)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	if promptIfDuplicate(ctx, d, "rejections", vector) {
		fmt.Println("Aborted — nothing recorded.")
		return nil
	}

	r := store.Rejection{
		Approach: approach,
		Reason:   reason,
		Tags:     parseTags(rejectTags),
		TaskID:   taskRef,
		Author:   resolveAuthor(cmd),
	}

	if err := d.store.AddRejection(ctx, r, vector); err != nil {
		return fmt.Errorf("store rejection: %w", err)
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
