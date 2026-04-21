package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

var bugListCmd = &cobra.Command{
	Use:   "list",
	Short: "List bugs",
	Args:  cobra.NoArgs,
	RunE:  runBugList,
}

var bugGetCmd = &cobra.Command{
	Use:   "get BUG-ID",
	Short: "Show bug details",
	Args:  cobra.ExactArgs(1),
	RunE:  runBugGet,
}

var (
	bugListStatus  string
	bugListCompact bool
)

func init() {
	bugListCmd.Flags().StringVar(&bugListStatus, "status", "", "filter by status: open, fixing, fixed, wontfix")
	bugListCmd.Flags().BoolVar(&bugListCompact, "compact", false, "one line per bug — drops fix-summary to preserve agent context window")
	bugCmd.AddCommand(bugListCmd)
	bugCmd.AddCommand(bugGetCmd)
}

func runBugList(cmd *cobra.Command, _ []string) error {
	if bugListStatus != "" && !validBugStatuses[bugListStatus] {
		return fmt.Errorf("invalid status %q — use open, fixing, fixed, or wontfix", bugListStatus)
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	bugs, err := d.store.ListBugs(ctx, bugListStatus)
	if err != nil {
		return fmt.Errorf("list bugs: %w", err)
	}

	return printJSON(bugs, func() {
		if len(bugs) == 0 {
			fmt.Println("No bugs found.")
			return
		}
		if isCompactActive(cmd) {
			emitCompact(cmd, "list",
				func(w io.Writer) { renderBugListDefault(w, bugs) },
				func(w io.Writer) { writeCompactBugs(w, bugs) },
			)
			return
		}
		renderBugListDefault(os.Stdout, bugs)
	})
}

func renderBugListDefault(w io.Writer, bugs []store.Bug) {
	for _, b := range bugs {
		fmt.Fprintf(w, "%s %s [%s/%s] %s\n", bugStatusIcon(b.Status), b.ID, b.Severity, b.Status, b.Title)
		if b.Status == "fixed" && b.FixSummary != "" {
			fmt.Fprintf(w, "    ✓ Fix: %s\n", b.FixSummary)
		}
	}
}

func runBugGet(cmd *cobra.Command, args []string) error {
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

	b, err := d.store.GetBug(ctx, bugID)
	if err != nil {
		return notFound(err.Error())
	}

	return printJSON(b, func() {
		fmt.Printf("%s %s [%s/%s] %s\n", bugStatusIcon(b.Status), b.ID, b.Severity, b.Status, b.Title)
		if b.Detail != "" {
			fmt.Printf("  Detail: %s\n", b.Detail)
		}
		if len(b.Tags) > 0 {
			fmt.Printf("  Tags: %s\n", strings.Join(b.Tags, ", "))
		}
		if b.TaskID != "" {
			fmt.Printf("  Task: %s\n", b.TaskID)
		}
		if b.RootCause != "" {
			fmt.Printf("  Root cause: %s\n", b.RootCause)
		}
		if b.FixSummary != "" {
			fmt.Printf("  Fix: %s\n", b.FixSummary)
		}
		if b.ReproScript != "" {
			fmt.Printf("  Repro: %s\n", b.ReproScript)
		}
		fmt.Printf("  Created: %s\n", shortDate(b.CreatedAt))
		fmt.Printf("  Updated: %s\n", shortDate(b.UpdatedAt))
	})
}
