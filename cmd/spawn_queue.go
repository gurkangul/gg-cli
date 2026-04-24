package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
	"github.com/gurkangul/gg-cli/internal/store"
)

var spawnQueueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Sequential queue runner: drain pending tasks by spawning workers one-at-a-time",
	Long: `Drain the pending task queue by spawning a worker pane for each task.

Workers run sequentially (MaxConcurrent=1 for this command; parallel workers
are planned in TASK-276). For each pending task the runner:
  1. Verifies master liveness (heartbeat must be fresh)
  2. Opens a worker pane via the terminal backend (GG_TERMINAL)
  3. Sends the task ID to the worker for context loading
  4. Waits for the worker pane to exit
  5. Advances to the next pending task

The queue respects dependency ordering: tasks whose dependencies are not yet
done are skipped (they appear in the 'skipped' counter in 'gg spawn status').

Use --resume to re-attach to a stalled session. The queue state is persisted
under ~/.gg/projects/<id>/spawn/session.json.

Exit conditions:
  - All pending tasks processed (success)
  - --max-tasks limit reached
  - Master heartbeat goes stale (master session appears dead)
  - Ctrl+C (interrupt)`,
	RunE: runSpawnQueue,
}

var (
	spawnQueueAgent     string
	spawnQueueResume    bool
	spawnQueueMaxTasks  int
	spawnQueuePollSecs  int
	spawnQueueSkipDone  bool
)

func init() {
	spawnQueueCmd.Flags().StringVar(&spawnQueueAgent, "agent", "", "agent command for worker panes (default: $GG_SPAWN_AGENT or 'claude')")
	spawnQueueCmd.Flags().BoolVar(&spawnQueueResume, "resume", false, "resume a stalled session (keep completed/skipped state)")
	spawnQueueCmd.Flags().IntVar(&spawnQueueMaxTasks, "max-tasks", 0, "stop after processing this many tasks (0 = no limit)")
	spawnQueueCmd.Flags().IntVar(&spawnQueuePollSecs, "poll", 30, "seconds between liveness checks while a worker is running")
	spawnQueueCmd.Flags().BoolVar(&spawnQueueSkipDone, "skip-done", true, "skip tasks already in 'done' state (default true)")
	spawnCmd.AddCommand(spawnQueueCmd)
}

func runSpawnQueue(cmd *cobra.Command, _ []string) error {
	agentCmd := spawnQueueAgent
	if agentCmd == "" {
		agentCmd = spawnAgentDefault()
	}

	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	// Write initial heartbeat to confirm the master is alive at queue start.
	masterAgent := os.Getenv("GG_AGENT")
	if masterAgent == "" {
		masterAgent = os.Getenv("GG_ROLE")
	}
	if hbErr := spawn.WriteHeartbeat(rt, masterAgent); hbErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ heartbeat write failed: %v\n", hbErr)
	}

	// Load or create queue session.
	sess, err := loadOrCreateSession(rt, agentCmd)
	if err != nil {
		return err
	}

	// Load terminal backend.
	term, err := terminal.NewFromEnv()
	if err != nil {
		return fmt.Errorf("terminal backend: %w", err)
	}

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx := cmd.Context()
	processed := 0

	fmt.Printf("→ Queue runner started (agent: %s)\n", agentCmd)
	if spawnQueueResume {
		fmt.Printf("  Resuming — completed: %d  skipped: %d\n",
			len(sess.Completed), len(sess.Skipped))
	}

	for {
		// Check context cancellation (Ctrl+C).
		if ctx.Err() != nil {
			fmt.Println("\n⚠ Interrupted — queue paused. Use --resume to continue.")
			return nil
		}

		// Enforce max-tasks limit.
		if spawnQueueMaxTasks > 0 && processed >= spawnQueueMaxTasks {
			fmt.Printf("✓ Reached --max-tasks=%d limit. Queue paused.\n", spawnQueueMaxTasks)
			return nil
		}

		// Refresh heartbeat before each task (proves master is still active).
		if hbErr := spawn.WriteHeartbeat(rt, masterAgent); hbErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ heartbeat refresh failed: %v\n", hbErr)
		}

		// Fetch the next ready task from the store.
		task, err := nextReadyTask(ctx, d.store, sess)
		if err != nil {
			return fmt.Errorf("fetch next task: %w", err)
		}
		if task == nil {
			fmt.Println("✓ Queue empty — all pending tasks processed.")
			break
		}

		fmt.Printf("\n→ [%d] Spawning worker for %s: %s\n", processed+1, task.ID, task.Title)

		// Mark as current in session.
		sess.Current = task.ID
		if wErr := spawn.WriteSession(rt, sess); wErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ session write: %v\n", wErr)
		}

		// Open worker pane.
		surfaceID, spawnErr := spawnWorkerForTask(ctx, term, rt, agentCmd, task.ID)
		if spawnErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ spawn failed for %s: %v — skipping\n", task.ID, spawnErr)
			sess.Skipped = appendUniqID(sess.Skipped, task.ID)
			sess.Current = ""
			_ = spawn.WriteSession(rt, sess)
			continue
		}

		// Wait for worker to finish (poll liveness, detect pane exit).
		workerDone := waitForWorker(ctx, term, rt, surfaceID, masterAgent, spawnQueuePollSecs)

		// Deregister worker regardless of outcome.
		_ = spawn.RemoveWorker(rt, task.ID)

		if workerDone == workerExitStalemaster {
			fmt.Printf("✗ Master heartbeat stale during %s — queue paused. Resume with --resume.\n", task.ID)
			sess.Current = ""
			_ = spawn.WriteSession(rt, sess)
			return nil
		}

		// Task is now done/in-review; move to completed list.
		sess.Completed = appendUniqID(sess.Completed, task.ID)
		sess.Current = ""
		_ = spawn.WriteSession(rt, sess)
		processed++
	}

	sess.Current = ""
	_ = spawn.WriteSession(rt, sess)

	fmt.Printf("\n✓ Queue run complete. Processed: %d  Skipped: %d\n",
		len(sess.Completed), len(sess.Skipped))
	return nil
}

