package cmd

import (
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
	text, err := requireNonEmpty("decision text", args[0])
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

	embedText := text
	if decideReason != "" {
		embedText = text + " " + decideReason
	}
	vector, err := d.embedder.Generate(ctx, embedText)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	dec := store.Decision{
		Text:   text,
		Reason: strings.TrimSpace(decideReason),
		Tags:   parseTags(decideTags),
		TaskID: strings.TrimSpace(decideTask),
	}

	if err := d.store.AddDecision(ctx, dec, vector); err != nil {
		return fmt.Errorf("store decision: %w", err)
	}

	fmt.Printf("✓ Decision recorded: %s\n", text)
	if dec.Reason != "" {
		fmt.Printf("  Reason: %s\n", dec.Reason)
	}
	if len(dec.Tags) > 0 {
		fmt.Printf("  Tags: %s\n", strings.Join(dec.Tags, ", "))
	}
	return nil
}
