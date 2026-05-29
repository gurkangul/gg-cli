package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/projectstate"
)

func TestTaskDoneHydrationGateAllowsUntaggedHumanSession(t *testing.T) {
	t.Setenv("GG_AGENT", "")
	t.Setenv("GG_ROLE", "")

	if rej := checkTaskDoneHydrationGate(t.TempDir(), "TASK-123", time.Now().UTC()); rej != nil {
		t.Fatalf("untagged human session should not require hydration proof, got %v", rej)
	}
}

func TestTaskDoneHydrationGateBlocksTaggedSessionWithoutFullGet(t *testing.T) {
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "master")

	rej := checkTaskDoneHydrationGate(t.TempDir(), "TASK-123", time.Now().UTC())
	if rej == nil {
		t.Fatal("expected tagged session without hydration proof to be blocked")
		return
	}
	if rej.Code != ExitVerifyFailed {
		t.Fatalf("expected ExitVerifyFailed, got %d", rej.Code)
	}
	if !strings.Contains(rej.Message, "missing durable task evidence") || !strings.Contains(rej.Message, "gg task get TASK-123") {
		t.Fatalf("message should explain missing durable evidence and hydration command, got %q", rej.Message)
	}
}

func TestTaskDoneHydrationGateAllowsRecentFullGet(t *testing.T) {
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "master")
	dir := t.TempDir()
	if err := projectstate.RecordHydration(dir, "task", "TASK-123"); err != nil {
		t.Fatalf("RecordHydration: %v", err)
	}

	if rej := checkTaskDoneHydrationGate(dir, "TASK-123", time.Now().UTC()); rej != nil {
		t.Fatalf("recent hydration proof should pass, got %v", rej)
	}
}

func TestTaskDoneHydrationGateRejectsStaleFullGet(t *testing.T) {
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "master")
	dir := t.TempDir()
	old := time.Now().UTC().Add(-2 * taskHydrationWindow).Format(time.RFC3339)
	if err := projectstate.Write(dir, projectstate.State{RecentHydrations: []projectstate.HydrationEntry{
		{TS: old, EntityType: "task", EntityID: "TASK-123"},
	}}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if rej := checkTaskDoneHydrationGate(dir, "TASK-123", time.Now().UTC()); rej == nil {
		t.Fatal("stale hydration proof should be rejected")
	}
}

func TestTaskHydrationGateBlocksOtherStateTransitions(t *testing.T) {
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "master")

	for _, action := range []string{"task block", "task ready-for-live"} {
		t.Run(action, func(t *testing.T) {
			rej := checkTaskHydrationGate(t.TempDir(), "TASK-123", action, time.Now().UTC())
			if rej == nil {
				t.Fatalf("expected %s without hydration proof to be blocked", action)
				return
			}
			if !strings.Contains(rej.Message, action) || !strings.Contains(rej.Message, "missing durable task evidence") || !strings.Contains(rej.Message, "gg task get TASK-123") {
				t.Fatalf("message should name action, missing evidence, and hydration command, got %q", rej.Message)
			}
		})
	}
}

func TestReadyForLiveHydrationGateMessageMentionsContextNotEnough(t *testing.T) {
	t.Setenv("GG_AGENT", "omo-slim")
	t.Setenv("GG_ROLE", "implementer")

	rej := checkTaskHydrationGate(t.TempDir(), "TASK-123", "task ready-for-live", time.Now().UTC())
	if rej == nil {
		t.Fatal("expected ready-for-live without hydration proof to be blocked")
	}
	if !strings.Contains(rej.Message, "before ready-for-live") {
		t.Fatalf("message should name ready-for-live, got %q", rej.Message)
	}
	if !strings.Contains(rej.Message, "gg context --for-task TASK-123") || !strings.Contains(rej.Message, "not enough") {
		t.Fatalf("message should clarify context alone is not enough, got %q", rej.Message)
	}
}

func TestBugHydrationGateBlocksTaggedSessionWithoutFullBugGet(t *testing.T) {
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "master")

	rej := checkBugHydrationGate(t.TempDir(), "BUG-058", "bug fix", time.Now().UTC())
	if rej == nil {
		t.Fatal("expected tagged session without bug hydration proof to be blocked")
		return
	}
	if rej.Code != ExitVerifyFailed {
		t.Fatalf("expected ExitVerifyFailed, got %d", rej.Code)
	}
	if !strings.Contains(rej.Message, "missing durable bug evidence") || !strings.Contains(rej.Message, "gg bug get BUG-058") || !strings.Contains(rej.Message, "gg bug triage BUG-058") {
		t.Fatalf("message should explain missing bug evidence and hydration commands, got %q", rej.Message)
	}
}

func TestBugHydrationGateAllowsRecentFullBugGet(t *testing.T) {
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "master")
	dir := t.TempDir()
	if err := projectstate.RecordHydration(dir, "bug", "BUG-058"); err != nil {
		t.Fatalf("RecordHydration: %v", err)
	}

	if rej := checkBugHydrationGate(dir, "BUG-058", "bug fix", time.Now().UTC()); rej != nil {
		t.Fatalf("recent bug hydration proof should pass, got %v", rej)
	}
}