// workerExitReason classifies how a worker pane exited.
type workerExitReason int

const (
	workerExitOK           workerExitReason = iota
	workerExitStalemaster                   // master heartbeat went stale
	workerExitContextDone                   // ctx cancelled (Ctrl+C)
)

// waitForWorker polls until the worker pane closes or a stopping condition fires.
// Returns the exit reason so the caller can decide whether to continue.
func waitForWorker(ctx context.Context, term terminal.Terminal, rt string, surfaceID terminal.SurfaceID, masterAgent string, pollSecs int) workerExitReason {
	ticker := time.NewTicker(time.Duration(pollSecs) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return workerExitContextDone

		case <-ticker.C:
			// Refresh master heartbeat so workers can see we're alive.
			if hbErr := spawn.WriteHeartbeat(rt, masterAgent); hbErr != nil {
				fmt.Fprintf(os.Stderr, "⚠ heartbeat refresh: %v\n", hbErr)
			}

			// Check liveness from this side as well.
			if alive, reason := spawn.IsMasterAlive(rt); !alive {
				fmt.Fprintf(os.Stderr, "✗ Master liveness check failed: %s\n", reason)
				return workerExitStalemaster
			}

			// Check if the pane is still alive (terminal sends key noop and checks error).
			if !isPaneAlive(ctx, term, surfaceID) {
				return workerExitOK
			}
		}
	}
}

// isPaneAlive probes whether the surface is still present.
// We use ReadScreen if supported; otherwise we attempt a no-op Focus.
func isPaneAlive(ctx context.Context, term terminal.Terminal, id terminal.SurfaceID) bool {
	if term.Capabilities().CanReadScreen {
		_, err := term.ReadScreen(ctx, id)
		return err == nil
	}
	err := term.Focus(ctx, id)
	return err == nil
}

// nextReadyTask returns the next pending task that is not already in the
// session's completed or skipped sets, and whose dependencies are all done.
// Returns nil when no ready task exists.
func nextReadyTask(ctx context.Context, st *store.Client, sess *spawn.QueueSession) (*store.Task, error) {
	pending, err := st.ListTasks(ctx, "pending")
	if err != nil {
		return nil, err
	}
	done, err := st.ListTasks(ctx, "done")
	if err != nil {
		return nil, err
	}
	doneSet := make(map[string]bool, len(done))
	for _, t := range done {
		doneSet[t.ID] = true
	}

	skipSet := make(map[string]bool)
	for _, id := range sess.Completed {
		skipSet[id] = true
	}
	for _, id := range sess.Skipped {
		skipSet[id] = true
	}

	for i := range pending {
		t := &pending[i]
		if skipSet[t.ID] {
			continue
		}
		// All dependencies must be done.
		allDepsOK := true
		for _, dep := range t.DependsOn {
			if !doneSet[dep] {
				allDepsOK = false
				break
			}
		}
		if allDepsOK {
			return t, nil
		}
	}
	return nil, nil
}

// spawnWorkerForTask opens a new pane for taskID and registers it.
func spawnWorkerForTask(ctx context.Context, term terminal.Terminal, rt, agentCmd, taskID string) (terminal.SurfaceID, error) {
	env := buildWorkerEnv(taskID, nil)
	surfaceID, err := term.NewSplit(ctx, terminal.SplitOpts{
		Dir:  terminal.SplitHorizontal,
		Env:  env,
		Cmd:  agentCmd,
	})
	if err != nil {
		return "", err
	}

	// Send startup orientation.
	startup := buildWorkerStartup(taskID)
	if sErr := term.Send(ctx, surfaceID, startup); sErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ startup send to pane %s: %v\n", surfaceID, sErr)
	}
	if kErr := term.SendKey(ctx, surfaceID, "Enter"); kErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ Enter send to pane %s: %v\n", surfaceID, kErr)
	}

	// Register worker.
	_ = spawn.RegisterWorker(rt, spawn.WorkerEntry{
		SurfaceID: string(surfaceID),
		TaskID:    taskID,
		Agent:     agentCmd,
		SpawnedAt: time.Now().UTC(),
	})

	return surfaceID, nil
}

// loadOrCreateSession loads an existing session (--resume) or starts a new one.
func loadOrCreateSession(rt, agentCmd string) (*spawn.QueueSession, error) {
	if spawnQueueResume {
		sess, err := spawn.ReadSession(rt)
		if err != nil && !errors.Is(err, spawn.ErrNoSession) {
			return nil, fmt.Errorf("read session: %w", err)
		}
		if sess != nil {
			sess.Agent = agentCmd // update agent in case it changed
			return sess, nil
		}
		fmt.Println("⚠ No existing session found — starting fresh.")
	}

	sess := &spawn.QueueSession{
		Agent:     agentCmd,
		StartedAt: time.Now().UTC(),
	}
	if err := spawn.WriteSession(rt, sess); err != nil {
		return nil, fmt.Errorf("init session: %w", err)
	}
	return sess, nil
}

// appendUniqID appends id to slice only if not already present.
func appendUniqID(slice []string, id string) []string {
	for _, v := range slice {
		if v == id {
			return slice
		}
	}
	return append(slice, id)
}

