package cmd

import (
	"fmt"
	"strings"

	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

var decideCmd = &cobra.Command{
	Use:   `decide "decision text"`,
	Short: "Record a decision or rejection (deprecated: use gg record)",
	Long: `Record a decision (default) or a rejected approach (--stance=reject).

DEPRECATED: use 'gg record' instead.
This command will be removed in a future major release.

  gg record "use JWT"                          # accepted decision
  gg record --stance=reject "use sessions"     # rejected approach`,
	Args: cobra.ExactArgs(1),
	RunE: runDecide,
}

var (
	decideReason string
	decideTags   string
	decideTask   string
	decideStance string
)

func init() {
	decideCmd.Flags().StringVar(&decideReason, "reason", "", "why this decision was made (or rejected)")
	decideCmd.Flags().StringVar(&decideTags, "tags", "", "comma-separated tags")
	decideCmd.Flags().StringVar(&decideTask, "task", "", "related task ID")
	decideCmd.Flags().StringVar(&decideStance, "stance", "accept", `stance: "accept" (decision) or "reject" (rejection)`)
	addFromFlag(decideCmd)
	rootCmd.AddCommand(decideCmd)
}

func runDecide(cmd *cobra.Command, args []string) error {
	fmt.Fprintln(cmd.ErrOrStderr(), "warning: 'gg decide' is deprecated — use 'gg record' instead")

	text, err := requireNonEmpty("decision text", args[0])
	if err != nil {
		return err
	}

	stance := strings.ToLower(strings.TrimSpace(decideStance))
	if stance != "accept" && stance != "reject" {
		return fmt.Errorf("--stance must be \"accept\" or \"reject\", got %q", decideStance)
	}

	if stance == "reject" {
		return runDecideAsRejection(cmd, text)
	}

	reason := strings.TrimSpace(decideReason)
	taskRef, err := normalizeTaskRef(decideTask)
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

	if promptIfDuplicate(ctx, d, "decisions", vector) {
		fmt.Println("Aborted — nothing recorded.")
		return nil
	}

	dec := store.Decision{
		Text:   text,
		Reason: reason,
		Tags:   parseTags(decideTags),
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

// runDecideAsRejection handles `gg decide --stance=reject`.
// It delegates to the same store path as `gg reject`, keeping the two
// command implementations in sync without duplicating logic.
func runDecideAsRejection(cmd *cobra.Command, approach string) error {
	reason := strings.TrimSpace(decideReason)
	taskRef, err := normalizeTaskRef(decideTask)
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
		Tags:     parseTags(decideTags),
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
