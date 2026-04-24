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
