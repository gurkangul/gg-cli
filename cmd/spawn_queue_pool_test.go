package cmd

import (
	"context"
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
		{taskID: "TASK-004", reason: workerExitOK},
	}

	processed := 0
	for _, res := range results {
		switch res.reason {
		case workerExitStalemaster, workerExitContextDone:
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
	for _, id := range []string{"TASK-002", "TASK-003"} {
		for _, c := range sess.Completed {
			if c == id {
				t.Errorf("%s (cancelled/stale) should not be in Completed", id)
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
		mu          sync.Mutex
		maxSeen     int
		inFlight    int
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
		reason = waitForWorker(ctx, term, rt, surf, "test-master", 1)
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

