package spawn_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
)

func TestWriteReadHeartbeat(t *testing.T) {
	dir := t.TempDir()

	if err := spawn.WriteHeartbeat(dir, "claude-code"); err != nil {
		t.Fatalf("WriteHeartbeat: %v", err)
	}

	hb, err := spawn.ReadHeartbeat(dir)
	if err != nil {
		t.Fatalf("ReadHeartbeat: %v", err)
	}
	if hb.Agent != "claude-code" {
		t.Errorf("agent = %q, want %q", hb.Agent, "claude-code")
	}
	if hb.PID == 0 {
		t.Error("PID should be non-zero")
	}
	if hb.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

func TestReadHeartbeat_NotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := spawn.ReadHeartbeat(dir)
	if !errors.Is(err, spawn.ErrNoHeartbeat) {
		t.Errorf("got %v, want ErrNoHeartbeat", err)
	}
}

func TestIsMasterAlive_Fresh(t *testing.T) {
	dir := t.TempDir()
	if err := spawn.WriteHeartbeat(dir, "claude-code"); err != nil {
		t.Fatal(err)
	}

	alive, reason := spawn.IsMasterAlive(dir)
	if !alive {
		t.Errorf("expected alive, got reason: %s", reason)
	}
}

func TestIsMasterAlive_NoFile(t *testing.T) {
	dir := t.TempDir()
	alive, reason := spawn.IsMasterAlive(dir)
	if alive {
		t.Error("expected not alive when no heartbeat file")
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestIsMasterAlive_Stale(t *testing.T) {
	dir := t.TempDir()

	if err := spawn.WriteHeartbeat(dir, "claude-code"); err != nil {
		t.Fatal(err)
	}

	staleTime := time.Now().Add(-10 * time.Minute)
	hbPath := filepath.Join(spawn.Dir(dir), spawn.HeartbeatFile)

	hb, err := spawn.ReadHeartbeat(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = hb

	content := `{"pid":1,"agent":"claude-code","updated_at":"` + staleTime.UTC().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(hbPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	alive, reason := spawn.IsMasterAlive(dir)
	if alive {
		t.Errorf("expected stale heartbeat to report not-alive; reason=%s", reason)
	}
}

func TestWriteReadQueue(t *testing.T) {
	dir := t.TempDir()

	s := &spawn.QueueSession{
		Agent:       "claude",
		StartedAt:   time.Now().UTC(),
		Completed:   []string{"TASK-001"},
		Skipped:     []string{"TASK-002"},
		CurrentTask: "TASK-003",
	}
	if err := spawn.WriteQueue(dir, s); err != nil {
		t.Fatalf("WriteQueue: %v", err)
	}

	got, err := spawn.ReadQueue(dir)
	if err != nil {
		t.Fatalf("ReadQueue: %v", err)
	}
	if got.Agent != s.Agent {
		t.Errorf("Agent = %q, want %q", got.Agent, s.Agent)
	}
	if len(got.Completed) != 1 || got.Completed[0] != "TASK-001" {
		t.Errorf("Completed = %v, want [TASK-001]", got.Completed)
	}
	if got.CurrentTask != "TASK-003" {
		t.Errorf("CurrentTask = %q, want TASK-003", got.CurrentTask)
	}
}

func TestReadQueue_NotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := spawn.ReadQueue(dir)
	if !errors.Is(err, spawn.ErrNoQueue) {
		t.Errorf("got %v, want ErrNoQueue", err)
	}
}

func TestDeleteQueue(t *testing.T) {
	dir := t.TempDir()
	s := &spawn.QueueSession{Agent: "claude", StartedAt: time.Now().UTC()}
	if err := spawn.WriteQueue(dir, s); err != nil {
		t.Fatal(err)
	}
	if err := spawn.DeleteQueue(dir); err != nil {
		t.Fatalf("DeleteQueue: %v", err)
	}
	_, err := spawn.ReadQueue(dir)
	if !errors.Is(err, spawn.ErrNoQueue) {
		t.Errorf("expected ErrNoQueue after delete, got %v", err)
	}
}

func TestRegisterRemovePane(t *testing.T) {
	dir := t.TempDir()

	w := spawn.WorkerPane{
		SurfaceID: "surface-abc",
		TaskID:    "TASK-042",
		Agent:     "claude",
		SpawnedAt: time.Now().UTC(),
	}
	if err := spawn.RegisterPane(dir, w); err != nil {
		t.Fatalf("RegisterPane: %v", err)
	}

	panes, err := spawn.ListPanes(dir)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	if len(panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(panes))
	}
	if panes[0].TaskID != "TASK-042" {
		t.Errorf("TaskID = %q, want TASK-042", panes[0].TaskID)
	}

	if err := spawn.RemovePane(dir, "TASK-042"); err != nil {
		t.Fatalf("RemovePane: %v", err)
	}

	panes, err = spawn.ListPanes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 0 {
		t.Errorf("expected 0 panes after removal, got %d", len(panes))
	}
}

func TestRegisterPane_Idempotent(t *testing.T) {
	dir := t.TempDir()

	w := spawn.WorkerPane{SurfaceID: "pane-1", TaskID: "TASK-001", Agent: "claude", SpawnedAt: time.Now().UTC()}
	if err := spawn.RegisterPane(dir, w); err != nil {
		t.Fatal(err)
	}
	// Re-register same task ID — should replace, not duplicate.
	w2 := spawn.WorkerPane{SurfaceID: "pane-2", TaskID: "TASK-001", Agent: "claude", SpawnedAt: time.Now().UTC()}
	if err := spawn.RegisterPane(dir, w2); err != nil {
		t.Fatal(err)
	}

	panes, err := spawn.ListPanes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 1 {
		t.Errorf("expected 1 pane after idempotent register, got %d", len(panes))
	}
	if panes[0].SurfaceID != "pane-2" {
		t.Errorf("SurfaceID = %q, want pane-2", panes[0].SurfaceID)
	}
}

func TestListPanes_Empty(t *testing.T) {
	dir := t.TempDir()
	panes, err := spawn.ListPanes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if panes != nil {
		t.Errorf("expected nil slice for empty dir, got %v", panes)
	}
}

func TestRemovePane_NotExist(t *testing.T) {
	dir := t.TempDir()
	if err := spawn.RemovePane(dir, "TASK-999"); err != nil {
		t.Errorf("RemovePane non-existent: %v", err)
	}
}

func TestDir(t *testing.T) {
	got := spawn.Dir("/tmp/runtime")
	if got != "/tmp/runtime/spawn" {
		t.Errorf("Dir = %q, want /tmp/runtime/spawn", got)
	}
}

// TestAdvanceSentinel_RoundTrip verifies write → read → consume lifecycle.
func TestAdvanceSentinel_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Write sentinel.
	if err := spawn.WriteAdvanceSentinel(dir, "TASK-042", "surface-1", "abc123"); err != nil {
		t.Fatalf("WriteAdvanceSentinel: %v", err)
	}

	// Read without consuming.
	s, err := spawn.ReadAdvanceSentinel(dir, "TASK-042")
	if err != nil {
		t.Fatalf("ReadAdvanceSentinel: %v", err)
	}
	if s.TaskID != "TASK-042" {
		t.Errorf("TaskID = %q, want TASK-042", s.TaskID)
	}
	if s.CommitSHA != "abc123" {
		t.Errorf("CommitSHA = %q, want abc123", s.CommitSHA)
	}
	if s.SurfaceID != "surface-1" {
		t.Errorf("SurfaceID = %q, want surface-1", s.SurfaceID)
	}
	if s.WrittenAt.IsZero() {
		t.Error("WrittenAt should be set")
	}

	// Consume — should return the sentinel and remove the .done file.
	consumed, err := spawn.ConsumeAdvanceSentinel(dir, "TASK-042")
	if err != nil {
		t.Fatalf("ConsumeAdvanceSentinel: %v", err)
	}
	if consumed == nil {
		t.Fatal("expected sentinel, got nil")
	}
	if consumed.CommitSHA != "abc123" {
		t.Errorf("consumed.CommitSHA = %q, want abc123", consumed.CommitSHA)
	}

	// Second consume should return nil (sentinel is gone / consumed).
	again, err := spawn.ConsumeAdvanceSentinel(dir, "TASK-042")
	if err != nil {
		t.Fatalf("second ConsumeAdvanceSentinel: %v", err)
	}
	if again != nil {
		t.Errorf("expected nil on second consume, got %+v", again)
	}
}

// TestAdvanceSentinel_Idempotent verifies write is safe to call twice (amend).
func TestAdvanceSentinel_Idempotent(t *testing.T) {
	dir := t.TempDir()

	if err := spawn.WriteAdvanceSentinel(dir, "TASK-001", "surf-1", "sha-v1"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := spawn.WriteAdvanceSentinel(dir, "TASK-001", "surf-1", "sha-v2"); err != nil {
		t.Fatalf("second write: %v", err)
	}

	s, err := spawn.ReadAdvanceSentinel(dir, "TASK-001")
	if err != nil {
		t.Fatalf("ReadAdvanceSentinel: %v", err)
	}
	if s.CommitSHA != "sha-v2" {
		t.Errorf("expected sha-v2 after idempotent write, got %q", s.CommitSHA)
	}
}

// TestAdvanceSentinel_NoFile verifies ErrNoSentinel when absent.
func TestAdvanceSentinel_NoFile(t *testing.T) {
	dir := t.TempDir()
	_, err := spawn.ReadAdvanceSentinel(dir, "TASK-999")
	if !errors.Is(err, spawn.ErrNoSentinel) {
		t.Errorf("got %v, want ErrNoSentinel", err)
	}
}

// TestConsumeAdvanceSentinel_NilWhenAbsent verifies consume returns nil, nil when no sentinel.
func TestConsumeAdvanceSentinel_NilWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	s, err := spawn.ConsumeAdvanceSentinel(dir, "TASK-404")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != nil {
		t.Errorf("expected nil sentinel when absent, got %+v", s)
	}
}

// TestConsumeAdvanceSentinel_Concurrent verifies that two racing heartbeat
// ticks (goroutines) can only consume a sentinel once — the atomic rename
// guarantees exactly one winner and one nil return.
func TestConsumeAdvanceSentinel_Concurrent(t *testing.T) {
	dir := t.TempDir()
	if err := spawn.WriteAdvanceSentinel(dir, "TASK-777", "surf-A", "sha-concurrent"); err != nil {
		t.Fatalf("WriteAdvanceSentinel: %v", err)
	}

	const workers = 8
	results := make([]*spawn.AdvanceSentinel, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = spawn.ConsumeAdvanceSentinel(dir, "TASK-777")
		}(i)
	}
	wg.Wait()

	// Exactly one goroutine should have won the rename and received the sentinel.
	wins := 0
	for i, s := range results {
		if errs[i] != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, errs[i])
		}
		if s != nil {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("expected exactly 1 consumer to win, got %d", wins)
	}
}

// TestLockFilePath verifies the lock file path helper.
func TestLockFilePath(t *testing.T) {
	got := spawn.LockFilePath("/tmp/rt", "TASK-042")
	want := "/tmp/rt/spawn/TASK-042.lock"
	if got != want {
		t.Errorf("LockFilePath = %q, want %q", got, want)
	}
	if spawn.LockFilePath("/tmp/rt", "") != "" {
		t.Error("empty taskID should return empty string")
	}
}
