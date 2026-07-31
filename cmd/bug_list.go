package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/projectstate"
	"github.com/gurkangul/gg-cli/internal/store"
)

var bugListCmd = &cobra.Command{
	Use:   "list",
	Short: "List bugs",
	Args:  cobra.NoArgs,
	RunE:  runBugList,
}

var bugGetCmd = &cobra.Command{
	Use: "get BUG-ID",
	// BUG-093: `show` is the natural verb agents reach for; alias it to get so
	// `gg bug show BUG-1` works instead of falling through to help.
	Aliases: []string{"show"},
	Short:   "Show bug details",
	Args:    cobra.ExactArgs(1),
	RunE:    runBugGet,
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
				compactRendererV_bugList,
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
	// BUG-074: only a full (non-compact) read proves hydration. Compact drops
	// reasons/details, so it must not satisfy the hydration gate.
	if !isCompactActive(cmd) {
		recordBugFullHydration(b.ID)
	}

	renderBugGet := func(w io.Writer) {
		fmt.Fprintf(w, "%s %s [%s/%s] %s\n", bugStatusIcon(b.Status), b.ID, b.Severity, b.Status, b.Title)
		if b.Detail != "" {
			fmt.Fprintf(w, "  Detail: %s\n", b.Detail)
		}
		if len(b.Tags) > 0 {
			fmt.Fprintf(w, "  Tags: %s\n", strings.Join(b.Tags, ", "))
		}
		if b.TaskID != "" {
			fmt.Fprintf(w, "  Task: %s\n", b.TaskID)
		}
		if b.RootCause != "" {
			fmt.Fprintf(w, "  Root cause: %s\n", b.RootCause)
		}
		if b.FixSummary != "" {
			fmt.Fprintf(w, "  Fix: %s\n", b.FixSummary)
		}
		// BUG-106: the reporter (By) and the fixer are different people, and the
		// fix used to inherit the reporter's name silently.
		if b.Status == "fixed" || b.Status == "wontfix" {
			fmt.Fprintf(w, "  Fixed by: %s\n", authorLabel(b.FixedBy))
		}
		if b.ReproScript != "" {
			fmt.Fprintf(w, "  Repro: %s\n", b.ReproScript)
		}
		fmt.Fprintf(w, "  Created: %s\n", shortDate(b.CreatedAt))
		fmt.Fprintf(w, "  Updated: %s\n", shortDate(b.UpdatedAt))
	}
	// TASK-491: a non-compact `bug get` is the bug-fix gate / triage pre-flight
	// full read (bug get has no discretionary --full toggle; --compact is the
	// only opt-out, and we already skipped recordBugFullHydration under compact).
	// Tag it gate-mandated so it does not feed the discretionary drop-list-risk
	// signal — it is the FIRST full read, not a re-fetch after a compact view.
	emitHydration(cmd, "bug", true, renderBugGet)
	return printJSON(b, func() { renderBugGet(os.Stdout) })
}

func recordBugFullHydration(bugID string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ could not record bug hydration proof: %v\n", err)
		return
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ could not record bug hydration proof: %v\n", err)
		return
	}
	if err := projectstate.RecordHydration(runtimeDir, "bug", bugID); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ could not record bug hydration proof: %v\n", err)
	}
}
