package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
)

// TestDrainResultReason verifies that workerExitContextDone and
// workerExitStalemaster results are NOT counted in sess.Completed.
func TestDrainResultReason(t *testing.T) {
	sess := &spawn.QueueSession{}

	// Simulate the post-wg.Wait drain loop logic directly.
	results := []workerResult{
		{taskID: "TASK-001", reason: workerExitOK},
		{taskID: "TASK-002", reason: workerExitContextDone},
		{taskID: "TASK-003", reason: workerExitStalemaster},
		{taskID: "TASK-005", reason: workerExitPaneGone},
		{taskID: "TASK-004", reason: workerExitOK},
	}

	processed := 0
	for _, res := range results {
		switch res.reason {
		case workerExitStalemaster, workerExitContextDone, workerExitPaneGone:
			// not counted
		default:
			sess.Completed = appendUniqID(sess.Completed, res.taskID)
			processed++
		}
	}

	if processed != 2 {
		t.Errorf("processed = %d, want 2", processed)
	}
	if len(sess.Completed) != 2 {
		t.Errorf("Completed = %v, want [TASK-001 TASK-004]", sess.Completed)
	}
	for _, id := range []string{"TASK-002", "TASK-003", "TASK-005"} {
		for _, c := range sess.Completed {
			if c == id {
				t.Errorf("%s (cancelled/stale/disappeared) should not be in Completed", id)
			}
		}
	}
}

// TestSkippedTransientNotPermanent verifies that transient collision skips go
// to SkippedTransient (not Skipped) and that buildSkipSet excludes them during
// the run but resume clears them for retry.
func TestSkippedTransientNotPermanent(t *testing.T) {
	sess := &spawn.QueueSession{}
	active := map[string]bool{}

	// Simulate a transient collision skip.
	sess.SkippedTransient = appendUniqID(sess.SkippedTransient, "TASK-010")
	if len(sess.Skipped) != 0 {
		t.Errorf("Skipped should be empty, got %v", sess.Skipped)
	}
	if len(sess.SkippedTransient) != 1 {
		t.Errorf("SkippedTransient should be 1, got %d", len(sess.SkippedTransient))
	}

	// buildSkipSet must include transient-skipped tasks during the run.
	skipSet := buildSkipSet(sess, active)
	if !skipSet["TASK-010"] {
		t.Error("TASK-010 should be in skip set during run")
	}

	// On resume, SkippedTransient is cleared — task becomes eligible again.
	sess.SkippedTransient = nil
	skipSet = buildSkipSet(sess, active)
	if skipSet["TASK-010"] {
		t.Error("TASK-010 should NOT be in skip set after resume clears SkippedTransient")
	}
}

// TestPoolParallelConcurrency verifies that multiple workers can run concurrently
// via the semaphore: if cap=2 and two workers are in-flight, a third blocks until
// one slot is released.
func TestPoolParallelConcurrency(t *testing.T) {
	// Use a fake terminal that counts concurrent in-flight workers.
	var (
		mu       sync.Mutex
		maxSeen  int
		inFlight int
	)

	cap := 2
	sem := make(chan struct{}, cap)

	// Simulate N workers acquiring and releasing the semaphore concurrently.
	var wg sync.WaitGroup
	const workers = 5
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{} // acquire
			mu.Lock()
			inFlight++
			if inFlight > maxSeen {
				maxSeen = inFlight
			}
			mu.Unlock()

			time.Sleep(5 * time.Millisecond) // simulate work

			mu.Lock()
			inFlight--
			mu.Unlock()
			<-sem // release
		}()
	}
	wg.Wait()

	if maxSeen > cap {
		t.Errorf("concurrent workers exceeded cap: maxSeen=%d cap=%d", maxSeen, cap)
	}
	if maxSeen < 2 {
		t.Errorf("expected at least 2 concurrent workers, maxSeen=%d", maxSeen)
	}
}

// TestMaxConcurrentFlag verifies that spawnQueueMaxConcurrent takes priority
// over the GG_QUEUE_MAX env var when set.
func TestMaxConcurrentFlag(t *testing.T) {
	orig := spawnQueueMaxConcurrent
	t.Cleanup(func() { spawnQueueMaxConcurrent = orig })
	t.Setenv("GG_QUEUE_MAX", "5")

	spawnQueueMaxConcurrent = 2
	if got := maxConcurrent(); got != 2 {
		t.Errorf("maxConcurrent() = %d, want 2 (flag takes priority)", got)
	}

	spawnQueueMaxConcurrent = 0
	if got := maxConcurrent(); got != 5 {
		t.Errorf("maxConcurrent() = %d, want 5 (GG_QUEUE_MAX fallback)", got)
	}
}

// TestWaitForWorkerContextCancel verifies waitForWorker returns workerExitContextDone
// when the context is cancelled before the pane exits.
func TestWaitForWorkerContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	rt := t.TempDir()
	term := terminal.NewFake()

	// Register a pane that never exits on its own.
	surf, _ := term.NewSplit(ctx, terminal.SplitOpts{Dir: terminal.SplitHorizontal})

	var reason workerExitReason
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reason = waitForWorker(ctx, term, rt, "TASK-001", surf, "test-master", 1, alwaysTaskPending)
	}()

	// Cancel the context; worker should exit quickly.
	cancel()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("waitForWorker did not return after context cancellation")
	}

	if reason != workerExitContextDone {
		t.Errorf("reason = %v, want workerExitContextDone", reason)
	}
}

