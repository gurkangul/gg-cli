package cmd

import (
	"fmt"
	"strings"

	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

var recordCmd = &cobra.Command{
	Use:   `record "text"`,
	Short: "Record a decision or rejected approach",
	Long: `Record a decision (default) or a rejected approach.

  gg record "use JWT for auth"                         # accepted decision
  gg record --stance=reject "store sessions in Redis"  # rejected approach

This is the canonical verb. 'gg decide' and 'gg reject' still work but are
deprecated and will be removed in a future major release.`,
	Args: cobra.ExactArgs(1),
	RunE: runRecord,
}

var (
	recordReason string
	recordTags   string
	recordTask   string
	recordStance string
)

func init() {
	recordCmd.Flags().StringVar(&recordReason, "reason", "", "why this decision was made (or rejected)")
	recordCmd.Flags().StringVar(&recordTags, "tags", "", "comma-separated tags")
	recordCmd.Flags().StringVar(&recordTask, "task", "", "related task ID")
	recordCmd.Flags().StringVar(&recordStance, "stance", "accept", `stance: "accept" (decision) or "reject" (rejection)`)
	addFromFlag(recordCmd)
	rootCmd.AddCommand(recordCmd)
}

func runRecord(cmd *cobra.Command, args []string) error {
	text, err := requireNonEmpty("text", args[0])
	if err != nil {
		return err
	}

	stance := strings.ToLower(strings.TrimSpace(recordStance))
	if stance != "accept" && stance != "reject" {
		return fmt.Errorf("--stance must be \"accept\" or \"reject\", got %q", recordStance)
	}

	reason := strings.TrimSpace(recordReason)
	taskRef, err := normalizeTaskRef(recordTask)
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

	embedText := text
	if reason != "" {
		embedText = text + " " + reason
	}
	vector, err := d.embedder.Generate(ctx, embedText)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	if stance == "reject" {
		r := store.Rejection{
			Approach: text,
			Reason:   reason,
			Tags:     parseTags(recordTags),
			TaskID:   taskRef,
			Author:   resolveAuthor(cmd),
		}
		if err := d.store.AddRejection(ctx, r, vector); err != nil {
			return fmt.Errorf("store rejection: %w", err)
		}
		return printJSON(r, func() {
			fmt.Printf("✗ Rejection recorded: %s\n", text)
			if r.Reason != "" {
				fmt.Printf("  Reason: %s\n", r.Reason)
			}
			if len(r.Tags) > 0 {
				fmt.Printf("  Tags: %s\n", strings.Join(r.Tags, ", "))
			}
		})
	}

	dec := store.Decision{
		Text:   text,
		Reason: reason,
		Tags:   parseTags(recordTags),
		TaskID: taskRef,
		Author: resolveAuthor(cmd),
	}
	if err := d.store.AddDecision(ctx, dec, vector); err != nil {
		return fmt.Errorf("store decision: %w", err)
	}
	return printJSON(dec, func() {
		fmt.Printf("✓ Decision recorded: %s\n", text)
		if dec.Reason != "" {
			fmt.Printf("  Reason: %s\n", dec.Reason)
		}
		if len(dec.Tags) > 0 {
			fmt.Printf("  Tags: %s\n", strings.Join(dec.Tags, ", "))
		}
	})
}
