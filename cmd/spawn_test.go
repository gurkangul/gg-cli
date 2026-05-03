package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

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
	t.Setenv("GG_ROLE", "master")

	env := buildWorkerEnv("TASK-042", nil)

	hasAgent := false
	hasTask := false
	hasRole := false
	hasMasterAgent := false
	hasMasterRole := false
	for _, e := range env {
		if e == "GG_AGENT=claude-code" {
			hasAgent = true
		}
		if e == "GG_TASK_ID=TASK-042" {
			hasTask = true
		}
		if e == "GG_ROLE=master" {
			hasRole = true
		}
		if e == "GG_MASTER_AGENT=claude-code" {
			hasMasterAgent = true
		}
		if e == "GG_MASTER_ROLE=master" {
			hasMasterRole = true
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
	if !hasMasterAgent {
		t.Error("env missing GG_MASTER_AGENT")
	}
	if !hasMasterRole {
		t.Error("env missing GG_MASTER_ROLE")
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

func TestBuildAgentLaunchCommand_CDsToProjectRoot(t *testing.T) {
	setupGGDir(t)

	launch := buildAgentLaunchCommand("gsd")
	for _, want := range []string{
		"cd '",
		"exec gsd",
	} {
		if !spawnContains(launch, want) {
			t.Fatalf("launch command missing %q: %s", want, launch)
		}
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
	prevDelay, prevTimeout, prevInterval := spawnAgentPromptDelay, spawnAgentReadyTimeout, spawnAgentReadyInterval
	spawnAgentPromptDelay = 0
	spawnAgentReadyTimeout = 20 * time.Millisecond
	spawnAgentReadyInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		spawnAgentPromptDelay, spawnAgentReadyTimeout, spawnAgentReadyInterval = prevDelay, prevTimeout, prevInterval
	})

	fake := terminal.NewFake()
	id, err := fake.NewSplit(context.Background(), terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}
	fake.SetScreen(id, []byte("Get Shit Done\nSystem OK"))
	var buf bytes.Buffer
	bootstrapAgentInPane(context.Background(), fake, id, "gsd", "TASK-042", &buf)

	if c := fake.Calls[1]; c.Method != "Send" || c.Arg != "gsd" {
		t.Errorf("call[1] = %+v, want Send gsd", c)
	}
	if c := fake.Calls[2]; c.Method != "SendKey" || c.Arg != "enter" {
		t.Errorf("call[2] = %+v, want SendKey enter", c)
	}
	if !sendTaskPromptSeen(fake.Calls) {
		t.Fatalf("expected task prompt to be sent; calls=%+v", fake.Calls)
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

func TestSpawnWorker_PromptRoutesCompletionToActiveCodexMaster(t *testing.T) {
	t.Setenv("GG_ROLE", "master")
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_MASTER_ROLE", "")
	t.Setenv("GG_MASTER_AGENT", "")

	prompt := buildWorkerPrompt("TASK-367")
	for _, want := range []string{
		"gg tell master,codex,claude-code",
		"gg spawn advance --task TASK-367",
		"Do not stop at prose confirmation",
	} {
		if !spawnContains(prompt, want) {
			t.Errorf("buildWorkerPrompt: missing %q\ngot: %s", want, prompt)
		}
	}
}

func TestMasterMessageTargetsPreferSpawnedMasterEnv(t *testing.T) {
	t.Setenv("GG_MASTER_ROLE", "master")
	t.Setenv("GG_MASTER_AGENT", "codex")
	t.Setenv("GG_ROLE", "developer")
	t.Setenv("GG_AGENT", "claude-code")

	got := masterMessageTargetCSV()
	want := "master,codex,claude-code"
	if got != want {
		t.Fatalf("masterMessageTargetCSV() = %q, want %q", got, want)
	}
}

func TestSpawnWorker_PromptIsSingleLineForGSD(t *testing.T) {
	prompt := buildWorkerPrompt("TASK-042")
	if strings.ContainsAny(prompt, "\r\n") {
		t.Fatalf("buildWorkerPrompt must be single-line for GSD/cmux input; got %q", prompt)
	}
}

func TestGSDLikeAgentDetection(t *testing.T) {
	setupGGDir(t)

	if !isGSDLikeAgent("gsd") {
		t.Fatal("bare gsd should be detected as GSD-like agent")
	}
	if !isGSDLikeAgent(buildAgentLaunchCommand("gsd")) {
		t.Fatalf("launch command should be detected as GSD-like: %q", buildAgentLaunchCommand("gsd"))
	}
	if isGSDLikeAgent("codex") {
		t.Fatal("non-GSD agent should not be detected as GSD-like")
	}
}

func TestGSDReadyScreenDetection(t *testing.T) {
	tests := []struct {
		name   string
		screen string
		want   bool
	}{
		{name: "ready prompt marker", screen: "Get Shit Done\n/gsd to begin", want: true},
		{name: "system ok marker", screen: "GET SHIT DONE\nSystem OK", want: true},
		{name: "mcp ready marker", screen: "Get Shit Done\nMCP client ready", want: true},
		{name: "missing title", screen: "/gsd to begin", want: false},
		{name: "title but no marker", screen: "Get Shit Done\nloading", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isGSDReadyScreen([]byte(tc.screen))
			if got != tc.want {
				t.Fatalf("isGSDReadyScreen(%q) = %v, want %v", tc.screen, got, tc.want)
			}
		})
	}
}

func TestBootstrapAgentInPane_GSDWaitsForReadyBeforePrompt(t *testing.T) {
	prevDelay, prevTimeout, prevInterval := spawnAgentPromptDelay, spawnAgentReadyTimeout, spawnAgentReadyInterval
	spawnAgentPromptDelay = 0
	spawnAgentReadyTimeout = 100 * time.Millisecond
	spawnAgentReadyInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		spawnAgentPromptDelay, spawnAgentReadyTimeout, spawnAgentReadyInterval = prevDelay, prevTimeout, prevInterval
	})

	fake := terminal.NewFake()
	id, err := fake.NewSplit(context.Background(), terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		bootstrapAgentInPane(context.Background(), fake, id, "gsd", "TASK-042", &buf)
		close(done)
	}()

	deadline := time.Now().Add(120 * time.Millisecond)
	for {
		calls := fake.CallsSnapshot()
		if sendTaskPromptSeen(calls) {
			t.Fatal("task prompt sent before ready marker")
		}
		if countReadScreen(calls) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bootstrap did not poll screen for readiness")
		}
		time.Sleep(2 * time.Millisecond)
	}

	fake.SetScreen(id, []byte("Get Shit Done\n/gsd to begin"))

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("bootstrap did not finish after ready marker")
	}

	calls := fake.CallsSnapshot()
	if !sendTaskPromptSeen(calls) {
		t.Fatalf("expected task prompt after ready; calls=%+v", calls)
	}
}

