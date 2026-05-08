package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
	"github.com/gurkangul/gg-cli/internal/store"
)

// TestSpawnAgentDefault verifies fallback behaviour for the agent default.
func TestSpawnAgentDefault_Fallback(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GG_SPAWN_AGENT", "")
	if got := spawnAgentDefault(); got != "" {
		t.Errorf("spawnAgentDefault() = %q, want empty", got)
	}
}

func TestSpawnAgentDefault_EnvOverride(t *testing.T) {
	t.Setenv("GG_SPAWN_AGENT", "codex")
	if got := spawnAgentDefault(); got != "codex" {
		t.Errorf("spawnAgentDefault() = %q, want %q", got, "codex")
	}
}

func TestSpawnAgentDefault_ConfigCommand(t *testing.T) {
	ggDir := setupGGDir(t)
	cfgWithCommand := ggConfig + "developer:\n  command: gsd --model openai-codex/gpt-5.3-codex\n  transport: cmux\n"
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfgWithCommand), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("GG_SPAWN_AGENT", "")

	want := "gsd --model openai-codex/gpt-5.3-codex"
	if got := spawnAgentDefault(); got != want {
		t.Errorf("spawnAgentDefault() = %q, want %q", got, want)
	}
}

func TestSpawnAgentDefault_RoleCommand(t *testing.T) {
	ggDir := setupGGDir(t)
	cfgWithRoles := ggConfig + "roles:\n  reviewer:\n    command: codex --model gpt-5.3-codex\n    transport: cmux\n"
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfgWithRoles), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if got := spawnAgentDefaultForRole("reviewer"); got != "codex --model gpt-5.3-codex" {
		t.Errorf("spawnAgentDefaultForRole(reviewer) = %q", got)
	}
}

func TestSpawnAgentDefault_LegacyGSDAgentMapsToCommand(t *testing.T) {
	ggDir := setupGGDir(t)
	cfgWithLegacyAgent := ggConfig + "developer:\n  agent: gsd-sonnet-4.6\n  transport: cmux\n"
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfgWithLegacyAgent), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("GG_SPAWN_AGENT", "")

	if got := spawnAgentDefault(); got != "gsd" {
		t.Errorf("spawnAgentDefault() = %q, want gsd", got)
	}
}

func TestSpawnAgentDefault_LegacyModelIDIsNotCommand(t *testing.T) {
	ggDir := setupGGDir(t)
	cfgWithLegacyAgent := ggConfig + "developer:\n  agent: claude-sonnet-4.5\n  transport: cmux\n"
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfgWithLegacyAgent), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("GG_SPAWN_AGENT", "")

	if got := spawnAgentDefault(); got != "" {
		t.Errorf("spawnAgentDefault() = %q, want empty", got)
	}
}

