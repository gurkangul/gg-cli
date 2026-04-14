package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Pre-push health check — open tasks, unresolved discussions",
	Long: `Run a quick health check before pushing code.

Reports:
  - Open (pending/blocked) tasks that may be relevant to the current branch
  - Unresolved discussions that need a decision before merging

Exit codes:
  0  all clear
  1  one or more issues found (blocks push when used as a git hook)

Usage as a git pre-push hook:
  echo "gg check" >> .git/hooks/pre-push
  chmod +x .git/hooks/pre-push`,
	Args: cobra.NoArgs,
	RunE: runCheck,
}

func init() {
	rootCmd.AddCommand(checkCmd)
}

func runCheck(cmd *cobra.Command, _ []string) error {
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	issues := 0

	// ── Open tasks ────────────────────────────────────────────────────────────
	pending, pendErr := d.store.CountTasks(ctx, "pending")
	blocked, blocErr := d.store.CountTasks(ctx, "blocked")

	if pendErr != nil || blocErr != nil {
		fmt.Println("  ~ tasks: could not query (Qdrant unreachable?)")
	} else {
		total := pending + blocked
		if total > 0 {
			issues++
			fmt.Printf("  ✗ %-30s %d open (%d pending, %d blocked)\n",
				"open tasks", total, pending, blocked)
		} else {
			fmt.Printf("  ✓ %-30s all done\n", "open tasks")
		}
	}

	// ── Unresolved discussions ────────────────────────────────────────────────
	openDisc, discErr := d.store.CountOpenDiscussions(ctx)
	if discErr != nil {
		fmt.Println("  ~ discussions: could not query")
	} else if openDisc > 0 {
		issues++
		fmt.Printf("  ✗ %-30s %d unresolved\n", "open discussions", openDisc)
	} else {
		fmt.Printf("  ✓ %-30s none\n", "open discussions")
	}

	fmt.Println(strings.Repeat("─", 50))

	if issues == 0 {
		fmt.Println("All clear — safe to push.")
		return nil
	}
	return fmt.Errorf("%d issue(s) — review the above before pushing", issues)
}
