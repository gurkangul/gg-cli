package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
)

// spawnQueueCmd is the parent for all queue sub-operations.
var spawnQueueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Manage the parallel task queue for multi-agent orchestration",
	Long: `Control the parallel queue runner that drains pending tasks by spawning workers.

Subcommands:
  start   — begin a new queue run (drains pending tasks in parallel)
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
	Short: "Begin a new parallel queue run",
	Long: `Drain the pending task queue by spawning worker panes in parallel.

Up to --max-concurrent workers run simultaneously (default: GG_QUEUE_MAX env, else 3). For each
pending task the runner:
  1. Verifies master liveness (heartbeat must be fresh)
  2. Checks advisory file-lock collision (locks.json); blocks on conflict
     unless --force is passed
  3. Opens a worker pane via the terminal backend (GG_TERMINAL)
  4. Sends the task ID to the worker for context loading
  5. When the worker pane exits, advances to the next pending task

Queue state is persisted at ~/.gg/projects/<id>/spawn/queue.json.
Active workers cap: --max-concurrent flag > GG_QUEUE_MAX env var > default 3.`,
	RunE: runSpawnQueueStart,
}

var (
	spawnQueueAgent    string
	spawnQueueMaxTasks int
	spawnQueuePollSecs int
	spawnQueueForce    bool
)

func init() {
	spawnQueueStartCmd.Flags().StringVar(&spawnQueueAgent, "agent", "", "agent command for worker panes (default: $GG_SPAWN_AGENT or 'gsd')")
	spawnQueueStartCmd.Flags().IntVar(&spawnQueueMaxTasks, "max-tasks", 0, "stop after processing this many tasks (0 = no limit)")
	spawnQueueStartCmd.Flags().IntVar(&spawnQueuePollSecs, "poll", 30, "seconds between liveness checks while a worker is running")
	spawnQueueStartCmd.Flags().BoolVar(&spawnQueueForce, "force", false, "override advisory file-lock collisions (logs override, continues spawn)")
	spawnQueueStartCmd.Flags().IntVar(&spawnQueueMaxConcurrent, "max-concurrent", 0, "max simultaneous workers (default: GG_QUEUE_MAX or 3)")
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

	masterAgent := masterAgentTag()
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

	cap := maxConcurrent()
	fmt.Printf("→ Queue started (agent: %s, max-concurrent: %d)\n", agentCmd, cap)
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
	// Transient-skipped tasks (advisory collision) are eligible for retry on resume.
	if len(sess.SkippedTransient) > 0 {
		fmt.Printf("→ Returning %d transient-skipped task(s) to queue for retry.\n", len(sess.SkippedTransient))
		sess.SkippedTransient = nil
	}

	masterAgent := masterAgentTag()
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
	fmt.Println("⏸ Queue paused — active workers finish naturally, then runner stops.")
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

	alive, reason := spawn.IsMasterAlive(rt)
	if alive {
		fmt.Println("✓ Master heartbeat: fresh")
	} else {
		fmt.Printf("✗ Master heartbeat: %s\n", reason)
		ok = false
	}

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
		fmt.Printf("✓ Queue: agent=%s completed=%d skipped=%d current=%q%s cap=%d\n",
			sess.Agent, len(sess.Completed), len(sess.Skipped), sess.CurrentTask, pausedNote, maxConcurrent())
	}

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
	fmt.Printf("Queue%s — agent: %s  running: %s  cap: %d\n", pausedNote, sess.Agent, dur, maxConcurrent())
	if sess.CurrentTask != "" {
		fmt.Printf("  Current: %s\n", sess.CurrentTask)
	}
	fmt.Printf("  Completed: %d  Skipped: %d  Active panes: %d\n",
		len(sess.Completed), len(sess.Skipped), len(panes))
	return nil
}

// ── shared helpers ────────────────────────────────────────────────────────────

// masterAgentTag returns the GG_AGENT or GG_ROLE value for heartbeat labelling.
func masterAgentTag() string {
	if v := os.Getenv("GG_AGENT"); v != "" {
		return v
	}
	if v := os.Getenv("GG_ROLE"); v != "" {
		return v
	}
	return "master"
}

func init() {
	spawnCmd.AddCommand(spawnQueueCmd)
}