// TestBuildWorkerEnv verifies that task ID and agent are always exported.
func TestBuildWorkerEnv_TaskID(t *testing.T) {
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "master")

	env := buildWorkerEnv("TASK-042", nil, "gsd --model openai-codex/gpt-5.3-codex")

	hasAgent := false
	hasTask := false
	hasRole := false
	hasMasterAgent := false
	hasMasterRole := false
	for _, e := range env {
		if e == "GG_AGENT=gsd" {
			hasAgent = true
		}
		if e == "GG_TASK_ID=TASK-042" {
			hasTask = true
		}
		if e == "GG_ROLE=developer" {
			hasRole = true
		}
		if e == "GG_MASTER_AGENT=codex" {
			hasMasterAgent = true
		}
		if e == "GG_MASTER_ROLE=master" {
			hasMasterRole = true
		}
	}
	if !hasAgent {
		t.Error("env missing GG_AGENT=gsd")
	}
	if !hasTask {
		t.Error("env missing GG_TASK_ID")
	}
	if !hasRole {
		t.Error("env missing GG_ROLE=developer")
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

	env := buildWorkerEnv("", nil, "gsd")
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
	env := buildWorkerEnv("TASK-001", extra, "codex")

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

func TestWorkerAgentIdentity(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{cmd: "gsd --model openai-codex/gpt-5.3-codex", want: "gsd"},
		{cmd: "zsh -lc 'exec gsd --model openai-codex/gpt-5.3-codex'", want: "gsd"},
		{cmd: "/Users/example/.gg/bin/ggdev-worker", want: "gsd"},
		{cmd: "codex --model gpt-5.3-codex", want: "codex"},
		{cmd: "", want: "developer"},
	}
	for _, tc := range tests {
		if got := workerAgentIdentity(tc.cmd); got != tc.want {
			t.Fatalf("workerAgentIdentity(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
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
	bootstrapAgentInPane(context.Background(), fake, id, "gsd", "TASK-042", "developer", &buf)

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
	bootstrapAgentInPane(context.Background(), fake, id, "gsd", "", "developer", &buf)

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
	if !spawnContains(prompt, "Review-Convergence:") {
		t.Errorf("buildWorkerPrompt: missing 'Review-Convergence:' trailer instruction\ngot: %s", prompt)
	}
	if !spawnContains(prompt, "TASK-042") {
		t.Errorf("buildWorkerPrompt: missing task ID TASK-042\ngot: %s", prompt)
	}
}

func TestBuildReviewerPrompt_RoutesReviewNotImplementation(t *testing.T) {
	prompt := buildReviewerPrompt("TASK-042", "reviewer")
	for _, want := range []string{
		"GG_ROLE=reviewer",
		"gg task get TASK-042 --json",
		"skeptical senior architect",
		"solves the right problem",
		"reliable 90%",
		"verified and unverified assumptions",
		"missing deployment-path changes",
		"breaking-change risks",
		"simpler alternatives",
		"Confidence: N/10",
		"Verdict: HUMAN_REVIEW",
		"gg task review TASK-042 --approve --notes",
		"gg task done TASK-042",
		"--verifier reviewer",
		"Do not run gg task done in the reviewer pane",
		"master owns the long-running close",
		"gg task review TASK-042 --reject --notes",
		"Do not implement production code",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("reviewer prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestPreflightSpawnWorkerTask_RejectsBlockedTask(t *testing.T) {
	tasks := fakeSpawnTaskReader{
		"TASK-042": {ID: "TASK-042", Status: "blocked", BlockReason: "waiting on TASK-041"},
	}

	err := preflightSpawnWorkerTask(context.Background(), tasks, "TASK-042", "developer")
	if err == nil {
		t.Fatal("expected blocked task to be rejected")
	}
	for _, want := range []string{"TASK-042 is blocked", "waiting on TASK-041"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestPreflightSpawnWorkerTask_RejectsReadyForLiveTask(t *testing.T) {
	tasks := fakeSpawnTaskReader{
		"TASK-042": {ID: "TASK-042", Status: "ready_for_live"},
	}

	err := preflightSpawnWorkerTask(context.Background(), tasks, "TASK-042", "developer")
	if err == nil {
		t.Fatal("expected ready_for_live task to be rejected")
	}
	if !strings.Contains(err.Error(), "assign a verifier/reviewer") {
		t.Fatalf("error should route to verifier/reviewer, got: %v", err)
	}
}

func TestPreflightSpawnWorkerTask_AllowsReviewerForReadyForLiveTask(t *testing.T) {
	tasks := fakeSpawnTaskReader{
		"TASK-042": {ID: "TASK-042", Status: "ready_for_live"},
	}

	if err := preflightSpawnWorkerTask(context.Background(), tasks, "TASK-042", "reviewer"); err != nil {
		t.Fatalf("preflightSpawnWorkerTask() error = %v, want nil", err)
	}
}

func TestPreflightSpawnWorkerTask_RejectsReviewerForPendingTask(t *testing.T) {
	tasks := fakeSpawnTaskReader{
		"TASK-042": {ID: "TASK-042", Status: "pending"},
	}

	err := preflightSpawnWorkerTask(context.Background(), tasks, "TASK-042", "reviewer")
	if err == nil {
		t.Fatal("expected reviewer to be rejected for pending implementation task")
	}
	if !strings.Contains(err.Error(), "route implementation to developer") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreflightSpawnWorkerTask_RejectsUnfinishedDependencies(t *testing.T) {
	tasks := fakeSpawnTaskReader{
		"TASK-042": {ID: "TASK-042", Status: "pending", DependsOn: []string{"TASK-041", "TASK-040"}},
		"TASK-041": {ID: "TASK-041", Status: "blocked"},
		"TASK-040": {ID: "TASK-040", Status: "done"},
	}

	err := preflightSpawnWorkerTask(context.Background(), tasks, "TASK-042", "developer")
	if err == nil {
		t.Fatal("expected unfinished dependency to be rejected")
	}
	for _, want := range []string{"unfinished dependencies", "TASK-041 (blocked)", "gg task deps TASK-042"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestPreflightSpawnWorkerTask_AllowsReadyPendingTask(t *testing.T) {
	tasks := fakeSpawnTaskReader{
		"TASK-042": {ID: "TASK-042", Status: "pending", DependsOn: []string{"TASK-041"}},
		"TASK-041": {ID: "TASK-041", Status: "done"},
	}

	if err := preflightSpawnWorkerTask(context.Background(), tasks, "TASK-042", "developer"); err != nil {
		t.Fatalf("preflightSpawnWorkerTask() error = %v, want nil", err)
	}
}

func TestSpawnWorker_PromptContainsAckProtocol(t *testing.T) {
	prompt := buildWorkerPrompt("TASK-042")
	for _, want := range []string{
		"export GG_AGENT=${GG_AGENT:-developer} GG_ROLE=developer",
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
		"gg tell master,codex",
		"gg task ready-for-live TASK-367",
		"--from developer",
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
	want := "master,codex"
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

func TestSpawnWorker_GGDevWorkerUsesShortRunTaskPrompt(t *testing.T) {
	prompt := buildWorkerPromptForAgent("/Users/example/.gg/bin/ggdev-worker", "TASK-427", "developer")
	if prompt != "RUN_TASK TASK-427" {
		t.Fatalf("buildWorkerPromptForAgent() = %q, want RUN_TASK TASK-427", prompt)
	}
	if len(prompt) > 80 {
		t.Fatalf("ggdev-worker prompt should stay short for PTY line discipline, got %d bytes", len(prompt))
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
	if !isGSDLikeAgent("cd '/tmp/project' && exec /Users/example/.gg/bin/ggdev-worker") {
		t.Fatal("ggdev-worker adapter should be detected as GSD-like so spawn waits for readiness")
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
		{name: "ggdev adapter marker", screen: "ggdev-worker ready: GG_AGENT=gsd GG_ROLE=developer", want: true},
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
		bootstrapAgentInPane(context.Background(), fake, id, "gsd", "TASK-042", "developer", &buf)
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
	result := bootstrapAgentInPane(context.Background(), fake, id, "gsd", "TASK-042", "developer", &buf)

	if sendTaskPromptSeen(fake.Calls) {
		t.Fatalf("did not expect task prompt before GSD ready; calls=%+v", fake.Calls)
	}
	if !strings.Contains(buf.String(), "skipping task prompt delivery") {
		t.Fatalf("expected skip warning, got %q", buf.String())
	}
	if result.Status != "skipped" || !strings.Contains(result.Warning, "skipping task prompt delivery") {
		t.Fatalf("expected skipped bootstrap result, got %+v", result)
	}
}

func TestSpawnWorkerForTask_RegistersSkippedPromptWarning(t *testing.T) {
	prevDelay, prevTimeout, prevInterval := spawnAgentPromptDelay, spawnAgentReadyTimeout, spawnAgentReadyInterval
	spawnAgentPromptDelay = 0
	spawnAgentReadyTimeout = 20 * time.Millisecond
	spawnAgentReadyInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		spawnAgentPromptDelay, spawnAgentReadyTimeout, spawnAgentReadyInterval = prevDelay, prevTimeout, prevInterval
	})

	rt := t.TempDir()
	fake := terminal.NewFake()
	if _, err := spawnWorkerForTask(context.Background(), fake, rt, "gsd", "TASK-401"); err != nil {
		t.Fatalf("spawnWorkerForTask: %v", err)
	}

	panes, err := spawn.ListPanes(rt)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	if len(panes) != 1 {
		t.Fatalf("panes len = %d, want 1", len(panes))
	}
	pane := panes[0]
	if pane.State != spawn.WorkerStateWaiting {
		t.Fatalf("pane state = %q, want waiting-on-master", pane.State)
	}
	if pane.PromptDeliveryStatus != "skipped" {
		t.Fatalf("PromptDeliveryStatus = %q, want skipped", pane.PromptDeliveryStatus)
	}
	if !strings.Contains(pane.PromptDeliveryError, "skipping task prompt delivery") {
		t.Fatalf("PromptDeliveryError missing skip warning: %q", pane.PromptDeliveryError)
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
	bootstrapAgentInPane(context.Background(), fake, id, "codex", "TASK-042", "developer", &buf)

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

type fakeSpawnTaskReader map[string]store.Task

func (f fakeSpawnTaskReader) GetTask(_ context.Context, taskID string) (*store.Task, error) {
	t, ok := f[taskID]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &t, nil
}
