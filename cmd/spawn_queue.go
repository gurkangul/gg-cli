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

// spawnQueueCmd is the parent for all queue sub-operations.
var spawnQueueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Manage the sequential task queue for multi-agent orchestration",
	Long: `Control the sequential queue runner that drains pending tasks by spawning workers.

Subcommands:
  start   — begin a new queue run (drains pending tasks sequentially)
  pause   — suspend the running queue after the current worker finishes
  resume  — resume a paused queue from where it stopped
  status  — show queue state, current task, completed/skipped counts
  cancel  — abort the queue run and remove queue.json
  skip    — mark the current (or named) task as skipped and advance
  check   — verify queue health (heartbeat freshness, panes liveness)`,
}

// ── start ─────────────────────────────────────────────────────────────────────

var spawnQueueStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Begin a new sequential queue run",
	Long: `Drain the pending task queue by spawning a worker pane for each task.

Workers run sequentially (MaxConcurrent=1). For each pending task the runner:
  1. Verifies master liveness (heartbeat must be fresh)
  2. Opens a worker pane via the terminal backend (GG_TERMINAL)
  3. Sends the task ID to the worker for context loading
  4. Waits for the worker pane to exit
  5. Advances to the next pending task

Queue state is persisted at ~/.gg/projects/<id>/spawn/queue.json.`,
	RunE: runSpawnQueueStart,
}

var (
	spawnQueueAgent    string
	spawnQueueMaxTasks int
	spawnQueuePollSecs int
)

func init() {
	spawnQueueStartCmd.Flags().StringVar(&spawnQueueAgent, "agent", "", "agent command for worker panes (default: $GG_SPAWN_AGENT or 'gsd')")
	spawnQueueStartCmd.Flags().IntVar(&spawnQueueMaxTasks, "max-tasks", 0, "stop after processing this many tasks (0 = no limit)")
	spawnQueueStartCmd.Flags().IntVar(&spawnQueuePollSecs, "poll", 30, "seconds between liveness checks while a worker is running")
	spawnQueueCmd.AddCommand(spawnQueueStartCmd)
}

func runSpawnQueueStart(cmd *cobra.Command, _ []string) error {
	agentCmd := spawnQueueAgent
	if agentCmd == "" {
		agentCmd = spawnAgentDefault()
	}

	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	masterAgent := os.Getenv("GG_AGENT")
	if masterAgent == "" {
		masterAgent = os.Getenv("GG_ROLE")
	}
	if hbErr := spawn.WriteHeartbeat(rt, masterAgent); hbErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ heartbeat write failed: %v\n", hbErr)
	}

	sess := &spawn.QueueSession{
		Agent:     agentCmd,
		StartedAt: time.Now().UTC(),
	}
	if err := spawn.WriteQueue(rt, sess); err != nil {
		return fmt.Errorf("init queue: %w", err)
	}

	term, err := terminal.NewFromEnv()
	if err != nil {
		return fmt.Errorf("terminal backend: %w", err)
	}

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	return drainQueue(cmd.Context(), cmd, rt, sess, term, d.store, masterAgent, agentCmd)
}

// ── resume ────────────────────────────────────────────────────────────────────

var spawnQueueResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume a paused queue run from where it stopped",
	RunE:  runSpawnQueueResume,
}

func init() {
	spawnQueueResumeCmd.Flags().StringVar(&spawnQueueAgent, "agent", "", "override agent command (default: value from queue.json)")
	spawnQueueCmd.AddCommand(spawnQueueResumeCmd)
}

func runSpawnQueueResume(cmd *cobra.Command, _ []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	sess, err := spawn.ReadQueue(rt)
	if errors.Is(err, spawn.ErrNoQueue) {
		return fmt.Errorf("no queue session found — run 'gg spawn queue start' first")
	}
	if err != nil {
		return fmt.Errorf("read queue: %w", err)
	}

	if spawnQueueAgent != "" {
		sess.Agent = spawnQueueAgent
	}
	sess.Paused = false

	masterAgent := os.Getenv("GG_AGENT")
	if masterAgent == "" {
		masterAgent = os.Getenv("GG_ROLE")
	}
	if hbErr := spawn.WriteHeartbeat(rt, masterAgent); hbErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ heartbeat write: %v\n", hbErr)
	}

	term, err := terminal.NewFromEnv()
	if err != nil {
		return fmt.Errorf("terminal backend: %w", err)
	}

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	fmt.Printf("→ Resuming queue (agent: %s, completed: %d, skipped: %d)\n",
		sess.Agent, len(sess.Completed), len(sess.Skipped))
	return drainQueue(cmd.Context(), cmd, rt, sess, term, d.store, masterAgent, sess.Agent)
}

