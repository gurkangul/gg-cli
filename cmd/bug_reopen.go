package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

var bugReopenCmd = &cobra.Command{
	Use:   `reopen BUG-ID "reason"`,
	Short: "Reopen a fixed or wontfix bug",
	Long: `Transition a fixed or wontfix bug back to "reopened" status.

The reason is appended to the bug's reopen_reasons history and the
reopen_count is incremented. Use this when a fix regressed or a wontfix
decision is reversed.

The auto-reopen trigger (scan-refs) calls this automatically when a commit
message references a fixed bug.`,
	Args: cobra.ExactArgs(2),
	RunE: runBugReopen,
}

var bugScanRefsCmd = &cobra.Command{
	Use:   `scan-refs "text"`,
	Short: "Scan text for BUG-NNN references and auto-reopen any that are fixed",
	Long: `Parse text (e.g. a commit message) for BUG-NNN patterns. For each
referenced bug that is currently in "fixed" status, automatically reopen it.

Intended to be called from git commit-msg hooks or the pre-task-done hook so
that a commit touching a previously-fixed area triggers a reopen before the
task is marked done.

Exit code 0 even when bugs are reopened — check stdout for the list of
affected bug IDs.`,
	Args: cobra.ExactArgs(1),
	RunE: runBugScanRefs,
}

var bugReopenSilent bool

func init() {
	bugScanRefsCmd.Flags().BoolVar(&bugReopenSilent, "silent", false, "suppress output when no bugs are reopened")
	bugCmd.AddCommand(bugReopenCmd)
	bugCmd.AddCommand(bugScanRefsCmd)
}

func runBugReopen(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
	if err != nil {
		return err
	}
	reason, err := requireNonEmpty("reason", args[1])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := enforceBugHydrationGate(cmd.ErrOrStderr(), nil, bugID, "bug reopen", "compact-hydration-bug-reopen"); err != nil {
		return err
	}
	if err := d.store.ReopenBug(ctx, bugID, reason); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": bugID, "status": "reopened", "reason": reason}, func() {
		fmt.Printf("↺ %s reopened\n", bugID)
		fmt.Printf("  Reason: %s\n", reason)
	})
}

var bugRefPattern = regexp.MustCompile(`\bBUG-\d{3,}\b`)

func runBugScanRefs(cmd *cobra.Command, args []string) error {
	text := args[0]
	matches := bugRefPattern.FindAllString(strings.ToUpper(text), -1)
	if len(matches) == 0 {
		if !bugReopenSilent {
			fmt.Println("no BUG-NNN references found")
		}
		return nil
	}

	// Deduplicate while preserving order.
	seen := make(map[string]bool)
	var ids []string
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			ids = append(ids, m)
		}
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	var reopened []string
	var skipped []string

	for _, id := range ids {
		bug, getErr := d.store.GetBug(ctx, id)
		if getErr != nil {
			skipped = append(skipped, fmt.Sprintf("%s (not found)", id))
			continue
		}
		if bug.Status != "fixed" {
			skipped = append(skipped, fmt.Sprintf("%s (%s)", id, bug.Status))
			continue
		}
		reason := fmt.Sprintf("referenced in commit after being marked fixed: %.100s", text)
		if reopenErr := d.store.ReopenBug(ctx, id, reason); reopenErr != nil {
			return fmt.Errorf("reopen %s: %w", id, reopenErr)
		}
		reopened = append(reopened, id)
	}

	if len(reopened) == 0 && bugReopenSilent {
		return nil
	}

	return printJSON(map[string]any{"reopened": reopened, "skipped": skipped}, func() {
		if len(reopened) > 0 {
			fmt.Printf("↺ reopened: %s\n", strings.Join(reopened, ", "))
		}
		if len(skipped) > 0 {
			fmt.Printf("~ skipped:  %s\n", strings.Join(skipped, ", "))
		}
		if len(reopened) == 0 {
			fmt.Println("no fixed bugs to reopen")
		}
	})
}
