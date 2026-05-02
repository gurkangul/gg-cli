package cmd

// spawn_queue_pool.go — parallel worker-pool execution for gg spawn queue.
//
// drainQueueParallel dispatches up to GG_QUEUE_MAX workers concurrently,
// enforcing advisory file-lock collision checks (TASK-276 AC1/AC2/AC5).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/locks"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

// spawnQueueMaxConcurrent is set by the --max-concurrent flag on 'gg spawn queue start'.
// 0 means "use GG_QUEUE_MAX or the compiled default".
var spawnQueueMaxConcurrent int

// maxConcurrent returns the effective worker cap, in priority order:
//  1. --max-concurrent flag (when > 0)
//  2. GG_QUEUE_MAX environment variable
//  3. compiled default (DefaultMaxConcurrent = 3)
func maxConcurrent() int {
	if spawnQueueMaxConcurrent > 0 {
		return spawnQueueMaxConcurrent
	}
	if v := os.Getenv("GG_QUEUE_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return spawn.DefaultMaxConcurrent
}

// workerExitReason classifies how a worker pane exited.
type workerExitReason int

const (
	workerExitOK          workerExitReason = iota
	workerExitStalemaster                  // master heartbeat went stale
	workerExitContextDone                  // ctx cancelled (Ctrl+C)
	workerExitPaneGone                     // worker pane disappeared before task done
)

// workerResult carries the outcome of one worker slot.
type workerResult struct {
	taskID    string
	surfaceID terminal.SurfaceID
	reason    workerExitReason
}

// drainQueue drains the pending task queue using a parallel worker pool.
// At most maxConcurrent() workers run simultaneously (GG_QUEUE_MAX, default 3).
// For each task: advisory collision is checked before spawn; on collision the
// task is skipped with a clear error message (--force overrides).
func drainQueue(ctx context.Context, cmd *cobra.Command, rt string, sess *spawn.QueueSession, term terminal.Terminal, st *store.Client, masterAgent, agentCmd string) error {
	cap := maxConcurrent()
	lockStore := locks.New(spawn.Dir(rt))

	// Semaphore: limits active workers to cap.
	sem := make(chan struct{}, cap)
	results := make(chan workerResult, cap*2)

	var wg sync.WaitGroup
	processed := 0
	// activeByTask tracks which tasks are currently running (for skip-set dedup).
	activeByTask := make(map[string]bool)
	var mu sync.Mutex

	for {
		if ctx.Err() != nil {
			fmt.Println("\n⚠ Interrupted — queue paused. Use 'gg spawn queue resume' to continue.")
			break
		}

		// Drain completed workers before deciding to spawn new ones.
		drained := false
		for !drained {
			select {
			case res := <-results:
				wg.Done()
				<-sem
				_ = spawn.RemovePane(rt, res.taskID)
				if rErr := lockStore.Release(res.taskID); rErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "⚠ release lock for %s: %v\n", res.taskID, rErr)
				}

				mu.Lock()
				delete(activeByTask, res.taskID)
				mu.Unlock()

				switch res.reason {
				case workerExitStalemaster:
					fmt.Fprintf(cmd.ErrOrStderr(), "✗ Master heartbeat stale during %s — queue paused.\n", res.taskID)
					sess.CurrentTask = ""
					_ = spawn.WriteQueue(rt, sess)
					wg.Wait()
					return nil
				case workerExitContextDone:
					// Context cancelled — don't count as completed or skipped.
					sess.CurrentTask = ""
					_ = spawn.WriteQueue(rt, sess)
				case workerExitPaneGone:
					fmt.Fprintf(cmd.ErrOrStderr(), "✗ Worker pane for %s disappeared before task was done — queue paused.\n", res.taskID)
					sess.CurrentTask = ""
					_ = spawn.WriteQueue(rt, sess)
					wg.Wait()
					return nil
				default:
					sess.Completed = appendUniqID(sess.Completed, res.taskID)
					sess.CurrentTask = res.taskID
					_ = spawn.WriteQueue(rt, sess)
					processed++
				}
			default:
				drained = true
			}
		}

		// Re-read queue state to catch pause/cancel from another process.
		current, err := spawn.ReadQueue(rt)
		if isQueueCancelled(err, current) {
			break
		}

		if spawnQueueMaxTasks > 0 && processed >= spawnQueueMaxTasks {
			fmt.Printf("✓ Reached --max-tasks=%d limit.\n", spawnQueueMaxTasks)
			break
		}

		// Record active_workers telemetry.
		mu.Lock()
		activeCount := len(activeByTask)
		mu.Unlock()

		rtDir := rt
		telemetry.RecordActiveWorkers(rtDir, masterAgent, activeCount)

		// Try to acquire a slot.
		select {
		case sem <- struct{}{}:
			// Slot acquired — find next task.
		default:
			// Pool full — wait for a result before trying again.
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if hbErr := spawn.WriteHeartbeat(rt, masterAgent); hbErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ heartbeat: %v\n", hbErr)
		}

		mu.Lock()
		skipSet := buildSkipSet(sess, activeByTask)
		mu.Unlock()

		task, taskErr := nextReadyTask(ctx, st, skipSet)
		if taskErr != nil {
			<-sem
			return fmt.Errorf("fetch next task: %w", taskErr)
		}
		if task == nil {
			<-sem
			mu.Lock()
			idle := len(activeByTask) == 0
			mu.Unlock()
			if idle {
				fmt.Println("✓ Queue empty — all pending tasks processed.")
				break
			}
			// Workers still running; wait for one to finish.
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Check advisory collision before spawning.
		// PathsFor returns the paths this task already claimed in a prior run (e.g.
		// on resume). For a first-time dispatch, paths is nil and CheckConflicts is
		// a no-op — the worker registers its paths via `gg task claim-files` after
		// it determines which files it will touch.
		taskPaths, pathErr := lockStore.PathsFor(task.ID)
		if pathErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ lock path lookup for %s: %v\n", task.ID, pathErr)
		}
		collisions, collErr := lockStore.CheckConflicts(taskPaths, task.ID)
		if collErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ lock check for %s: %v\n", task.ID, collErr)
		}
		if len(collisions) > 0 && !spawnQueueForce {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ %s collision-skipped (transient): %s\n", task.ID, collisions.Error())
			sess.SkippedTransient = appendUniqID(sess.SkippedTransient, task.ID)
			_ = spawn.WriteQueue(rt, sess)
			<-sem
			continue
		}
		if len(collisions) > 0 && spawnQueueForce {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ force-overriding collision for %s: %s\n", task.ID, collisions.Error())
		}

		fmt.Printf("\n→ [slot %d/%d] Spawning worker for %s: %s\n", activeCount+1, cap, task.ID, task.Title)

		surfaceID, spawnErr := spawnWorkerForTask(ctx, term, rt, agentCmd, task.ID)
		if spawnErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ spawn failed for %s: %v — skipping\n", task.ID, spawnErr)
			sess.Skipped = appendUniqID(sess.Skipped, task.ID)
			_ = spawn.WriteQueue(rt, sess)
			<-sem
			continue
		}

		mu.Lock()
		activeByTask[task.ID] = true
		mu.Unlock()

		pollSecs := spawnQueuePollSecs
		if pollSecs == 0 {
			pollSecs = 30
		}

		wg.Add(1)
		isDone := taskDoneChecker(st)
		go func(tid string, sid terminal.SurfaceID) {
			reason := waitForWorker(ctx, term, rt, tid, sid, masterAgent, pollSecs, isDone)
			results <- workerResult{taskID: tid, surfaceID: sid, reason: reason}
		}(task.ID, surfaceID)
	}

	// Wait for all in-flight workers to finish.
	wg.Wait()
	// Drain remaining results — must check reason, same as the hot loop.
	close(results)
	for res := range results {
		if rErr := lockStore.Release(res.taskID); rErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ release lock for %s: %v\n", res.taskID, rErr)
		}
		_ = spawn.RemovePane(rt, res.taskID)
		switch res.reason {
		case workerExitStalemaster, workerExitContextDone, workerExitPaneGone:
			// Stale/cancelled/disappeared workers are not counted as completed.
		default:
			sess.Completed = appendUniqID(sess.Completed, res.taskID)
			processed++
		}
	}

	sess.CurrentTask = ""
	_ = spawn.WriteQueue(rt, sess)
	fmt.Printf("\n✓ Queue run complete. Processed: %d  Skipped: %d\n",
		len(sess.Completed), len(sess.Skipped))
	return nil
}

