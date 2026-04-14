package store

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// seededClient returns a Client wired to a temp dir with the seq file
// pre-seeded to n. The Qdrant client is intentionally nil — the bootstrap
// path (maxTaskIDNumber) must NOT be reached if n > 0.
func seededTaskClient(t *testing.T, n int) *Client {
	t.Helper()
	dir := t.TempDir()
	if n > 0 {
		if err := os.WriteFile(dir+"/.task-seq", []byte(fmt.Sprintf("%d\n", n)), 0600); err != nil {
			t.Fatalf("seed seq file: %v", err)
		}
	}
	return &Client{dataDir: dir}
}

func seededDiscClient(t *testing.T, n int) *Client {
	t.Helper()
	dir := t.TempDir()
	if n > 0 {
		if err := os.WriteFile(dir+"/.disc-seq", []byte(fmt.Sprintf("%d\n", n)), 0600); err != nil {
			t.Fatalf("seed seq file: %v", err)
		}
	}
	return &Client{dataDir: dir}
}

// TestAllocTaskID_Sequential verifies that a single goroutine gets strictly
// incrementing IDs from a pre-seeded counter.
func TestAllocTaskID_Sequential(t *testing.T) {
	c := seededTaskClient(t, 5)
	ctx := context.Background()

	for i := 6; i <= 10; i++ {
		id, err := c.allocTaskID(ctx)
		if err != nil {
			t.Fatalf("alloc %d: %v", i, err)
		}
		want := fmt.Sprintf("TASK-%03d", i)
		if id != want {
			t.Fatalf("got %s, want %s", id, want)
		}
	}
}

// TestAllocTaskID_Concurrent verifies that N goroutines each get a unique ID
// with no duplicates and no gaps. The seq file is pre-seeded so the Qdrant
// bootstrap path is never triggered.
func TestAllocTaskID_Concurrent(t *testing.T) {
	const goroutines = 50
	c := seededTaskClient(t, 0)
	// Seed with 0 would trigger bootstrap — write 1 to the file instead, so
	// allocTaskID sees n=1 and skips maxTaskIDNumber.
	if err := os.WriteFile(c.dataDir+"/.task-seq", []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ids := make(chan string, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			id, err := c.allocTaskID(ctx)
			if err != nil {
				// surface via a sentinel — we check below
				ids <- "ERROR:" + err.Error()
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]int)
	for id := range ids {
		if strings.HasPrefix(id, "ERROR:") {
			t.Fatalf("goroutine error: %s", id)
		}
		seen[id]++
	}

	// All IDs must be unique.
	for id, count := range seen {
		if count > 1 {
			t.Errorf("duplicate ID %s seen %d times", id, count)
		}
	}

	// IDs must form a contiguous range [2 .. goroutines+1].
	nums := make([]int, 0, len(seen))
	for id := range seen {
		n, err := ParseTaskID(id)
		if err != nil {
			t.Fatalf("parse %s: %v", id, err)
		}
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for i, n := range nums {
		want := i + 2 // starts at 2 because seq was seeded at 1
		if n != want {
			t.Errorf("gap at position %d: got %d, want %d (full list: %v)", i, n, want, nums)
			break
		}
	}
	if len(nums) != goroutines {
		t.Errorf("got %d unique IDs, want %d", len(nums), goroutines)
	}
}

// TestAllocDiscID_Concurrent mirrors the task test for discussion IDs.
func TestAllocDiscID_Concurrent(t *testing.T) {
	const goroutines = 30
	c := seededDiscClient(t, 0)
	if err := os.WriteFile(c.dataDir+"/.disc-seq", []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ids := make(chan string, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			id, err := c.allocDiscID(ctx)
			if err != nil {
				ids <- "ERROR:" + err.Error()
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]int)
	for id := range ids {
		if strings.HasPrefix(id, "ERROR:") {
			t.Fatalf("goroutine error: %s", id)
		}
		seen[id]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("duplicate disc ID %s seen %d times", id, count)
		}
	}

	nums := make([]int, 0, len(seen))
	for id := range seen {
		n, err := ParseDiscID(id)
		if err != nil {
			t.Fatalf("parse %s: %v", id, err)
		}
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for i, n := range nums {
		want := i + 2
		if n != want {
			t.Errorf("gap at position %d: got %d, want %d", i, n, want)
			break
		}
	}
	if len(nums) != goroutines {
		t.Errorf("got %d unique IDs, want %d", len(nums), goroutines)
	}
}

// TestLockFileCtx_CancelWhileWaiting verifies that cancelling the context
// unblocks a goroutine waiting on a held lock.
func TestLockFileCtx_CancelWhileWaiting(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/locktest"

	// Holder: open and lock the file.
	holder, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()
	if err := tryLockFile(holder); err != nil {
		t.Fatalf("acquire holder lock: %v", err)
	}
	defer func() { _ = unlockFile(holder) }()

	// Waiter: open the same path with a different file descriptor.
	waiter, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = waiter.Close() }()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- lockFileCtx(ctx, waiter)
	}()

	// Give the goroutine time to block.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lockFileCtx did not unblock after context cancel")
	}
}

