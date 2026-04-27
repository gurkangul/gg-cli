package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
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

When the master heartbeat includes a terminal surface, this command also
sends a best-effort wake prompt to the master pane immediately. The polling
loop remains the fallback for disconnected or stuck workers.

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

	wakeStatus := wakeMasterAfterAdvance(cmd, rt, taskID, surfaceID, spawnAdvanceCommitSHA)

	return printJSON(map[string]any{
		"task_id":    taskID,
		"surface_id": surfaceID,
		"commit_sha": spawnAdvanceCommitSHA,
		"status":     "sentinel written",
		"wake":       wakeStatus,
	}, func() {
		if spawnAdvanceCommitSHA != "" {
			fmt.Printf("✓ Advance sentinel written for %s (commit: %s)\n", taskID, spawnAdvanceCommitSHA)
		} else {
			fmt.Printf("✓ Advance sentinel written for %s\n", taskID)
		}
	})
}

func wakeMasterAfterAdvance(cmd *cobra.Command, rt, taskID, workerSurfaceID, commitSHA string) string {
	hb, err := spawn.ReadHeartbeat(rt)
	if err != nil || hb == nil || strings.TrimSpace(hb.SurfaceID) == "" {
		return "skipped: no master surface"
	}
	if hb.SurfaceID == workerSurfaceID {
		return "skipped: master surface matches worker"
	}

	term, err := terminal.NewFromEnv()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ master wake skipped: terminal backend unavailable: %v\n", err)
		return "skipped: terminal backend unavailable"
	}

	commitRef := strings.TrimSpace(commitSHA)
	if commitRef == "" {
		commitRef = "(no sha)"
	}
	prompt := fmt.Sprintf("Worker ready for review: %s at %s. Review the commit and worker pane before closing the task.", taskID, commitRef)
	if workerSurfaceID != "" {
		prompt = fmt.Sprintf("%s Worker pane: %s.", prompt, workerSurfaceID)
	}

	spawnDir := spawn.Dir(rt)
	if err := terminal.WakeAndSendWithFlock(cmd.Context(), term, terminal.SurfaceID(hb.SurfaceID), prompt, spawnDir); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ master wake failed for %s: %v\n", hb.SurfaceID, err)
		return "failed"
	}
	return "sent"
}