// isQueueCancelled returns true if queue.json was removed or paused.
func isQueueCancelled(err error, current *spawn.QueueSession) bool {
	if err != nil {
		if isNoQueue(err) {
			fmt.Println("⚠ queue.json removed — queue cancelled.")
			return true
		}
	}
	if current != nil && current.Paused {
		fmt.Println("⏸ Queue paused by external signal.")
		return true
	}
	return false
}

// buildSkipSet builds the combined skip set from the session plus currently
// active tasks, so we don't pick up a task that's already being worked on.
// SkippedTransient tasks are included so they aren't re-tried while the
// conflicting task is still running; on the next queue resume they are cleared.
func buildSkipSet(sess *spawn.QueueSession, active map[string]bool) map[string]bool {
	total := len(sess.Completed) + len(sess.Skipped) + len(sess.SkippedTransient) + len(active)
	s := make(map[string]bool, total)
	for _, id := range sess.Completed {
		s[id] = true
	}
	for _, id := range sess.Skipped {
		s[id] = true
	}
	for _, id := range sess.SkippedTransient {
		s[id] = true
	}
	for id := range active {
		s[id] = true
	}
	return s
}

type taskDoneFunc func(context.Context, string) bool

func taskDoneChecker(st *store.Client) taskDoneFunc {
	return func(ctx context.Context, taskID string) bool {
		task, err := st.GetTask(ctx, taskID)
		if err != nil {
			return false
		}
		return task.Status == "done"
	}
}