// ── pause ─────────────────────────────────────────────────────────────────────

var spawnQueuePauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Suspend the queue after the current worker finishes",
	RunE:  runSpawnQueuePause,
}

func init() {
	spawnQueueCmd.AddCommand(spawnQueuePauseCmd)
}

func runSpawnQueuePause(_ *cobra.Command, _ []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	sess, err := spawn.ReadQueue(rt)
	if errors.Is(err, spawn.ErrNoQueue) {
		return fmt.Errorf("no active queue session")
	}
	if err != nil {
		return fmt.Errorf("read queue: %w", err)
	}

	sess.Paused = true
	if err := spawn.WriteQueue(rt, sess); err != nil {
		return err
	}
	fmt.Println("⏸ Queue paused — current worker will finish, then runner stops.")
	fmt.Println("  Resume with: gg spawn queue resume")
	return nil
}

// ── cancel ────────────────────────────────────────────────────────────────────

var spawnQueueCancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Abort the queue run and remove queue.json",
	RunE:  runSpawnQueueCancel,
}

func init() {
	spawnQueueCmd.AddCommand(spawnQueueCancelCmd)
}

func runSpawnQueueCancel(_ *cobra.Command, _ []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	sess, readErr := spawn.ReadQueue(rt)
	if readErr == nil && sess != nil && sess.CurrentTask != "" {
		fmt.Printf("⚠ Cancelling while task %s is active — worker pane may still be running.\n", sess.CurrentTask)
	}

	if err := spawn.DeleteQueue(rt); err != nil {
		return fmt.Errorf("cancel queue: %w", err)
	}
	fmt.Println("✓ Queue cancelled and queue.json removed.")
	return nil
}

// ── skip ──────────────────────────────────────────────────────────────────────

var spawnQueueSkipCmd = &cobra.Command{
	Use:   "skip [task-id]",
	Short: "Mark the current (or named) task as skipped and advance the queue",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSpawnQueueSkip,
}

func init() {
	spawnQueueCmd.AddCommand(spawnQueueSkipCmd)
}

func runSpawnQueueSkip(_ *cobra.Command, args []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	sess, err := spawn.ReadQueue(rt)
	if errors.Is(err, spawn.ErrNoQueue) {
		return fmt.Errorf("no active queue session")
	}
	if err != nil {
		return fmt.Errorf("read queue: %w", err)
	}

	taskID := sess.CurrentTask
	if len(args) == 1 {
		taskID = args[0]
	}
	if taskID == "" {
		return fmt.Errorf("no current task — pass a task ID explicitly: gg spawn queue skip TASK-NNN")
	}

	sess.Skipped = appendUniqID(sess.Skipped, taskID)
	if sess.CurrentTask == taskID {
		sess.CurrentTask = ""
	}
	if err := spawn.WriteQueue(rt, sess); err != nil {
		return err
	}
	fmt.Printf("⏭ %s marked as skipped.\n", taskID)
	return nil
}

// ── check ─────────────────────────────────────────────────────────────────────

var spawnQueueCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Verify queue health: heartbeat freshness and pane liveness",
	RunE:  runSpawnQueueCheck,
}

func init() {
	spawnQueueCmd.AddCommand(spawnQueueCheckCmd)
}

