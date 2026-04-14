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

	// Tasks summary — each sub-count is allowed to fail independently with "?"
	// so the user sees whatever is available when Qdrant is partially degraded.
	counts := map[string]struct {
		n   uint64
		err error
	}{}
	for _, s := range []string{"pending", "in_progress", "blocked", "done"} {
		n, err := d.store.CountTasks(ctx, s)
		counts[s] = struct {
			n   uint64
			err error
		}{n, err}
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: count tasks (%s): %v\n", s, err)
		}
	}

	fmt.Println("TASKS:")
	fmt.Printf("  ○ Pending: %s  → In Progress: %s  ⚠ Blocked: %s  ✓ Done: %s\n",
		fmtCount(counts["pending"].n, counts["pending"].err),
		fmtCount(counts["in_progress"].n, counts["in_progress"].err),
		fmtCount(counts["blocked"].n, counts["blocked"].err),
		fmtCount(counts["done"].n, counts["done"].err),
	)

	hasOpen := counts["pending"].n+counts["in_progress"].n+counts["blocked"].n > 0
	if hasOpen {
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

	// Messages — fetch once and derive the count from the result so we can't
	// race with a concurrent `gg inbox` between Count and Scroll.
	messages, err := d.store.GetInbox(ctx, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: get inbox:", err)
	} else {
		fmt.Printf("\nMESSAGES:\n  Unread: %d\n", len(messages))
		for _, m := range messages {
			fmt.Printf("  [%s → %s] %s\n", m.FromRole, m.ToRole, m.Content)
		}
		if len(messages) > 0 {
			fmt.Println("  (run `gg inbox` to mark as read)")
		}
	}

	// Open discussions — unresolved topics the next agent must close.
	openDiscs, err := d.store.ListDiscussions(ctx, "open")
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: list discussions:", err)
	} else if len(openDiscs) > 0 {
		fmt.Printf("\nOPEN DISCUSSIONS (%d — resolve or dismiss before closing session):\n", len(openDiscs))
		for _, disc := range openDiscs {
			fmt.Printf("  • %s — %s\n", disc.ID, disc.Topic)
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
