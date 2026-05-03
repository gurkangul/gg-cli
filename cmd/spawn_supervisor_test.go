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
	msg := store.Message{ID: "msg-1", ToRole: "developer", TaskID: "TASK-383", Content: "please proceed\nAC-1: complete handoff\n`gg task start TASK-383`"}
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	processSupervisorMessages(ctx, cmd, runtimeDir, fake, "developer", state, []store.Message{msg, msg}, false)

	if !state.Processed[msg.ID] {
		t.Fatalf("expected message %s to be marked processed", msg.ID)
	}
	sent := 0
	for _, c := range fake.Calls {
		if c.Method != "Send" {
			continue
		}
		if strings.Contains(c.Arg, msg.Content) &&
			strings.Contains(c.Arg, "Task ID: TASK-383") &&
			strings.Contains(c.Arg, "Required next command: gg task start TASK-383") {
			sent++
		}
	}
	if sent != 1 {
		t.Fatalf("expected exactly one delivery send for duplicate message, got %d calls=%+v", sent, fake.Calls)
	}
}

func TestSupervisorProcessMessages_StartupIgnoresPreexistingDeliversNew(t *testing.T) {
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
	pre := store.Message{ID: "msg-pre", ToRole: "developer", TaskID: "TASK-383", Content: "old backlog\nAC-0: prior"}
	post := store.Message{ID: "msg-post", ToRole: "developer", TaskID: "TASK-383", Content: "new message\nAC-1: ship fix\n`gg task start TASK-383`"}
	seedSupervisorProcessed(state, []store.Message{pre})

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	processSupervisorMessages(ctx, cmd, runtimeDir, fake, "developer", state, []store.Message{pre, post}, false)

	if !state.Processed[pre.ID] || !state.Processed[post.ID] {
		t.Fatalf("expected both pre and post messages marked processed, got %+v", state.Processed)
	}
	preSent := 0
	postSent := 0
	for _, c := range fake.Calls {
		if c.Method != "Send" {
			continue
		}
		if strings.Contains(c.Arg, pre.Content) &&
			strings.Contains(c.Arg, "Task ID: TASK-383") {
			preSent++
		}
		if strings.Contains(c.Arg, post.Content) &&
			strings.Contains(c.Arg, "Task ID: TASK-383") &&
			strings.Contains(c.Arg, "Required next command: gg task start TASK-383") {
			postSent++
		}
	}
	if preSent != 0 {
		t.Fatalf("expected pre-existing message to be ignored, sent=%d calls=%+v", preSent, fake.Calls)
	}
	if postSent != 1 {
		t.Fatalf("expected post-start message delivered once, sent=%d calls=%+v", postSent, fake.Calls)
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

	status := deliverSupervisorMessage(ctx, cmd, runtimeDir, fake, store.Message{
		ID:      "msg-missing",
		ToRole:  "developer",
		TaskID:  "TASK-999",
		Content: "do thing",
	}, false)
	if status.Status != "missing-pane" {
		t.Fatalf("expected missing-pane status, got %+v", status)
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

	status := deliverSupervisorMessage(ctx, cmd, runtimeDir, fake, store.Message{
		ID:      "msg-stale",
		ToRole:  "developer",
		TaskID:  "TASK-383",
		Content: "nudge",
	}, false)
	if status.Status != "stale-pruned" {
		t.Fatalf("expected stale-pruned status, got %+v", status)
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

func TestSupervisorDeliver_FormatsActionablePrompt(t *testing.T) {
	runtimeDir := t.TempDir()
	ctx := context.Background()
	fake := terminal.NewFake()
	id, err := fake.NewSplit(ctx, terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}
	if err := spawn.RegisterPane(runtimeDir, spawn.WorkerPane{
		SurfaceID: string(id),
		TaskID:    "TASK-385",
		Agent:     "gsd",
		SpawnedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RegisterPane: %v", err)
	}

	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetErr(stderr)
	cmd.SetOut(stdout)

	content := "please pick this up\nAC-1: wake worker\n`gg task start TASK-385`"
	status := deliverSupervisorMessage(ctx, cmd, runtimeDir, fake, store.Message{
		ID:      "msg-actionable",
		ToRole:  "developer",
		TaskID:  "TASK-385",
		Content: content,
	}, false)
	if status.Status != "delivered" {
		t.Fatalf("expected delivered status, got %+v", status)
	}

	sendPayload := ""
	for _, c := range fake.Calls {
		if c.Method == "Send" {
			sendPayload = c.Arg
		}
	}
	if sendPayload == "" {
		t.Fatalf("expected send payload, calls=%+v", fake.Calls)
	}
	if !strings.Contains(sendPayload, "Task ID: TASK-385") {
		t.Fatalf("expected task id in payload, got: %q", sendPayload)
	}
	if !strings.Contains(sendPayload, "Acceptance criteria: AC-1: wake worker") {
		t.Fatalf("expected acceptance criteria in payload, got: %q", sendPayload)
	}
	if !strings.Contains(sendPayload, "Required next command: gg task start TASK-385") {
		t.Fatalf("expected required next command in payload, got: %q", sendPayload)
	}
}

func TestSupervisorProcessMessages_RecordsDeliverySuccessAndFailure(t *testing.T) {
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

	state := &supervisorState{Processed: map[string]bool{}, Delivery: map[string]supervisorDeliveryStatus{}}
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	delivered := store.Message{ID: "msg-ok", ToRole: "developer", TaskID: "TASK-383", Content: "go\nAC-1: x\n`gg task start TASK-383`"}
	missing := store.Message{ID: "msg-missing", ToRole: "developer", TaskID: "TASK-999", Content: "go"}

	processSupervisorMessages(ctx, cmd, runtimeDir, fake, "developer", state, []store.Message{delivered, missing}, false)

	if got := state.Delivery[delivered.ID].Status; got != "delivered" {
		t.Fatalf("expected delivered status for %s, got %q", delivered.ID, got)
	}
	if got := state.Delivery[missing.ID].Status; got != "missing-pane" {
		t.Fatalf("expected missing-pane status for %s, got %q", missing.ID, got)
	}
}

func TestSupervisorActionablePrompt_FallbacksWhenDetailsMissing(t *testing.T) {
	prompt := supervisorActionablePrompt(store.Message{Content: "", TaskID: ""})
	if !strings.Contains(prompt, "Task ID: (none)") {
		t.Fatalf("expected fallback task id, got: %q", prompt)
	}
	if !strings.Contains(prompt, "Acceptance criteria: not provided") {
		t.Fatalf("expected fallback acceptance criteria, got: %q", prompt)
	}
	if !strings.Contains(prompt, "Required next command:") {
		t.Fatalf("expected required next command line, got: %q", prompt)
	}
}