func runSpawnQueueCheck(_ *cobra.Command, _ []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	ok := true

	// Check heartbeat.
	alive, reason := spawn.IsMasterAlive(rt)
	if alive {
		fmt.Println("✓ Master heartbeat: fresh")
	} else {
		fmt.Printf("✗ Master heartbeat: %s\n", reason)
		ok = false
	}

	// Check queue.
	sess, qErr := spawn.ReadQueue(rt)
	if errors.Is(qErr, spawn.ErrNoQueue) {
		fmt.Println("⚠ Queue: no active session")
	} else if qErr != nil {
		fmt.Printf("✗ Queue read error: %v\n", qErr)
		ok = false
	} else {
		pausedNote := ""
		if sess.Paused {
			pausedNote = " [PAUSED]"
		}
		fmt.Printf("✓ Queue: agent=%s completed=%d skipped=%d current=%q%s\n",
			sess.Agent, len(sess.Completed), len(sess.Skipped), sess.CurrentTask, pausedNote)
	}

	// Check panes.
	panes, panesErr := spawn.ListPanes(rt)
	if panesErr != nil {
		fmt.Printf("✗ Panes read error: %v\n", panesErr)
		ok = false
	} else {
		fmt.Printf("✓ Panes: %d registered\n", len(panes))
	}

	if !ok {
		return fmt.Errorf("queue health check failed")
	}
	return nil
}

// ── status (queue-level) ──────────────────────────────────────────────────────

var spawnQueueStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show queue state: current task, completed/skipped counts, panes",
	RunE:  runSpawnQueueStatus,
}

func init() {
	spawnQueueCmd.AddCommand(spawnQueueStatusCmd)
}

func runSpawnQueueStatus(_ *cobra.Command, _ []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	sess, err := spawn.ReadQueue(rt)
	if errors.Is(err, spawn.ErrNoQueue) {
		fmt.Println("No active queue session. Run 'gg spawn queue start' to begin.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("read queue: %w", err)
	}

	panes, _ := spawn.ListPanes(rt)

	pausedNote := ""
	if sess.Paused {
		pausedNote = " [PAUSED]"
	}
	dur := time.Since(sess.StartedAt).Round(time.Second)
	fmt.Printf("Queue%s — agent: %s  running: %s\n", pausedNote, sess.Agent, dur)
	if sess.CurrentTask != "" {
		fmt.Printf("  Current: %s\n", sess.CurrentTask)
	}
	fmt.Printf("  Completed: %d  Skipped: %d  Active panes: %d\n",
		len(sess.Completed), len(sess.Skipped), len(panes))
	return nil
}

// ── drain (shared runner) ─────────────────────────────────────────────────────

func drainQueue(ctx context.Context, cmd *cobra.Command, rt string, sess *spawn.QueueSession, term terminal.Terminal, st *store.Client, masterAgent, agentCmd string) error {
	processed := 0

	for {
		if ctx.Err() != nil {
			fmt.Println("\n⚠ Interrupted — queue paused. Use 'gg spawn queue resume' to continue.")
			return nil
		}

		// Re-read queue state to catch pause/cancel from another process.
		current, err := spawn.ReadQueue(rt)
		if errors.Is(err, spawn.ErrNoQueue) {
			fmt.Println("⚠ queue.json removed — queue cancelled.")
			return nil
		}
		if err == nil && current.Paused {
			fmt.Println("⏸ Queue paused by external signal.")
			return nil
		}

		if spawnQueueMaxTasks > 0 && processed >= spawnQueueMaxTasks {
			fmt.Printf("✓ Reached --max-tasks=%d limit. Queue paused.\n", spawnQueueMaxTasks)
			return nil
		}

		if hbErr := spawn.WriteHeartbeat(rt, masterAgent); hbErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ heartbeat refresh failed: %v\n", hbErr)
		}

		task, err := nextReadyTask(ctx, st, sess)
		if err != nil {
			return fmt.Errorf("fetch next task: %w", err)
		}
		if task == nil {
			fmt.Println("✓ Queue empty — all pending tasks processed.")
			break
		}

		fmt.Printf("\n→ [%d] Spawning worker for %s: %s\n", processed+1, task.ID, task.Title)

		sess.CurrentTask = task.ID
		if wErr := spawn.WriteQueue(rt, sess); wErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ queue write: %v\n", wErr)
		}

		surfaceID, spawnErr := spawnWorkerForTask(ctx, term, rt, agentCmd, task.ID)
		if spawnErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ spawn failed for %s: %v — skipping\n", task.ID, spawnErr)
			sess.Skipped = appendUniqID(sess.Skipped, task.ID)
			sess.CurrentTask = ""
			_ = spawn.WriteQueue(rt, sess)
			continue
		}

		pollSecs := spawnQueuePollSecs
		if pollSecs == 0 {
			pollSecs = 30
		}
		workerDone := waitForWorker(ctx, term, rt, surfaceID, masterAgent, pollSecs)

		_ = spawn.RemovePane(rt, task.ID)

		if workerDone == workerExitStalemaster {
			fmt.Printf("✗ Master heartbeat stale during %s — queue paused. Resume with 'gg spawn queue resume'.\n", task.ID)
			sess.CurrentTask = ""
			_ = spawn.WriteQueue(rt, sess)
			return nil
		}

		sess.Completed = appendUniqID(sess.Completed, task.ID)
		sess.CurrentTask = ""
		_ = spawn.WriteQueue(rt, sess)
		processed++
	}

	sess.CurrentTask = ""
	_ = spawn.WriteQueue(rt, sess)

	fmt.Printf("\n✓ Queue run complete. Processed: %d  Skipped: %d\n",
		len(sess.Completed), len(sess.Skipped))
	return nil
}

