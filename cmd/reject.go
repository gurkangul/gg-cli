package cmd

import (
	"fmt"
	"strings"

	"github.com/gurkangul/gg/internal/store"
	"github.com/spf13/cobra"
)

var rejectCmd = &cobra.Command{
	Use:   `reject "approach"`,
	Short: "Record a rejected approach",
	Args:  cobra.ExactArgs(1),
	RunE:  runReject,
}

var (
	rejectReason string
	rejectTask   string
)

func init() {
	rejectCmd.Flags().StringVar(&rejectReason, "reason", "", "why this approach was rejected")
	rejectCmd.Flags().StringVar(&rejectTask, "task", "", "related task ID")
	rootCmd.AddCommand(rejectCmd)
}

func runReject(cmd *cobra.Command, args []string) error {
	approach, err := requireNonEmpty("approach", args[0])
	if err != nil {
		return err
	}

	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	embedText := approach
	if rejectReason != "" {
		embedText = approach + " " + rejectReason
	}
	vector, err := d.embedder.Generate(ctx, embedText)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	r := store.Rejection{
		Approach: approach,
		Reason:   strings.TrimSpace(rejectReason),
		TaskID:   strings.TrimSpace(rejectTask),
	}

	if err := d.store.AddRejection(ctx, r, vector); err != nil {
		return fmt.Errorf("store rejection: %w", err)
	}

	fmt.Printf("✗ Rejection recorded: %s\n", approach)
	if r.Reason != "" {
		fmt.Printf("  Reason: %s\n", r.Reason)
	}
	return nil
}
