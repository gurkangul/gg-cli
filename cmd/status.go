package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show open tasks, pending messages, and recent decisions",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	client, err := newStoreClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx := context.Background()

	// Tasks summary
	pending, _ := client.CountTasks(ctx, "pending")
	inProgress, _ := client.CountTasks(ctx, "in_progress")
	blocked, _ := client.CountTasks(ctx, "blocked")
	done, _ := client.CountTasks(ctx, "done")

	fmt.Println("TASKS:")
	fmt.Printf("  ○ Pending: %d  → In Progress: %d  ⚠ Blocked: %d  ✓ Done: %d\n",
		pending, inProgress, blocked, done)

	// Show open tasks
	if pending+inProgress+blocked > 0 {
		tasks, err := client.ListTasks(ctx, "")
		if err == nil {
			for _, t := range tasks {
				if t.Status == "done" {
					continue
				}
				fmt.Printf("  %s %s [%s] %s\n", statusIcon(t.Status), t.ID, t.Priority, t.Title)
			}
		}
	}

	// Unread messages
	unread, _ := client.CountUnreadMessages(ctx, "")
	fmt.Printf("\nMESSAGES:\n  Unread: %d\n", unread)

	if unread > 0 {
		messages, err := client.GetInbox(ctx, "")
		if err == nil {
			for _, m := range messages {
				fmt.Printf("  [%s → %s] %s\n", m.FromRole, m.ToRole, m.Content)
			}
		}
		// Don't mark as read — status is read-only view
	}

	// Recent decisions
	decisions, err := client.ListDecisions(ctx, 5)
	if err == nil && len(decisions) > 0 {
		fmt.Println("\nRECENT DECISIONS:")
		for _, d := range decisions {
			fmt.Printf("  • %s\n", d.Text)
			if len(d.Tags) > 0 {
				fmt.Printf("    Tags: %s\n", strings.Join(d.Tags, ", "))
			}
		}
	}

	// Recent rejections
	rejections, err := client.ListRejections(ctx, 5)
	if err == nil && len(rejections) > 0 {
		fmt.Println("\nRECENT REJECTIONS:")
		for _, r := range rejections {
			fmt.Printf("  ✗ %s\n", r.Approach)
		}
	}

	return nil
}
