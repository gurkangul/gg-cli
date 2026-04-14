package cmd

import (
	"fmt"
	"os"
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

Exit codes (default / warn mode):
  0  always — issues are printed to stderr as warnings

Exit codes (--strict mode):
  0  all clear
  1  one or more issues found

Usage as a git pre-push hook (strict mode):
  echo "gg check --strict" >> .git/hooks/pre-push
  chmod +x .git/hooks/pre-push

Agent/pipeline usage (non-blocking, machine-readable):
  gg check --json`,
	Args: cobra.NoArgs,
	RunE: runCheck,
}

var checkStrict bool

func init() {
	checkCmd.Flags().BoolVar(&checkStrict, "strict", false, "exit 1 when issues are found (for git hooks)")
	rootCmd.AddCommand(checkCmd)
}

type checkResult struct {
	OpenTasks       uint64 `json:"open_tasks"`
	PendingTasks    uint64 `json:"pending_tasks"`
	BlockedTasks    uint64 `json:"blocked_tasks"`
	OpenDiscussions uint64 `json:"open_discussions"`
	Issues          int    `json:"issues"`
	Status          string `json:"status"` // "ok" or "issues"
}

func runCheck(cmd *cobra.Command, _ []string) error {
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	result := checkResult{}

	// ── Open tasks ──────────────────────────────────────────────────────────
	pending, pendErr := d.store.CountTasks(ctx, "pending")
	blocked, blocErr := d.store.CountTasks(ctx, "blocked")

	if pendErr == nil && blocErr == nil {
		result.PendingTasks = pending
		result.BlockedTasks = blocked
		result.OpenTasks = pending + blocked
		if result.OpenTasks > 0 {
			result.Issues++
		}
	}

	// ── Unresolved discussions ───────────────────────────────────────────────
	openDisc, discErr := d.store.CountOpenDiscussions(ctx)
	if discErr == nil {
		result.OpenDiscussions = openDisc
		if openDisc > 0 {
			result.Issues++
		}
	}

	if result.Issues == 0 {
		result.Status = "ok"
	} else {
		result.Status = "issues"
	}

	err = printJSON(result, func() {
		if pendErr != nil || blocErr != nil {
			fmt.Fprintln(os.Stderr, "warning: could not query tasks (Qdrant unreachable?)")
		} else if result.OpenTasks > 0 {
			fmt.Fprintf(os.Stderr, "warn  %-30s %d open (%d pending, %d blocked)\n",
				"open tasks", result.OpenTasks, result.PendingTasks, result.BlockedTasks)
		} else {
			fmt.Printf("  ✓ %-30s all done\n", "open tasks")
		}

		if discErr != nil {
			fmt.Fprintln(os.Stderr, "warning: could not query discussions")
		} else if result.OpenDiscussions > 0 {
			fmt.Fprintf(os.Stderr, "warn  %-30s %d unresolved\n", "open discussions", result.OpenDiscussions)
		} else {
			fmt.Printf("  ✓ %-30s none\n", "open discussions")
		}

		fmt.Println(strings.Repeat("─", 50))

		if result.Issues == 0 {
			fmt.Println("All clear — safe to push.")
		} else if checkStrict {
			fmt.Fprintf(os.Stderr, "%d issue(s) found — review before pushing (--strict mode)\n", result.Issues)
		} else {
			fmt.Fprintf(os.Stderr, "%d issue(s) found (use --strict to block push)\n", result.Issues)
		}
	})
	if err != nil {
		return err
	}

	if checkStrict && result.Issues > 0 {
		return fmt.Errorf("%d issue(s) — review the above before pushing", result.Issues)
	}
	return nil
}