func TestBootstrapAgentInPane_GSDTimeoutSkipsPrompt(t *testing.T) {
	prevDelay, prevTimeout, prevInterval := spawnAgentPromptDelay, spawnAgentReadyTimeout, spawnAgentReadyInterval
	spawnAgentPromptDelay = 0
	spawnAgentReadyTimeout = 20 * time.Millisecond
	spawnAgentReadyInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		spawnAgentPromptDelay, spawnAgentReadyTimeout, spawnAgentReadyInterval = prevDelay, prevTimeout, prevInterval
	})

	fake := terminal.NewFake()
	id, err := fake.NewSplit(context.Background(), terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}

	var buf bytes.Buffer
	bootstrapAgentInPane(context.Background(), fake, id, "gsd", "TASK-042", &buf)

	if sendTaskPromptSeen(fake.Calls) {
		t.Fatalf("did not expect task prompt before GSD ready; calls=%+v", fake.Calls)
	}
	if !strings.Contains(buf.String(), "skipping task prompt delivery") {
		t.Fatalf("expected skip warning, got %q", buf.String())
	}
}

func TestBootstrapAgentInPane_NonGSDSkipsScreenPolling(t *testing.T) {
	prevDelay, prevTimeout, prevInterval := spawnAgentPromptDelay, spawnAgentReadyTimeout, spawnAgentReadyInterval
	spawnAgentPromptDelay = 0
	spawnAgentReadyTimeout = 50 * time.Millisecond
	spawnAgentReadyInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		spawnAgentPromptDelay, spawnAgentReadyTimeout, spawnAgentReadyInterval = prevDelay, prevTimeout, prevInterval
	})

	fake := terminal.NewFake()
	id, err := fake.NewSplit(context.Background(), terminal.SplitOpts{})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}

	var buf bytes.Buffer
	bootstrapAgentInPane(context.Background(), fake, id, "codex", "TASK-042", &buf)

	if countReadScreen(fake.Calls) != 0 {
		t.Fatalf("non-GSD path should not read screen; calls=%+v", fake.Calls)
	}
	if !sendTaskPromptSeen(fake.Calls) {
		t.Fatalf("non-GSD path should still send task prompt; calls=%+v", fake.Calls)
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

func sendTaskPromptSeen(calls []terminal.Call) bool {
	for _, c := range calls {
		if c.Method == "Send" && strings.Contains(c.Arg, "TASK-042") && strings.Contains(c.Arg, "gg task get") {
			return true
		}
	}
	return false
}

func countReadScreen(calls []terminal.Call) int {
	n := 0
	for _, c := range calls {
		if c.Method == "ReadScreen" {
			n++
		}
	}
	return n
}
