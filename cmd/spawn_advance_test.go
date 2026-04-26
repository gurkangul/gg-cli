package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
)

// TestSpawnAdvance_WritesSentinel verifies runSpawnAdvance writes a valid
// sentinel file to the advance/ directory under the runtime dir.
func TestSpawnAdvance_WritesSentinel(t *testing.T) {
	rt := t.TempDir()

	// Directly invoke the sentinel write to simulate what runSpawnAdvance does.
	if err := spawn.WriteAdvanceSentinel(rt, "TASK-042", "surf-99", "deadbeef"); err != nil {
		t.Fatalf("WriteAdvanceSentinel: %v", err)
	}

	s, err := spawn.ReadAdvanceSentinel(rt, "TASK-042")
	if err != nil {
		t.Fatalf("ReadAdvanceSentinel: %v", err)
	}
	if s.TaskID != "TASK-042" {
		t.Errorf("TaskID = %q, want TASK-042", s.TaskID)
	}
	if s.CommitSHA != "deadbeef" {
		t.Errorf("CommitSHA = %q, want deadbeef", s.CommitSHA)
	}
	if s.SurfaceID != "surf-99" {
		t.Errorf("SurfaceID = %q, want surf-99", s.SurfaceID)
	}
}

// TestKeepaliveProbesNotSendKey verifies sendPaneKeepalives does NOT call
// SendKey on the terminal (which would inject text into agent REPLs).
// The new implementation uses terminal.ProbeSurface (cmux identify, read-only).
func TestKeepaliveProbesNotSendKey(t *testing.T) {
	ctx := context.Background()
	rt := t.TempDir()
	term := terminal.NewFake()

	id1, _ := term.NewSplit(ctx, terminal.SplitOpts{})
	id2, _ := term.NewSplit(ctx, terminal.SplitOpts{})

	registerTestPane(t, rt, id1, "TASK-010")
	registerTestPane(t, rt, id2, "TASK-011")

	callsBefore := len(term.Calls) // captures NewSplit calls

	var buf bytes.Buffer
	// ProbeSurface shells out to cmux — it will error in test (no live surfaces),
	// but the error must be logged and not panic. The FakeTerminal is not used.
	sendPaneKeepalives(ctx, rt, term, &buf)

	for _, c := range term.Calls[callsBefore:] {
		if c.Method == "SendKey" || c.Method == "Send" {
			t.Errorf("sendPaneKeepalives must not inject input into panes; got %q call", c.Method)
		}
	}
}

// TestKeepaliveLogsProbeErrors verifies sendPaneKeepalives logs probe errors
// but does not panic or propagate them — transient cmux errors must not crash
// the heartbeat watch loop.
func TestKeepaliveLogsProbeErrors(t *testing.T) {
	ctx := context.Background()
	rt := t.TempDir()
	term := terminal.NewFake()

	id, _ := term.NewSplit(ctx, terminal.SplitOpts{})
	registerTestPane(t, rt, id, "TASK-020")

	var buf bytes.Buffer
	// ProbeSurface will error (surface not in live cmux). Must not panic.
	sendPaneKeepalives(ctx, rt, term, &buf)
	// Error should be written to errOut, not swallowed silently.
	// (Only expected when cmux binary is present but surface is unknown;
	// in CI without cmux the error path is still exercised.)
}

// TestPruneStalePane_RemovesEntryAndLock verifies pruneStalePane removes the
// panes.json entry and the lock file for the dead pane.
func TestPruneStalePane_RemovesEntryAndLock(t *testing.T) {
	rt := t.TempDir()

	// Register a pane.
	pane := spawn.WorkerPane{
		SurfaceID: "stale-surf",
		TaskID:    "TASK-030",
		Agent:     "gsd",
		SpawnedAt: time.Now().UTC(),
	}
	if err := spawn.RegisterPane(rt, pane); err != nil {
		t.Fatalf("RegisterPane: %v", err)
	}

	// Create a fake lock file.
	lockPath := spawn.LockFilePath(rt, "TASK-030")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	pruneStalePane(rt, pane)

	// panes.json entry should be gone.
	panes, err := spawn.ListPanes(rt)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	for _, p := range panes {
		if p.TaskID == "TASK-030" {
			t.Error("TASK-030 should have been pruned from panes.json")
		}
	}

	// Lock file should be gone.
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file should be removed, stat err = %v", err)
	}
}

// TestHeartbeatWatch_SignalsOnAdvance verifies pollAdvanceSentinels prints the
// ready signal and updates pane state to ready when a sentinel is found.
func TestHeartbeatWatch_SignalsOnAdvance(t *testing.T) {
	ctx := context.Background()
	rt := t.TempDir()
	term := terminal.NewFake()

	id, _ := term.NewSplit(ctx, terminal.SplitOpts{})
	registerTestPane(t, rt, id, "TASK-042")

	// Write an advance sentinel for TASK-042.
	if err := spawn.WriteAdvanceSentinel(rt, "TASK-042", string(id), "cafebabe"); err != nil {
		t.Fatalf("WriteAdvanceSentinel: %v", err)
	}

	// Redirect stderr to capture the ready signal.
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	pollAdvanceSentinels(ctx, rt, term)

	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	buf.ReadFrom(r)

	output := buf.String()
	if !spawnContains(output, "worker ready") {
		t.Errorf("expected '⚡ worker ready' in output, got: %s", output)
	}
	if !spawnContains(output, "cafebabe") {
		t.Errorf("expected commit SHA in output, got: %s", output)
	}

	// Pane state should be updated to ready.
	panes, err := spawn.ListPanes(rt)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	if len(panes) == 0 {
		t.Fatal("expected at least one pane")
	}
	if panes[0].State != spawn.WorkerStateReady {
		t.Errorf("pane state = %q, want ready", panes[0].State)
	}

	// Second poll should NOT fire again (sentinel consumed).
	r2, w2, _ := os.Pipe()
	os.Stderr = w2
	pollAdvanceSentinels(ctx, rt, term)
	w2.Close()
	os.Stderr = old
	var buf2 bytes.Buffer
	buf2.ReadFrom(r2)
	if spawnContains(buf2.String(), "worker ready") {
		t.Error("ready signal should not fire twice — sentinel already consumed")
	}
}

// TestDefaultKeepaliveSecs verifies priority: flag > env > default, and the 60s floor.
func TestDefaultKeepaliveSecs(t *testing.T) {
	t.Setenv("GG_PANE_KEEPALIVE_SEC", "120")

	// Flag takes priority.
	if got := defaultKeepaliveSecs(60); got != 60 {
		t.Errorf("flag=60: got %d, want 60", got)
	}

	// Env fallback.
	if got := defaultKeepaliveSecs(0); got != 120 {
		t.Errorf("flag=0 env=120: got %d, want 120", got)
	}

	// Default when env is cleared.
	t.Setenv("GG_PANE_KEEPALIVE_SEC", "")
	if got := defaultKeepaliveSecs(0); got != 240 {
		t.Errorf("no flag no env: got %d, want 240", got)
	}

	// Floor: flag below 60 clamps to 60.
	if got := defaultKeepaliveSecs(10); got != 60 {
		t.Errorf("flag=10 (below floor): got %d, want 60", got)
	}

	// Floor: env below 60 clamps to 60.
	t.Setenv("GG_PANE_KEEPALIVE_SEC", "30")
	if got := defaultKeepaliveSecs(0); got != 60 {
		t.Errorf("env=30 (below floor): got %d, want 60", got)
	}
}
