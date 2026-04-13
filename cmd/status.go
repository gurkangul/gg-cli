package cmd

import (
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
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := cmdContext()
	defer cancel()

	// Tasks summary
	pending, err := d.store.CountTasks(ctx, "pending")
	if err != nil {
		return fmt.Errorf("count tasks: %w", err)
	}
	inProgress, _ := d.store.CountTasks(ctx, "in_progress")
	blocked, _ := d.store.CountTasks(ctx, "blocked")
	done, _ := d.store.CountTasks(ctx, "done")

	fmt.Println("TASKS:")
	fmt.Printf("  ○ Pending: %d  → In Progress: %d  ⚠ Blocked: %d  ✓ Done: %d\n",
		pending, inProgress, blocked, done)

	if pending+inProgress+blocked > 0 {
		tasks, err := d.store.ListTasks(ctx, "")
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
	unread, _ := d.store.CountUnreadMessages(ctx, "")
	fmt.Printf("\nMESSAGES:\n  Unread: %d\n", unread)

	if unread > 0 {
		messages, err := d.store.GetInbox(ctx, "")
		if err == nil {
			for _, m := range messages {
				fmt.Printf("  [%s → %s] %s\n", m.FromRole, m.ToRole, m.Content)
			}
		}
	}

	// Recent decisions
	decisions, err := d.store.ListDecisions(ctx, 5)
	if err == nil && len(decisions) > 0 {
		fmt.Println("\nRECENT DECISIONS:")
		for _, dec := range decisions {
			fmt.Printf("  • %s\n", dec.Text)
			if len(dec.Tags) > 0 {
				fmt.Printf("    Tags: %s\n", strings.Join(dec.Tags, ", "))
			}
		}
	}

	// Recent rejections
	rejections, err := d.store.ListRejections(ctx, 5)
	if err == nil && len(rejections) > 0 {
		fmt.Println("\nRECENT REJECTIONS:")
		for _, r := range rejections {
			fmt.Printf("  ✗ %s\n", r.Approach)
		}
	}

	return nil
}
