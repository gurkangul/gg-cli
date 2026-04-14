package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var bugFixCmd = &cobra.Command{
	Use:   `fix BUG-ID "summary"`,
	Short: "Mark a bug as fixed",
	Args:  cobra.ExactArgs(2),
	RunE:  runBugFix,
}

var bugStartCmd = &cobra.Command{
	Use:   "start BUG-ID",
	Short: "Move a bug to 'fixing' status",
	Args:  cobra.ExactArgs(1),
	RunE:  runBugStart,
}

var bugWontFixCmd = &cobra.Command{
	Use:   `wontfix BUG-ID "reason"`,
	Short: "Close a bug as won't-fix",
	Args:  cobra.ExactArgs(2),
	RunE:  runBugWontFix,
}

var bugRootCause string

func init() {
	bugFixCmd.Flags().StringVar(&bugRootCause, "root-cause", "", "root cause identified during fix")
	bugCmd.AddCommand(bugFixCmd)
	bugCmd.AddCommand(bugStartCmd)
	bugCmd.AddCommand(bugWontFixCmd)
}

func runBugFix(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
	if err != nil {
		return err
	}
	summary, err := requireNonEmpty("summary", args[1])
	if err != nil {
		return err
	}
	rootCause, err := requireNonEmpty("--root-cause", bugRootCause)
	if err != nil {
		return fmt.Errorf("%w (root cause must be the underlying defect, not the symptom — required to close a bug)", err)
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.FixBug(ctx, bugID, rootCause, summary); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": bugID, "status": "fixed", "summary": summary, "root_cause": rootCause}, func() {
		fmt.Printf("✓ %s marked as fixed\n", bugID)
		fmt.Printf("  Root cause: %s\n", rootCause)
	})
}

func runBugStart(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
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

	if err := d.store.StartFixingBug(ctx, bugID); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": bugID, "status": "fixing"}, func() {
		fmt.Printf("→ %s marked as fixing\n", bugID)
	})
}

func runBugWontFix(cmd *cobra.Command, args []string) error {
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

	if err := d.store.WontFixBug(ctx, bugID, reason); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": bugID, "status": "wontfix", "reason": reason}, func() {
		fmt.Printf("– %s marked as wontfix\n", bugID)
	})
}
