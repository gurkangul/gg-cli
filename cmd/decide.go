package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/gurkangul/gg/internal/store"
	"github.com/spf13/cobra"
)

var decideCmd = &cobra.Command{
	Use:   `decide "decision text"`,
	Short: "Record a decision",
	Args:  cobra.ExactArgs(1),
	RunE:  runDecide,
}

var (
	decideReason string
	decideTags   string
	decideTask   string
)

func init() {
	decideCmd.Flags().StringVar(&decideReason, "reason", "", "why this decision was made")
	decideCmd.Flags().StringVar(&decideTags, "tags", "", "comma-separated tags")
	decideCmd.Flags().StringVar(&decideTask, "task", "", "related task ID")
	rootCmd.AddCommand(decideCmd)
}

func runDecide(cmd *cobra.Command, args []string) error {
	text := args[0]

	embedder, err := newEmbedder()
	if err != nil {
		return err
	}

	// Build embedding from text + reason
	embedText := text
	if decideReason != "" {
		embedText = text + " " + decideReason
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

	d := store.Decision{
		Text:   text,
		Reason: decideReason,
		Tags:   parseTags(decideTags),
		TaskID: decideTask,
	}

	if err := client.AddDecision(context.Background(), d, vector); err != nil {
		return fmt.Errorf("store decision: %w", err)
	}

	fmt.Printf("✓ Decision recorded: %s\n", text)
	if decideReason != "" {
		fmt.Printf("  Reason: %s\n", decideReason)
	}
	if decideTags != "" {
		fmt.Printf("  Tags: %s\n", strings.Join(parseTags(decideTags), ", "))
	}
	return nil
}
