package cmd

import (
	"testing"
)

// TestSpawnAgentDefault verifies fallback behaviour for the agent default.
func TestSpawnAgentDefault_Fallback(t *testing.T) {
	t.Setenv("GG_SPAWN_AGENT", "")
	if got := spawnAgentDefault(); got != "gsd" {
		t.Errorf("spawnAgentDefault() = %q, want %q", got, "gsd")
	}
}

func TestSpawnAgentDefault_EnvOverride(t *testing.T) {
	t.Setenv("GG_SPAWN_AGENT", "codex")
	if got := spawnAgentDefault(); got != "codex" {
		t.Errorf("spawnAgentDefault() = %q, want %q", got, "codex")
	}
}

// TestBuildWorkerEnv verifies that task ID and agent are always exported.
func TestBuildWorkerEnv_TaskID(t *testing.T) {
	t.Setenv("GG_AGENT", "claude-code")
	t.Setenv("GG_ROLE", "developer")

	env := buildWorkerEnv("TASK-042", nil)

	hasAgent := false
	hasTask := false
	hasRole := false
	for _, e := range env {
		if e == "GG_AGENT=claude-code" {
			hasAgent = true
		}
		if e == "GG_TASK_ID=TASK-042" {
			hasTask = true
		}
		if e == "GG_ROLE=developer" {
			hasRole = true
		}
	}
	if !hasAgent {
		t.Error("env missing GG_AGENT")
	}
	if !hasTask {
		t.Error("env missing GG_TASK_ID")
	}
	if !hasRole {
		t.Error("env missing GG_ROLE")
	}
}

func TestBuildWorkerEnv_EmptyTaskID(t *testing.T) {
	t.Setenv("GG_AGENT", "")
	t.Setenv("GG_ROLE", "")

	env := buildWorkerEnv("", nil)
	for _, e := range env {
		if len(e) > len("GG_TASK_ID=") && e[:len("GG_TASK_ID=")] == "GG_TASK_ID=" {
			t.Errorf("should not export GG_TASK_ID when taskID is empty, got %q", e)
		}
	}
}

func TestBuildWorkerEnv_ExtraEnv(t *testing.T) {
	t.Setenv("GG_AGENT", "")
	t.Setenv("GG_ROLE", "")

	extra := []string{"FOO=bar", "BAZ=qux"}
	env := buildWorkerEnv("TASK-001", extra)

	hasFoo := false
	hasBaz := false
	for _, e := range env {
		if e == "FOO=bar" {
			hasFoo = true
		}
		if e == "BAZ=qux" {
			hasBaz = true
		}
	}
	if !hasFoo {
		t.Error("env missing FOO=bar")
	}
	if !hasBaz {
		t.Error("env missing BAZ=qux")
	}
}

// TestBuildWorkerStartup verifies the startup command shape.
func TestBuildWorkerStartup(t *testing.T) {
	startup := buildWorkerStartup("TASK-042")
	if startup == "" {
		t.Error("startup should not be empty")
	}
	// Must reference the task ID so the agent loads the right context.
	if !spawnContains(startup, "TASK-042") {
		t.Errorf("startup %q does not reference TASK-042", startup)
	}
}

// TestAppendUniqID verifies deduplication behaviour.
func TestAppendUniqID(t *testing.T) {
	s := []string{"TASK-001", "TASK-002"}
	s = appendUniqID(s, "TASK-001") // duplicate — should not append
	if len(s) != 2 {
		t.Errorf("expected 2 elements after duplicate append, got %d", len(s))
	}

	s = appendUniqID(s, "TASK-003") // new — should append
	if len(s) != 3 {
		t.Errorf("expected 3 elements after new append, got %d", len(s))
	}
	if s[2] != "TASK-003" {
		t.Errorf("s[2] = %q, want TASK-003", s[2])
	}
}

func TestAppendUniqID_Empty(t *testing.T) {
	var s []string
	s = appendUniqID(s, "TASK-001")
	if len(s) != 1 || s[0] != "TASK-001" {
		t.Errorf("expected [TASK-001], got %v", s)
	}
}

// spawnContains is a simple substring helper for spawn tests.
func spawnContains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
