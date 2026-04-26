package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/store"
)

var (
	spawnAdvanceTaskID    string
	spawnAdvanceCommitSHA string
)

var spawnAdvanceCmd = &cobra.Command{
	Use:   "advance --task TASK-NNN [--commit <sha>]",
	Short: "Write a worker-ready sentinel for a task",
	Long: `Signal the master heartbeat loop that this worker has committed and is
awaiting review. Writes (or overwrites) a JSON sentinel at:

  ~/.gg/projects/<project_id>/spawn/advance/TASK-NNN.done

The sentinel records {task_id, surface_id, commit_sha, written_at}. The
master's heartbeat --watch loop polls this directory and transitions the
pane to state=ready when it finds the sentinel.

Idempotent: safe to call on amend — the sentinel is simply overwritten with
the new commit SHA.

Typical usage after commit:

  git commit -m "..." && gg spawn advance --task TASK-NNN --commit $(git rev-parse HEAD)`,
	RunE: runSpawnAdvance,
}

func init() {
	spawnAdvanceCmd.Flags().StringVar(&spawnAdvanceTaskID, "task", "", "task ID this worker has completed (e.g. TASK-042)")
	spawnAdvanceCmd.Flags().StringVar(&spawnAdvanceCommitSHA, "commit", "", "commit SHA to record in the sentinel (optional)")
	_ = spawnAdvanceCmd.MarkFlagRequired("task")
	spawnCmd.AddCommand(spawnAdvanceCmd)
}

func runSpawnAdvance(cmd *cobra.Command, _ []string) error {
	taskID := spawnAdvanceTaskID
	if _, err := store.ParseTaskID(taskID); err != nil {
		return fmt.Errorf("--task: %w", err)
	}

	surfaceID := os.Getenv("GG_SURFACE_ID")

	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	if err := spawn.WriteAdvanceSentinel(rt, taskID, surfaceID, spawnAdvanceCommitSHA); err != nil {
		return fmt.Errorf("write advance sentinel: %w", err)
	}

	return printJSON(map[string]any{
		"task_id":    taskID,
		"surface_id": surfaceID,
		"commit_sha": spawnAdvanceCommitSHA,
		"status":     "sentinel written",
	}, func() {
		if spawnAdvanceCommitSHA != "" {
			fmt.Printf("✓ Advance sentinel written for %s (commit: %s)\n", taskID, spawnAdvanceCommitSHA)
		} else {
			fmt.Printf("✓ Advance sentinel written for %s\n", taskID)
		}
	})
}
