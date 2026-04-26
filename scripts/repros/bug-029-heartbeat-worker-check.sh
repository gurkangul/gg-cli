#!/bin/sh
set -eu

cat > cmd/bug029_repro_test.go <<'GOEOF'
package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
)

func TestBUG029HeartbeatChecksRegisteredWorkers(t *testing.T) {
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
	term.SetScreen(idleID, []byte("waiting for input\n> "))
	term.SetScreen(busyID, []byte("thinking..."))

	for _, tc := range []struct {
		id     terminal.SurfaceID
		taskID string
	}{
		{id: idleID, taskID: "TASK-001"},
		{id: busyID, taskID: "TASK-002"},
	} {
		if err := spawn.RegisterPane(rt, spawn.WorkerPane{
			SurfaceID: string(tc.id),
			TaskID:    tc.taskID,
			Agent:     "gsd",
			SpawnedAt: time.Now().UTC(),
			State:     spawn.WorkerStateWorking,
		}); err != nil {
			t.Fatalf("RegisterPane %s: %v", tc.taskID, err)
		}
	}

	panes, err := spawn.ListPanes(rt)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	summary, err := checkWorkerPanesWithTerminal(ctx, rt, term, panes)
	if err != nil {
		t.Fatalf("checkWorkerPanesWithTerminal: %v", err)
	}
	if summary.Total != 2 || summary.Idle != 1 || summary.Working != 1 || summary.Missing != 0 {
		t.Fatalf("summary = %+v, want total=2 idle=1 working=1 missing=0", summary)
	}
}
GOEOF
trap 'rm -f cmd/bug029_repro_test.go' EXIT

go test ./cmd -run TestBUG029HeartbeatChecksRegisteredWorkers -count=1