// ── pane wait helpers ─────────────────────────────────────────────────────────

// workerExitReason classifies how a worker pane exited.
type workerExitReason int

const (
	workerExitOK          workerExitReason = iota
	workerExitStalemaster                  // master heartbeat went stale
	workerExitContextDone                  // ctx cancelled (Ctrl+C)
)

// waitForWorker polls until the worker pane closes or a stopping condition fires.
func waitForWorker(ctx context.Context, term terminal.Terminal, rt string, surfaceID terminal.SurfaceID, masterAgent string, pollSecs int) workerExitReason {
	ticker := time.NewTicker(time.Duration(pollSecs) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return workerExitContextDone

		case <-ticker.C:
			if hbErr := spawn.WriteHeartbeat(rt, masterAgent); hbErr != nil {
				fmt.Fprintf(os.Stderr, "⚠ heartbeat refresh: %v\n", hbErr)
			}

			if alive, reason := spawn.IsMasterAlive(rt); !alive {
				fmt.Fprintf(os.Stderr, "✗ Master liveness check failed: %s\n", reason)
				return workerExitStalemaster
			}

			if !isPaneAlive(ctx, term, surfaceID) {
				return workerExitOK
			}
		}
	}
}

// isPaneAlive probes whether the surface is still present.
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

// spawnWorkerForTask opens a new pane for taskID and registers it in panes.json.
func spawnWorkerForTask(ctx context.Context, term terminal.Terminal, rt, agentCmd, taskID string) (terminal.SurfaceID, error) {
	env := buildWorkerEnv(taskID, nil)
	surfaceID, err := term.NewSplit(ctx, terminal.SplitOpts{
		Dir: terminal.SplitHorizontal,
		Env: env,
		Cmd: agentCmd,
	})
	if err != nil {
		return "", err
	}

	startup := buildWorkerStartup(taskID)
	if sErr := term.Send(ctx, surfaceID, startup); sErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ startup send to pane %s: %v\n", surfaceID, sErr)
	}
	if kErr := term.SendKey(ctx, surfaceID, "Enter"); kErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ Enter send to pane %s: %v\n", surfaceID, kErr)
	}

	_ = spawn.RegisterPane(rt, spawn.WorkerPane{
		SurfaceID: string(surfaceID),
		TaskID:    taskID,
		Agent:     agentCmd,
		SpawnedAt: time.Now().UTC(),
	})

	return surfaceID, nil
}

// loadOrCreateSession loads an existing session (--resume) or starts a new one.
// Kept for backward compat with spawn_worker.go helper; new entrypoint is drainQueue.
func loadOrCreateSession(rt, agentCmd string) (*spawn.QueueSession, error) {
	sess, err := spawn.ReadQueue(rt)
	if err != nil && !errors.Is(err, spawn.ErrNoQueue) {
		return nil, fmt.Errorf("read queue: %w", err)
	}
	if sess != nil {
		sess.Agent = agentCmd
		return sess, nil
	}
	sess = &spawn.QueueSession{
		Agent:     agentCmd,
		StartedAt: time.Now().UTC(),
	}
	if err := spawn.WriteQueue(rt, sess); err != nil {
		return nil, fmt.Errorf("init queue: %w", err)
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

func init() {
	spawnCmd.AddCommand(spawnQueueCmd)
}
