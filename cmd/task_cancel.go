package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
)

var taskCancelReason string

var taskCancelCmd = &cobra.Command{
	Use:   "cancel TASK-ID",
	Short: "Permanently remove an accidental or probe task",
	Long: `Remove a task that should never have existed — probes, accidents, duplicates.

DISTINCT FROM task done: cancel is "this task should not have existed", not
"this work is complete". It bypasses the verifier-separation gate because
there is no work to verify.

Removes the Qdrant point and the Memgraph Task node + all its edges.

--reason is required to prevent accidental use.`,
	Args: cobra.ExactArgs(1),
	RunE: runTaskCancel,
}

func init() {
	taskCancelCmd.Flags().StringVar(&taskCancelReason, "reason", "", "why this task is being cancelled (required)")
	_ = taskCancelCmd.MarkFlagRequired("reason")
	taskCmd.AddCommand(taskCancelCmd)
}

func runTaskCancel(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	reason, err := requireNonEmpty("reason", taskCancelReason)
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

	// Verify the task exists before attempting removal.
	if _, getErr := d.store.GetTask(ctx, taskID); getErr != nil {
		return notFound(fmt.Sprintf("task %s not found: %v", taskID, getErr))
	}

	if err := d.store.CancelTask(ctx, taskID, resolveAuthor(cmd), reason); err != nil {
		return fmt.Errorf("remove task from store: %w", err)
	}

	// Best-effort: remove the Task node + edges from Memgraph.
	deleteTaskGraphNode(cmd, ctx, taskID)

	notifyTaskLifecycle(ctx, d.store, taskID, "cancelled", reason)

	return printJSON(map[string]any{"id": taskID, "status": "cancelled", "reason": reason}, func() {
		fmt.Printf("⊘ %s cancelled: %s\n", taskID, reason)
	})
}

// deleteTaskGraphNode removes the Task node and all its edges from Memgraph.
// Non-fatal: Qdrant is the source of truth; Memgraph is a derived view.
func deleteTaskGraphNode(cmd *cobra.Command, ctx context.Context, taskID string) {
	cfg, err := config.Load()
	if err != nil || cfg.Memgraph.URI == "" {
		return
	}
	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph init failed (%v) — Task node not deleted for %s\n", err, taskID)
		return
	}
	defer func() { _ = gc.Close(ctx) }()

	if dErr := gc.DeleteTaskNode(ctx, taskID); dErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph Task node delete failed for %s: %v\n", taskID, dErr)
	}
}
