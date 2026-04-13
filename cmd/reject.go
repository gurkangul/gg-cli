package cmd

import (
	"context"
	"fmt"

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
	approach := args[0]

	embedder, err := newEmbedder()
	if err != nil {
		return err
	}

	embedText := approach
	if rejectReason != "" {
		embedText = approach + " " + rejectReason
	}
	vector, err := embedder.Generate(embedText)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	client, err := newStoreClient()
	if err != nil {
		return err
	}
	defer client.Close()

	r := store.Rejection{
		Approach: approach,
		Reason:   rejectReason,
		TaskID:   rejectTask,
	}

	if err := client.AddRejection(context.Background(), r, vector); err != nil {
		return fmt.Errorf("store rejection: %w", err)
	}

	fmt.Printf("✗ Rejection recorded: %s\n", approach)
	if rejectReason != "" {
		fmt.Printf("  Reason: %s\n", rejectReason)
	}
	return nil
}
