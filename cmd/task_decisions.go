package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

var taskDecisionsCmd = &cobra.Command{
	Use:   "decisions TASK-ID",
	Short: "List decisions explicitly linked to a task",
	Long: `List decisions whose --task flag named this task exactly.

This is a structural lookup, not a semantic search — it returns only decisions
where Decision.TaskID == <TASK-ID>. Use it to prove that a task's design
choices were captured with gg record --task <TASK-ID>.

Consumed by the decision-capture gate (hooks/pre-task-done.d/20-decide-capture.sh).`,
	Args: cobra.ExactArgs(1),
	RunE: runTaskDecisions,
}

func init() {
	taskCmd.AddCommand(taskDecisionsCmd)
}

func runTaskDecisions(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
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

	decisions, err := d.store.ListDecisionsByTaskID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("list decisions for %s: %w", taskID, err)
	}

	renderDecisions := func(w io.Writer) {
		if len(decisions) == 0 {
			fmt.Fprintf(w, "(no decisions linked to %s — record one with `gg record \"...\" --task %s`)\n", taskID, taskID)
			return
		}
		for _, dec := range decisions {
			fmt.Fprintf(w, "D %s  %s\n", shortDate(dec.CreatedAt), compactTrim(dec.Text, compactLineWidth))
			if dec.Reason != "" {
				fmt.Fprintf(w, "    reason: %s\n", compactTrim(dec.Reason, compactLineWidth))
			}
		}
	}
	// TASK-491: `gg decisions` is a discretionary agent lookup (no explicit
	// --full gate path), so it counts toward the discretionary drop-list signal.
	emitHydration(cmd, "decisions", false, renderDecisions)
	return printJSON(decisions, func() { renderDecisions(os.Stdout) })
}
