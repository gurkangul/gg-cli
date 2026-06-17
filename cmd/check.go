package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/outbox"
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
	OpenBugs        uint64 `json:"open_bugs"`
	ReopenedBugs    uint64 `json:"reopened_bugs"`
	OutboxPending   int    `json:"outbox_pending"`
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

	// ── Open + reopened bugs (defect pressure before push) ─────────────────
	openBugs, obErr := d.store.CountBugs(ctx, "open")
	fixingBugs, fxErr := d.store.CountBugs(ctx, "fixing")
	reopenedBugs, rbErr := d.store.CountBugs(ctx, "reopened")
	if obErr == nil && fxErr == nil && rbErr == nil {
		result.OpenBugs = openBugs + fixingBugs
		result.ReopenedBugs = reopenedBugs
		if result.OpenBugs > 0 || result.ReopenedBugs > 0 {
			result.Issues++
		}
	}

	// ── Outbox backlog (crashed-index writes waiting for reconcile) ─────────
	var obxErr error
	if ggDir, ggErr := config.GGDir(); ggErr == nil {
		if entries, listErr := outbox.List(ggDir); listErr == nil {
			result.OutboxPending = len(entries)
			if result.OutboxPending > 0 {
				result.Issues++
			}
		} else {
			obxErr = listErr
		}
	}

	if result.Issues == 0 {
		result.Status = "ok"
	} else {
		result.Status = "issues"
	}

	err = printJSON(result, func() { //nolint:gocritic // ifElseChain: branches check distinct variables, switch is not applicable
		switch {
		case pendErr != nil || blocErr != nil:
			fmt.Fprintln(os.Stderr, "warning: could not query tasks (vector store unavailable?)")
		case result.OpenTasks > 0:
			fmt.Fprintf(os.Stderr, "warn  %-30s %d open (%d pending, %d blocked)\n",
				"open tasks", result.OpenTasks, result.PendingTasks, result.BlockedTasks)
		default:
			fmt.Printf("  ✓ %-30s all done\n", "open tasks")
		}

		switch {
		case discErr != nil:
			fmt.Fprintln(os.Stderr, "warning: could not query discussions")
		case result.OpenDiscussions > 0:
			fmt.Fprintf(os.Stderr, "warn  %-30s %d unresolved\n", "open discussions", result.OpenDiscussions)
		default:
			fmt.Printf("  ✓ %-30s none\n", "open discussions")
		}

		switch {
		case obErr != nil || fxErr != nil || rbErr != nil:
			fmt.Fprintln(os.Stderr, "warning: could not query bugs")
		case result.OpenBugs > 0 || result.ReopenedBugs > 0:
			fmt.Fprintf(os.Stderr, "warn  %-30s %d open, %d reopened\n", "bugs", result.OpenBugs, result.ReopenedBugs)
		default:
			fmt.Printf("  ✓ %-30s none\n", "open bugs")
		}

		switch {
		case obxErr != nil:
			fmt.Fprintln(os.Stderr, "warning: could not read outbox")
		case result.OutboxPending > 0:
			fmt.Fprintf(os.Stderr, "warn  %-30s %d pending (run `gg doctor --reconcile`)\n", "outbox", result.OutboxPending)
		default:
			fmt.Printf("  ✓ %-30s empty\n", "outbox")
		}

		fmt.Println(strings.Repeat("─", 50))

		switch {
		case result.Issues == 0:
			fmt.Println("All clear — safe to push.")
		case checkStrict:
			fmt.Fprintf(os.Stderr, "%d issue(s) found — review before pushing (--strict mode)\n", result.Issues)
		default:
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
