package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
	"github.com/gurkangul/gg-cli/internal/store"
)

func TestSupervisorStatePath_SanitizesRole(t *testing.T) {
	runtimeDir := t.TempDir()
	root := filepath.Join(spawn.Dir(runtimeDir), "supervisor")

	cases := []struct {
		role string
	}{
		{role: "../developer"},
		{role: "dev/ops"},
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			p := supervisorStatePath(runtimeDir, tc.role)
			rel, err := filepath.Rel(root, p)
			if err != nil {
				t.Fatalf("Rel: %v", err)
			}
			if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
				t.Fatalf("path escaped supervisor root: role=%q path=%q rel=%q", tc.role, p, rel)
			}
			base := filepath.Base(p)
			if strings.Contains(base, "/") || strings.Contains(base, "..") {
				t.Fatalf("unsafe filename for role=%q: %q", tc.role, base)
			}
		})
	}
}

func TestSupervisorEligibleForSupervisor_TargetedRole(t *testing.T) {
	msg := store.Message{ToRole: "developer", Audience: ""}
	if !eligibleForSupervisor("developer", msg) {
		t.Fatal("expected targeted role message to be eligible")
	}
	if eligibleForSupervisor("qa", msg) {
		t.Fatal("expected non-target role message to be ineligible")
	}
}

func TestSupervisorEligibleForSupervisor_AllAudienceAgents(t *testing.T) {
	if !eligibleForSupervisor("developer", store.Message{ToRole: "all", Audience: "agents"}) {
		t.Fatal("expected all+agents message to be eligible")
	}
	if !eligibleForSupervisor("developer", store.Message{ToRole: "all", Audience: ""}) {
		t.Fatal("expected all+empty-audience message to be eligible")
	}
	if eligibleForSupervisor("developer", store.Message{ToRole: "all", Audience: "human"}) {
		t.Fatal("expected all+human message to be ineligible")
	}
}

func TestSupervisorProcessMessages_DuplicateSuppression(t *testing.T) {
	runtimeDir := t.TempDir()
	fake := terminal.NewFake()
	ctx := context.Background()
	id, err := fake.NewSplit(ctx, terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}
	if err := spawn.RegisterPane(runtimeDir, spawn.WorkerPane{
		SurfaceID: string(id),
		TaskID:    "TASK-383",
		Agent:     "gsd",
		SpawnedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RegisterPane: %v", err)
	}

	state := &supervisorState{Processed: map[string]bool{}}
	msg := store.Message{ID: "msg-1", ToRole: "developer", TaskID: "TASK-383", Content: "please proceed"}
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	processSupervisorMessages(ctx, cmd, runtimeDir, fake, "developer", state, []store.Message{msg, msg}, false)

	if !state.Processed[msg.ID] {
		t.Fatalf("expected message %s to be marked processed", msg.ID)
	}
	sent := 0
	for _, c := range fake.Calls {
		if c.Method == "Send" && c.Arg == msg.Content {
			sent++
		}
	}
	if sent != 1 {
		t.Fatalf("expected exactly one delivery send for duplicate message, got %d calls=%+v", sent, fake.Calls)
	}
}

func TestSupervisorDeliver_MissingPaneHandledWithClearError(t *testing.T) {
	runtimeDir := t.TempDir()
	fake := terminal.NewFake()
	ctx := context.Background()
	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetErr(stderr)
	cmd.SetOut(stdout)

	ok := deliverSupervisorMessage(ctx, cmd, runtimeDir, fake, store.Message{
		ID:      "msg-missing",
		ToRole:  "developer",
		TaskID:  "TASK-999",
		Content: "do thing",
	}, false)
	if !ok {
		t.Fatal("expected missing-pane path to be handled and marked processed")
	}
	if !strings.Contains(stderr.String(), "missing pane for task TASK-999") {
		t.Fatalf("expected clear missing-pane stderr, got: %q", stderr.String())
	}
}

func TestSupervisorDeliver_StalePanePruned(t *testing.T) {
	runtimeDir := t.TempDir()
	ctx := context.Background()
	fake := terminal.NewFake()
	id, err := fake.NewSplit(ctx, terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}
	if err := spawn.RegisterPane(runtimeDir, spawn.WorkerPane{
		SurfaceID: string(id),
		TaskID:    "TASK-383",
		Agent:     "gsd",
		SpawnedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RegisterPane: %v", err)
	}
	if err := fake.Close(ctx, id); err != nil {
		t.Fatalf("Close: %v", err)
	}

	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetErr(stderr)
	cmd.SetOut(stdout)

	ok := deliverSupervisorMessage(ctx, cmd, runtimeDir, fake, store.Message{
		ID:      "msg-stale",
		ToRole:  "developer",
		TaskID:  "TASK-383",
		Content: "nudge",
	}, false)
	if !ok {
		t.Fatal("expected stale-pane path to be handled and marked processed")
	}
	if !strings.Contains(stderr.String(), "stale pane") {
		t.Fatalf("expected stale-pane stderr, got: %q", stderr.String())
	}

	pane, err := spawn.FindPaneForTask(runtimeDir, "TASK-383")
	if err != nil {
		t.Fatalf("FindPaneForTask: %v", err)
	}
	if pane != nil {
		t.Fatalf("expected stale pane to be pruned, got %+v", *pane)
	}
}
