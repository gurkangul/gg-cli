package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var taskReviewCmd = &cobra.Command{
	Use:   "review TASK-ID",
	Short: "Set review status on a task (approve or reject)",
	Long: `Update the review_status of a task without changing its lifecycle status.

Review status is orthogonal to task status: a done task can be approved or
rejected by a reviewer without re-opening it.

  gg task review TASK-042 --approve
  gg task review TASK-042 --approve --notes "LGTM, minor nit in line 47"
  gg task review TASK-042 --reject  --notes "Breaks isolation contract"`,
	Args: cobra.ExactArgs(1),
	RunE: runTaskReview,
}

var (
	taskReviewApprove bool
	taskReviewReject  bool
	taskReviewNotes   string
	taskReviewBy      string
)

func init() {
	taskReviewCmd.Flags().BoolVar(&taskReviewApprove, "approve", false, "approve the task")
	taskReviewCmd.Flags().BoolVar(&taskReviewReject, "reject", false, "reject the task")
	taskReviewCmd.Flags().StringVar(&taskReviewNotes, "notes", "", "reviewer notes")
	taskReviewCmd.Flags().StringVar(&taskReviewBy, "by", "", "reviewer role or name (defaults to GG_ROLE or 'reviewer')")
	taskCmd.AddCommand(taskReviewCmd)
}

func runTaskReview(cmd *cobra.Command, args []string) error {
	if taskReviewApprove == taskReviewReject {
		return fmt.Errorf("exactly one of --approve or --reject is required")
	}

	taskID, err := normalizeTaskRef(args[0])
	if err != nil {
		return fmt.Errorf("task ID: %w", err)
	}

	reviewStatus := "approved"
	if taskReviewReject {
		reviewStatus = "rejected"
	}

	reviewer := strings.TrimSpace(taskReviewBy)
	if reviewer == "" {
		reviewer = strings.TrimSpace(os.Getenv("GG_ROLE"))
	}
	if reviewer == "" {
		reviewer = "reviewer"
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.UpdateReviewStatus(ctx, taskID, reviewStatus, reviewer, strings.TrimSpace(taskReviewNotes)); err != nil {
		return fmt.Errorf("update review status: %w", err)
	}

	icon := "✓"
	if taskReviewReject {
		icon = "✗"
	}
	fmt.Printf("%s %s review_status=%s (by %s)\n", icon, taskID, reviewStatus, reviewer)
	if taskReviewNotes != "" {
		fmt.Printf("  Notes: %s\n", taskReviewNotes)
	}
	return nil
}