// waitForWorker polls until the task is actually done, the pane disappears, or
// a stopping condition fires. A worker advance sentinel means "ready for master
// review"; it must not close the pane or count the task complete.
func waitForWorker(ctx context.Context, term terminal.Terminal, rt, taskID string, surfaceID terminal.SurfaceID, masterAgent string, pollSecs int, isTaskDone taskDoneFunc) workerExitReason {
	ticker := time.NewTicker(time.Duration(pollSecs) * time.Second)
	defer ticker.Stop()

	for {
		consumeAdvanceSentinelAsReady(rt, taskID)

		if isTaskDone(ctx, taskID) {
			if err := term.Close(ctx, surfaceID); err != nil {
				fmt.Fprintf(os.Stderr, "⚠ close worker pane %s for %s: %v\n", surfaceID, taskID, err)
			}
			return workerExitOK
		}

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
				if isTaskDone(ctx, taskID) {
					return workerExitOK
				}
				return workerExitPaneGone
			}
		}
	}
}

func workerAdvanceSentinelPath(rt, taskID string) string {
	return filepath.Join(spawn.Dir(rt), "advance", taskID+".done")
}

func clearWorkerAdvanceSentinel(rt, taskID string) {
	if taskID == "" {
		return
	}
	if err := os.Remove(workerAdvanceSentinelPath(rt, taskID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "⚠ clear stale advance sentinel for %s: %v\n", taskID, err)
	}
}

func consumeAdvanceSentinelAsReady(rt, taskID string) bool {
	if taskID == "" {
		return false
	}
	s, err := spawn.ConsumeAdvanceSentinel(rt, taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ consume advance sentinel for %s: %v\n", taskID, err)
		return false
	}
	if s == nil {
		return false
	}
	commitRef := s.CommitSHA
	if commitRef == "" {
		commitRef = "(no sha)"
	}
	surfRef := s.SurfaceID
	if surfRef == "" {
		surfRef = "(unknown surface)"
	}
	fmt.Fprintf(os.Stderr, "⚡ worker ready: %s at %s on %s\n", taskID, commitRef, surfRef)
	_ = spawn.UpdateWorkerState(rt, taskID, spawn.WorkerStateReady)
	return true
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

// nextReadyTask returns the next pending task that is not in skipSet and whose
// deps are all done. Returns nil when no ready task exists.
func nextReadyTask(ctx context.Context, st *store.Client, skipSet map[string]bool) (*store.Task, error) {
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
	clearWorkerAdvanceSentinel(rt, taskID)

	env := buildWorkerEnv(taskID, nil)
	surfaceID, err := term.NewSplit(ctx, terminal.SplitOpts{
		Dir: terminal.SplitHorizontal,
		Env: env,
	})
	if err != nil {
		return "", err
	}

	bootstrapAgentInPane(ctx, term, surfaceID, buildAgentLaunchCommand(agentCmd), taskID, os.Stderr)

	_ = spawn.RegisterPane(rt, spawn.WorkerPane{
		SurfaceID:     string(surfaceID),
		TaskID:        taskID,
		Agent:         agentCmd,
		SpawnedAt:     time.Now().UTC(),
		State:         spawn.WorkerStateWorking,
		LastHeartbeat: time.Now().UTC(),
	})

	return surfaceID, nil
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

// isNoQueue checks whether err is ErrNoQueue.
func isNoQueue(err error) bool {
	return errors.Is(err, spawn.ErrNoQueue)
}
