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

// maxConcurrent returns the effective worker cap from GG_QUEUE_MAX or the
// compiled default.
func maxConcurrent() int {
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
				_ = lockStore.Release(res.taskID)

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
		collisions, collErr := lockStore.CheckConflicts(nil, task.ID)
		if collErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ lock check for %s: %v\n", task.ID, collErr)
		}
		if len(collisions) > 0 && !spawnQueueForce {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ %s skipped: %s\n", task.ID, collisions.Error())
			sess.Skipped = appendUniqID(sess.Skipped, task.ID)
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
		go func(tid string, sid terminal.SurfaceID) {
			reason := waitForWorker(ctx, term, rt, sid, masterAgent, pollSecs)
			results <- workerResult{taskID: tid, surfaceID: sid, reason: reason}
		}(task.ID, surfaceID)
	}

	// Wait for all in-flight workers to finish.
	wg.Wait()
	// Drain remaining results.
	close(results)
	for res := range results {
		sess.Completed = appendUniqID(sess.Completed, res.taskID)
		processed++
		_ = lockStore.Release(res.taskID)
		_ = spawn.RemovePane(rt, res.taskID)
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
func buildSkipSet(sess *spawn.QueueSession, active map[string]bool) map[string]bool {
	s := make(map[string]bool, len(sess.Completed)+len(sess.Skipped)+len(active))
	for _, id := range sess.Completed {
		s[id] = true
	}
	for _, id := range sess.Skipped {
		s[id] = true
	}
	for id := range active {
		s[id] = true
	}
	return s
}

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
