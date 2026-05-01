package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
)

func TestCheckWorkerPanesUpdatesStateFromScreens(t *testing.T) {
	ctx := context.Background()
	rt := t.TempDir()
	term := terminal.NewFake()

	idleID, err := term.NewSplit(ctx, terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit idle: %v", err)
	}
	busyID, err := term.NewSplit(ctx, terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit busy: %v", err)
	}
	missingID, err := term.NewSplit(ctx, terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit missing: %v", err)
	}
	if err := term.Close(ctx, missingID); err != nil {
		t.Fatalf("Close missing: %v", err)
	}

	term.SetScreen(idleID, []byte("TASK-001 complete\n> "))
	term.SetScreen(busyID, []byte("⠋ working on TASK-002"))

	registerTestPane(t, rt, idleID, "TASK-001")
	registerTestPane(t, rt, busyID, "TASK-002")
	registerTestPane(t, rt, missingID, "TASK-003")

	panes, err := spawn.ListPanes(rt)
	if err != nil {
		t.Fatalf("ListPanes before check: %v", err)
	}
	summary, err := checkWorkerPanesWithTerminal(ctx, rt, term, panes)
	if err != nil {
		t.Fatalf("checkWorkerPanesWithTerminal: %v", err)
	}
	if summary.Total != 3 || summary.Idle != 1 || summary.Working != 1 || summary.Missing != 1 {
		t.Fatalf("summary = %+v, want total=3 idle=1 working=1 missing=1", summary)
	}

	panes, err = spawn.ListPanes(rt)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	states := map[string]spawn.WorkerPane{}
	for _, p := range panes {
		states[p.TaskID] = p
	}
	if states["TASK-001"].State != spawn.WorkerStateIdle {
		t.Errorf("TASK-001 state = %q, want idle", states["TASK-001"].State)
	}
	if states["TASK-002"].State != spawn.WorkerStateWorking {
		t.Errorf("TASK-002 state = %q, want working", states["TASK-002"].State)
	}
	if states["TASK-001"].LastHeartbeat.IsZero() || states["TASK-002"].LastHeartbeat.IsZero() {
		t.Error("live panes should get LastHeartbeat updates")
	}
	if !states["TASK-003"].LastHeartbeat.IsZero() {
		t.Error("missing pane should not get LastHeartbeat update")
	}
}

func TestRecordHeartbeatWorkerModeSkipsPaneCheck(t *testing.T) {
	ctx := context.Background()
	rt := t.TempDir()

	registerTestPane(t, rt, "missing-surface", "TASK-009")

	hb, summary, err := recordHeartbeatAndCheckWorkers(ctx, rt, "worker-agent", false, "")
	if err != nil {
		t.Fatalf("recordHeartbeatAndCheckWorkers: %v", err)
	}
	if hb == nil || hb.Agent != "worker-agent" {
		t.Fatalf("heartbeat = %+v, want worker-agent", hb)
	}
	if summary.Total != 0 || summary.Working != 0 || summary.Idle != 0 || summary.Missing != 0 {
		t.Fatalf("summary = %+v, want zero summary when checks disabled", summary)
	}
}

func TestCheckWorkerPanesMarksPaneStalledAfterNudgeWithoutToolActivity(t *testing.T) {
	ctx := context.Background()
	rt := t.TempDir()
	term := terminal.NewFake()

	id, err := term.NewSplit(ctx, terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}
	nudge := "run gg task start TASK-367"
	screen := []byte("You: " + nudge + "\nGSD: Proceeding now.\n")
	term.SetScreen(id, screen)
	if err := spawn.RegisterPane(rt, spawn.WorkerPane{
		SurfaceID:           string(id),
		TaskID:              "TASK-367",
		Agent:               "gsd",
		SpawnedAt:           time.Now().Add(-10 * time.Minute).UTC(),
		State:               spawn.WorkerStateWorking,
		LastNudgeAt:         time.Now().Add(-3 * time.Minute).UTC(),
		LastNudgeText:       nudge,
		LastNudgeScreenHash: spawn.ScreenHash(screen),
	}); err != nil {
		t.Fatalf("RegisterPane: %v", err)
	}

	panes, err := spawn.ListPanes(rt)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	summary, err := checkWorkerPanesWithTerminal(ctx, rt, term, panes)
	if err != nil {
		t.Fatalf("checkWorkerPanesWithTerminal: %v", err)
	}
	if summary.Stalled != 1 || summary.Working != 0 {
		t.Fatalf("summary = %+v, want stalled=1 working=0", summary)
	}

	panes, err = spawn.ListPanes(rt)
	if err != nil {
		t.Fatalf("ListPanes after: %v", err)
	}
	if panes[0].State != spawn.WorkerStateWaiting {
		t.Fatalf("state = %q, want waiting-on-master", panes[0].State)
	}
	if panes[0].StalledNotifiedAt.IsZero() {
		t.Fatal("StalledNotifiedAt should be set")
	}
}

func TestCheckWorkerPanesDoesNotMarkStalledWhenToolActivityAppears(t *testing.T) {
	ctx := context.Background()
	rt := t.TempDir()
	term := terminal.NewFake()

	id, err := term.NewSplit(ctx, terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}
	nudge := "run gg task start TASK-367"
	screen := []byte("You: " + nudge + "\nTool bash running\n")
	term.SetScreen(id, screen)
	if err := spawn.RegisterPane(rt, spawn.WorkerPane{
		SurfaceID:     string(id),
		TaskID:        "TASK-367",
		Agent:         "gsd",
		SpawnedAt:     time.Now().Add(-10 * time.Minute).UTC(),
		State:         spawn.WorkerStateWorking,
		LastNudgeAt:   time.Now().Add(-3 * time.Minute).UTC(),
		LastNudgeText: nudge,
	}); err != nil {
		t.Fatalf("RegisterPane: %v", err)
	}

	panes, err := spawn.ListPanes(rt)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	summary, err := checkWorkerPanesWithTerminal(ctx, rt, term, panes)
	if err != nil {
		t.Fatalf("checkWorkerPanesWithTerminal: %v", err)
	}
	if summary.Stalled != 0 || summary.Working != 1 {
		t.Fatalf("summary = %+v, want stalled=0 working=1", summary)
	}
}

func registerTestPane(t *testing.T, rt string, id terminal.SurfaceID, taskID string) {
	t.Helper()
	if err := spawn.RegisterPane(rt, spawn.WorkerPane{
		SurfaceID: string(id),
		TaskID:    taskID,
		Agent:     "gsd",
		SpawnedAt: time.Now().UTC(),
		State:     spawn.WorkerStateWorking,
	}); err != nil {
		t.Fatalf("RegisterPane %s: %v", taskID, err)
	}
}