// TestWaitForWorkerAdvanceSentinelMarksReady verifies a worker advance sentinel
// is only a ready-for-review signal; it does not close the pane or complete the task.
func TestWaitForWorkerAdvanceSentinelMarksReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	rt := t.TempDir()
	term := terminal.NewFake()
	surf, _ := term.NewSplit(ctx, terminal.SplitOpts{Dir: terminal.SplitHorizontal})
	if err := spawn.RegisterPane(rt, spawn.WorkerPane{
		SurfaceID: string(surf),
		TaskID:    "TASK-042",
		Agent:     "gsd",
		State:     spawn.WorkerStateWorking,
	}); err != nil {
		t.Fatalf("RegisterPane: %v", err)
	}

	done := make(chan workerExitReason, 1)
	go func() {
		done <- waitForWorker(ctx, term, rt, "TASK-042", surf, "test-master", 1, alwaysTaskPending)
	}()

	if err := spawn.WriteAdvanceSentinel(rt, "TASK-042", string(surf), "abc123"); err != nil {
		t.Fatalf("WriteAdvanceSentinel: %v", err)
	}

	select {
	case reason := <-done:
		t.Fatalf("waitForWorker returned early after advance sentinel: %v", reason)
	case <-time.After(1500 * time.Millisecond):
	}

	if !term.IsAlive(surf) {
		t.Fatal("worker pane should remain open after advance sentinel")
	}
	if _, err := spawn.ReadAdvanceSentinel(rt, "TASK-042"); !errors.Is(err, spawn.ErrNoSentinel) {
		t.Fatalf("sentinel should be consumed, stat err=%v", err)
	}
	panes, err := spawn.ListPanes(rt)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	if len(panes) != 1 {
		t.Fatalf("panes = %v, want one pane", panes)
	}
	pane := panes[0]
	if pane.State != spawn.WorkerStateReady {
		t.Fatalf("pane state = %q, want %q", pane.State, spawn.WorkerStateReady)
	}

	cancel()
	if reason := <-done; reason != workerExitContextDone {
		t.Fatalf("reason after cancel = %v, want workerExitContextDone", reason)
	}
}

// TestWaitForWorkerTaskDoneClosesPane verifies the queue only closes the worker
// pane after the backing gg task is done.
func TestWaitForWorkerTaskDoneClosesPane(t *testing.T) {
	ctx := context.Background()
	rt := t.TempDir()
	term := terminal.NewFake()
	surf, _ := term.NewSplit(ctx, terminal.SplitOpts{Dir: terminal.SplitHorizontal})

	reason := waitForWorker(ctx, term, rt, "TASK-042", surf, "test-master", 1, alwaysTaskDone)
	if reason != workerExitOK {
		t.Fatalf("reason = %v, want workerExitOK", reason)
	}
	if term.IsAlive(surf) {
		t.Fatal("worker pane should close after task is done")
	}
}

// TestWaitForWorkerPaneGoneBeforeDone verifies a vanished pane is not counted as
// completed unless the task is already done.
func TestWaitForWorkerPaneGoneBeforeDone(t *testing.T) {
	ctx := context.Background()
	rt := t.TempDir()
	term := terminal.NewFake()
	surf, _ := term.NewSplit(ctx, terminal.SplitOpts{Dir: terminal.SplitHorizontal})
	if err := term.Close(ctx, surf); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reason := waitForWorker(ctx, term, rt, "TASK-042", surf, "test-master", 1, alwaysTaskPending)
	if reason != workerExitPaneGone {
		t.Fatalf("reason = %v, want workerExitPaneGone", reason)
	}
}

// TestSpawnWorkerForTaskClearsStaleAdvanceSentinel verifies an old completion
// marker cannot immediately close a newly spawned pane for the same task.
func TestSpawnWorkerForTaskClearsStaleAdvanceSentinel(t *testing.T) {
	ctx := context.Background()
	rt := t.TempDir()
	term := terminal.NewFake()

	sentinel := workerAdvanceSentinelPath(rt, "TASK-042")
	if err := os.MkdirAll(filepath.Dir(sentinel), 0o700); err != nil {
		t.Fatalf("mkdir sentinel dir: %v", err)
	}
	if err := os.WriteFile(sentinel, []byte("old done\n"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	surf, err := spawnWorkerForTask(ctx, term, rt, "gsd", "TASK-042")
	if err != nil {
		t.Fatalf("spawnWorkerForTask: %v", err)
	}
	if !term.IsAlive(surf) {
		t.Fatal("new worker pane should remain alive")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("stale sentinel should be removed before spawn, stat err=%v", err)
	}
}

// TestBuildSkipSet verifies the skip-set composition.
func TestBuildSkipSet(t *testing.T) {
	sess := &spawn.QueueSession{
		Completed:        []string{"TASK-001"},
		Skipped:          []string{"TASK-002"},
		SkippedTransient: []string{"TASK-003"},
	}
	active := map[string]bool{"TASK-004": true}

	s := buildSkipSet(sess, active)

	for _, id := range []string{"TASK-001", "TASK-002", "TASK-003", "TASK-004"} {
		if !s[id] {
			t.Errorf("%s should be in skip set", id)
		}
	}
	if s["TASK-099"] {
		t.Error("TASK-099 should not be in skip set")
	}
}

func alwaysTaskPending(context.Context, string) bool {
	return false
}

func alwaysTaskDone(context.Context, string) bool {
	return true
}
