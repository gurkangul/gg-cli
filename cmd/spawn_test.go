package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
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

// TestBootstrapAgentInPane_LaunchesAgentBeforePrompt is the regression guard
// for BUG-022 (queue-pool path silently dropped the agent launch and only sent
// a shell startup, leaving the pane as an idle bash). Both spawn paths now
// route through bootstrapAgentInPane(); this test asserts the Send order so a
// future refactor cannot reintroduce the divergence.
func TestBootstrapAgentInPane_LaunchesAgentBeforePrompt(t *testing.T) {
	if testing.Short() {
		t.Skip("3s sleep between agent launch and prompt makes this slow")
	}
	fake := terminal.NewFake()
	id, err := fake.NewSplit(context.Background(), terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}
	var buf bytes.Buffer
	bootstrapAgentInPane(context.Background(), fake, id, "gsd", "TASK-042", &buf)

	// Expect: NewSplit, Send(gsd), SendKey(enter), Send(prompt), SendKey(enter)
	if got := len(fake.Calls); got != 5 {
		t.Fatalf("Calls = %d, want 5: %+v", got, fake.Calls)
	}
	if c := fake.Calls[1]; c.Method != "Send" || c.Arg != "gsd" {
		t.Errorf("call[1] = %+v, want Send gsd", c)
	}
	if c := fake.Calls[2]; c.Method != "SendKey" || c.Arg != "enter" {
		t.Errorf("call[2] = %+v, want SendKey enter", c)
	}
	if c := fake.Calls[3]; c.Method != "Send" || !spawnContains(c.Arg, "TASK-042") {
		t.Errorf("call[3] = %+v, want Send <prompt with TASK-042>", c)
	}
	if c := fake.Calls[4]; c.Method != "SendKey" || c.Arg != "enter" {
		t.Errorf("call[4] = %+v, want SendKey enter", c)
	}
}

// TestBootstrapAgentInPane_NoTaskIDSkipsPrompt verifies that an empty taskID
// only launches the agent — no orientation prompt is sent.
func TestBootstrapAgentInPane_NoTaskIDSkipsPrompt(t *testing.T) {
	fake := terminal.NewFake()
	id, err := fake.NewSplit(context.Background(), terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}
	var buf bytes.Buffer
	bootstrapAgentInPane(context.Background(), fake, id, "gsd", "", &buf)

	if got := len(fake.Calls); got != 3 {
		t.Fatalf("Calls = %d, want 3 (NewSplit, Send gsd, SendKey enter): %+v", got, fake.Calls)
	}
}

// TestSpawnWorker_PromptContainsImpactStep asserts that the generated worker
// prompt includes "Impact analysis" and "gg impact" so workers always see the
// impact-analysis primer before implementing.
func TestSpawnWorker_PromptContainsImpactStep(t *testing.T) {
	prompt := buildWorkerPrompt("TASK-042")
	if !spawnContains(prompt, "Impact analysis") {
		t.Errorf("buildWorkerPrompt: missing 'Impact analysis' step\ngot: %s", prompt)
	}
	if !spawnContains(prompt, "gg impact") {
		t.Errorf("buildWorkerPrompt: missing 'gg impact' instruction\ngot: %s", prompt)
	}
	if !spawnContains(prompt, "Impact-Reviewed:") {
		t.Errorf("buildWorkerPrompt: missing 'Impact-Reviewed:' trailer instruction\ngot: %s", prompt)
	}
	if !spawnContains(prompt, "TASK-042") {
		t.Errorf("buildWorkerPrompt: missing task ID TASK-042\ngot: %s", prompt)
	}
}

func TestSpawnWorker_PromptContainsAckProtocol(t *testing.T) {
	prompt := buildWorkerPrompt("TASK-042")
	for _, want := range []string{
		"export GG_ROLE=developer",
		"gg task get TASK-042 --json",
		"gg task ack TASK-042",
		"ACK-OK",
		"ACK-FIX",
		"ACK-IMPLICIT",
	} {
		if !spawnContains(prompt, want) {
			t.Errorf("buildWorkerPrompt: missing %q\ngot: %s", want, prompt)
		}
	}
}

func TestSpawnWorker_PromptIsSingleLineForGSD(t *testing.T) {
	prompt := buildWorkerPrompt("TASK-042")
	if strings.ContainsAny(prompt, "\r\n") {
		t.Fatalf("buildWorkerPrompt must be single-line for GSD/cmux input; got %q", prompt)
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