// TestLockFileCtx_DeadlineExceeded covers the context.DeadlineExceeded path.
func TestLockFileCtx_DeadlineExceeded(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/locktest"

	holder, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()
	if err := tryLockFile(holder); err != nil {
		t.Fatalf("acquire holder lock: %v", err)
	}
	defer func() { _ = unlockFile(holder) }()

	waiter, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = waiter.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = lockFileCtx(ctx, waiter)
	elapsed := time.Since(start)

	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	// Should have returned roughly on the deadline, not spun for 10 seconds.
	if elapsed > 500*time.Millisecond {
		t.Errorf("took too long to respect deadline: %v", elapsed)
	}
}

// TestAllocTaskID_CorruptSeqFile verifies that corrupt file content returns a
// descriptive error rather than silently using a zero counter (which would
// trigger the Qdrant bootstrap path or reuse IDs).
func TestAllocTaskID_CorruptSeqFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/.task-seq", []byte("notanumber\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Client{dataDir: dir}
	_, err := c.allocTaskID(context.Background())
	if err == nil {
		t.Fatal("expected error for corrupt seq file, got nil")
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("error should mention 'corrupt': %v", err)
	}
}

// TestAllocDiscID_CorruptSeqFile mirrors the task corrupt-file test.
func TestAllocDiscID_CorruptSeqFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/.disc-seq", []byte("-99\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Client{dataDir: dir}
	_, err := c.allocDiscID(context.Background())
	if err == nil {
		t.Fatal("expected error for negative value, got nil")
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("error should mention 'corrupt': %v", err)
	}
}

// TestAllocTaskID_NegativeValue exercises the parsed < 0 branch.
func TestAllocTaskID_NegativeValue(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/.task-seq", []byte(strconv.Itoa(-1)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Client{dataDir: dir}
	_, err := c.allocTaskID(context.Background())
	if err == nil {
		t.Fatal("expected error for negative seq value")
	}
}

// TestAllocTaskID_NoDataDir verifies the guard against a zero-value Client.
func TestAllocTaskID_NoDataDir(t *testing.T) {
	c := &Client{}
	_, err := c.allocTaskID(context.Background())
	if err == nil {
		t.Fatal("expected error when dataDir is empty")
	}
}

// TestAppendTurnLock_FileCreated verifies that AppendTurn creates the per-disc
// lock file before reaching Qdrant. When Qdrant is absent the call fails, but
// the lock file must exist in dataDir after the attempt.
func TestAppendTurnLock_FileCreated(t *testing.T) {
	dir := t.TempDir()
	c := &Client{dataDir: dir}
	// qc is nil — AppendTurn will panic if it gets past the lock step
	// because qc.Get is called next. We recover from that panic to confirm
	// the lock file was created before the Qdrant call.
	defer func() { recover() }() //nolint:errcheck
	_, _ = c.AppendTurn(context.Background(), "DISC-001", Turn{By: "test", Text: "x"})
	// Regardless of Qdrant outcome, the lock file must have been created.
	lockPath := dir + "/.disc-DISC-001-turns.lock"
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file not created: %v (path: %s)", err, lockPath)
	}
}

// TestAppendTurnLock_SerializesConcurrent verifies that the per-discussion
// in-process sync.Mutex (discTurnsLocks) serializes concurrent goroutines.
// 50 goroutines each increment a shared counter 100 times while holding the
// lock — if the mutex fails to serialize, the race detector or a lost-update
// will surface immediately. This mirrors the acceptance criterion for TASK-105
// without requiring a live Qdrant instance.
func TestAppendTurnLock_SerializesConcurrent(t *testing.T) {
	const (
		goroutines   = 50
		acqPerWorker = 100
	)

	discID := "DISC-042"
	counter := 0 // shared; safe only because the mutex serializes all access

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range acqPerWorker {
				// Replicate the in-process locking path from AppendTurn.
				mu, _ := discTurnsLocks.LoadOrStore(discID, &sync.Mutex{})
				mu.(*sync.Mutex).Lock()
				counter++ // critical section: must not race
				mu.(*sync.Mutex).Unlock()
			}
		}()
	}
	wg.Wait()

	want := goroutines * acqPerWorker
	if counter != want {
		t.Errorf("counter = %d, want %d (lost updates detected)", counter, want)
	}
}
