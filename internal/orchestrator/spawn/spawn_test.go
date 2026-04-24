package spawn_test

import (
	"errors"
	"os"
	"path/filepath"
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

	// Write a heartbeat then manually backdate the file.
	if err := spawn.WriteHeartbeat(dir, "claude-code"); err != nil {
		t.Fatal(err)
	}

	// Overwrite with a stale timestamp directly.
	staleTime := time.Now().Add(-10 * time.Minute)
	hbPath := filepath.Join(spawn.Dir(dir), spawn.HeartbeatFile)

	// Round-trip: read, adjust UpdatedAt, write back.
	hb, err := spawn.ReadHeartbeat(dir)
	if err != nil {
		t.Fatal(err)
	}
	hb.UpdatedAt = staleTime
	b, err := os.ReadFile(hbPath)
	_ = b
	_ = err

	// Write a fresh file with stale timestamp via the unexported json path.
	// We test via the public API by mutating the file.
	content := `{"pid":1,"agent":"claude-code","updated_at":"` + staleTime.UTC().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(hbPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	alive, reason := spawn.IsMasterAlive(dir)
	if alive {
		t.Errorf("expected stale heartbeat to report not-alive; reason=%s", reason)
	}
	_ = hb
}

func TestWriteReadSession(t *testing.T) {
	dir := t.TempDir()

	s := &spawn.QueueSession{
		Agent:     "claude",
		StartedAt: time.Now().UTC(),
		Completed: []string{"TASK-001"},
		Skipped:   []string{"TASK-002"},
		Current:   "TASK-003",
	}
	if err := spawn.WriteSession(dir, s); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}

	got, err := spawn.ReadSession(dir)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if got.Agent != s.Agent {
		t.Errorf("Agent = %q, want %q", got.Agent, s.Agent)
	}
	if len(got.Completed) != 1 || got.Completed[0] != "TASK-001" {
		t.Errorf("Completed = %v, want [TASK-001]", got.Completed)
	}
	if got.Current != "TASK-003" {
		t.Errorf("Current = %q, want TASK-003", got.Current)
	}
}

func TestReadSession_NotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := spawn.ReadSession(dir)
	if !errors.Is(err, spawn.ErrNoSession) {
		t.Errorf("got %v, want ErrNoSession", err)
	}
}

func TestRegisterRemoveWorker(t *testing.T) {
	dir := t.TempDir()

	w := spawn.WorkerEntry{
		SurfaceID: "surface-abc",
		TaskID:    "TASK-042",
		Agent:     "claude",
		SpawnedAt: time.Now().UTC(),
	}
	if err := spawn.RegisterWorker(dir, w); err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	workers, err := spawn.ListWorkers(dir)
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(workers))
	}
	if workers[0].TaskID != "TASK-042" {
		t.Errorf("TaskID = %q, want TASK-042", workers[0].TaskID)
	}

	if err := spawn.RemoveWorker(dir, "TASK-042"); err != nil {
		t.Fatalf("RemoveWorker: %v", err)
	}

	workers, err = spawn.ListWorkers(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 0 {
		t.Errorf("expected 0 workers after removal, got %d", len(workers))
	}
}

func TestListWorkers_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	workers, err := spawn.ListWorkers(dir)
	if err != nil {
		t.Fatal(err)
	}
	if workers != nil {
		t.Errorf("expected nil slice for empty dir, got %v", workers)
	}
}

func TestRemoveWorker_NotExist(t *testing.T) {
	dir := t.TempDir()
	// Should not error when file doesn't exist.
	if err := spawn.RemoveWorker(dir, "TASK-999"); err != nil {
		t.Errorf("RemoveWorker non-existent: %v", err)
	}
}

func TestDir(t *testing.T) {
	got := spawn.Dir("/tmp/runtime")
	if got != "/tmp/runtime/spawn" {
		t.Errorf("Dir = %q, want /tmp/runtime/spawn", got)
	}
}
