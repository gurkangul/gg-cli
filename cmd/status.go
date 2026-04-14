package cmd

import (
	"fmt"
	"os"
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

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// Tasks summary — fail fast if the first call errors (likely Qdrant down).
	pending, err := d.store.CountTasks(ctx, "pending")
	if err != nil {
		return fmt.Errorf("count tasks: %w", err)
	}
	inProgress, ipErr := d.store.CountTasks(ctx, "in_progress")
	blocked, blErr := d.store.CountTasks(ctx, "blocked")
	done, doErr := d.store.CountTasks(ctx, "done")

	fmt.Println("TASKS:")
	fmt.Printf("  ○ Pending: %s  → In Progress: %s  ⚠ Blocked: %s  ✓ Done: %s\n",
		fmtCount(pending, nil),
		fmtCount(inProgress, ipErr),
		fmtCount(blocked, blErr),
		fmtCount(done, doErr),
	)
	warnIf(ipErr, blErr, doErr)

	if pending+inProgress+blocked > 0 {
		tasks, err := d.store.ListTasks(ctx, "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "warning: list tasks:", err)
		} else {
			for _, t := range tasks {
				if t.Status == "done" {
					continue
				}
				fmt.Printf("  %s %s [%s] %s\n", statusIcon(t.Status), t.ID, t.Priority, t.Title)
			}
		}
	}

	// Unread messages
	unread, err := d.store.CountUnreadMessages(ctx, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: count unread:", err)
	} else {
		fmt.Printf("\nMESSAGES:\n  Unread: %d\n", unread)
		if unread > 0 {
			messages, err := d.store.GetInbox(ctx, "")
			if err != nil {
				fmt.Fprintln(os.Stderr, "warning: get inbox:", err)
			} else {
				for _, m := range messages {
					fmt.Printf("  [%s → %s] %s\n", m.FromRole, m.ToRole, m.Content)
				}
				fmt.Println("  (run `gg inbox` to mark as read)")
			}
		}
	}

	// Recent decisions
	decisions, err := d.store.ListDecisions(ctx, 5)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: list decisions:", err)
	} else if len(decisions) > 0 {
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
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: list rejections:", err)
	} else if len(rejections) > 0 {
		fmt.Println("\nRECENT REJECTIONS:")
		for _, r := range rejections {
			fmt.Printf("  ✗ %s\n", r.Approach)
		}
	}

	return nil
}

func fmtCount(n uint64, err error) string {
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%d", n)
}

func warnIf(errs ...error) {
	for _, e := range errs {
		if e != nil {
			fmt.Fprintln(os.Stderr, "warning: count tasks:", e)
			return
		}
	}
}
