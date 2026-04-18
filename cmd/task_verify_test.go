// Package cmd — tests for the pre-done verify gate wired into `gg task done`.
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

// writePreTaskDoneHook drops a shell script into .gg/hooks/pre-task-done.d/
// under the test's current ggDir. body is appended after a `#!/bin/sh` shebang.
func writePreTaskDoneHook(t *testing.T, ggDir, name, body string) {
	t.Helper()
	hookDir := filepath.Join(ggDir, "hooks", "pre-task-done.d")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir pre-task-done.d: %v", err)
	}
	path := filepath.Join(hookDir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write hook %s: %v", name, err)
	}
}

// (a) No pre-task-done.d directory → pre-hook stage is a no-op and the command
// proceeds to the store. Since the test fixture points Qdrant at a dead port,
// we expect ExitStoreDown — which proves the pre-hook did not block.
func TestTaskDone_NoPreHookDir_ReachesStore(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "task", "done", "TASK-001", "no pre-hook installed")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d) when no pre-hook dir, got %d", ExitStoreDown, ee.Code)
	}
}

// (b) A pre-hook that exits non-zero MUST block the transition: return
// ExitVerifyFailed, never reach the store. Distinguishable from ExitStoreDown
// so agents know the failure was a gate rejection, not infra.
func TestTaskDone_PreHookFails_BlocksTransition(t *testing.T) {
	ggDir := setupGGDir(t)
	writePreTaskDoneHook(t, ggDir, "01-fail.sh", "echo 'build broken' >&2\nexit 1")
	_, _, err := execCmd(t, "task", "done", "TASK-001", "attempt while build broken")
	if err == nil {
		t.Fatal("expected error: pre-hook should have blocked the transition")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitVerifyFailed {
		t.Errorf("expected ExitVerifyFailed(%d), got %d (msg=%q)", ExitVerifyFailed, ee.Code, ee.Message)
	}
}

// (c) A pre-hook that passes MUST let the command continue. With Qdrant dead
// the expected terminal error is ExitStoreDown — confirming the gate opened.
func TestTaskDone_PreHookPasses_FallsThroughToStore(t *testing.T) {
	ggDir := setupGGDir(t)
	writePreTaskDoneHook(t, ggDir, "01-ok.sh", "echo 'verify ok'\nexit 0")
	_, _, err := execCmd(t, "task", "done", "TASK-001", "build passed")
	if err == nil {
		t.Fatal("expected error when Qdrant is down (pre-hook passed, store still unreachable)")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d) after pre-hook pass, got %d", ExitStoreDown, ee.Code)
	}
}

// (d) On rejection, stderr MUST include a single NDJSON line describing the
// failure so any agent (Claude / Codex / Cursor / CI) can parse it without
// scraping human-readable text. Contract: line with event=verify_failed,
// task=TASK-ID, hook=<script>, exit=<code>, ts=<rfc3339>.
func TestTaskDone_PreHookFails_EmitsNDJSONEvent(t *testing.T) {
	ggDir := setupGGDir(t)
	writePreTaskDoneHook(t, ggDir, "07-broken.sh", "echo 'compile error' >&2\nexit 3")
	// GG_NO_AUTO_NOTIFY so the test does not depend on the (unreachable) store.
	t.Setenv("GG_NO_AUTO_NOTIFY", "1")

	_, stderr, err := execCmd(t, "task", "done", "TASK-001", "attempt with broken build")
	if err == nil {
		t.Fatal("expected error from pre-hook rejection")
	}

	// Find the NDJSON event line — strict contract: at least one stderr line
	// must parse as JSON with event=verify_failed.
	var found map[string]any
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(line), &payload) == nil {
			if ev, _ := payload["event"].(string); ev == "verify_failed" {
				found = payload
				break
			}
		}
	}
	if found == nil {
		t.Fatalf("no NDJSON verify_failed event in stderr:\n%s", stderr)
	}

	// Stable field contract — agents program against these keys.
	if got, _ := found["task"].(string); got != "TASK-001" {
		t.Errorf("event.task: got %q, want TASK-001", got)
	}
	if got, _ := found["hook"].(string); got != "07-broken.sh" {
		t.Errorf("event.hook: got %q, want 07-broken.sh", got)
	}
	// JSON numbers decode to float64.
	if got, _ := found["exit"].(float64); int(got) != 3 {
		t.Errorf("event.exit: got %v, want 3", found["exit"])
	}
	if _, ok := found["ts"].(string); !ok {
		t.Errorf("event.ts missing or not a string: %v", found["ts"])
	}
	// gate field — forward-compat: must be present so parsers survive a second gate type.
	if got, _ := found["gate"].(string); got != "pre-task-done" {
		t.Errorf("event.gate: got %q, want pre-task-done", got)
	}
}

// (e) GG_NO_AUTO_NOTIFY=1 MUST suppress the cross-agent broadcast but MUST NOT
// alter the gate's primary contract — the exit code and the NDJSON event still
// fire. This is the CI / reentrant-hook escape valve.
func TestTaskDone_PreHookFails_NoAutoNotifyStillRejects(t *testing.T) {
	ggDir := setupGGDir(t)
	writePreTaskDoneHook(t, ggDir, "01-fail.sh", "exit 1")
	t.Setenv("GG_NO_AUTO_NOTIFY", "1")

	_, _, err := execCmd(t, "task", "done", "TASK-001", "attempt with notifications disabled")
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitVerifyFailed {
		t.Errorf("GG_NO_AUTO_NOTIFY must not alter exit code: got %d, want %d", ee.Code, ExitVerifyFailed)
	}
}

func TestTruncateHookOutput(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 100, "short"},
		{"  trimmed  ", 100, "trimmed"},
		{strings.Repeat("a", 50), 10, "…aaaaaaaaaa"},
		{"", 10, ""},
	}
	for _, tc := range cases {
		got := truncateHookOutput(tc.in, tc.n)
		if got != tc.want {
			t.Errorf("truncateHookOutput(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

// ── TASK-173: notifyVerifyFailure success path ────────────────────────────────

// mockMessageSender records calls to SendMessage so tests can assert the correct
// FromRole, ToRole, and TaskID without a live Qdrant store.
type mockMessageSender struct {
	msgs []store.Message
}

func (m *mockMessageSender) SendMessage(_ context.Context, msg store.Message) error {
	m.msgs = append(m.msgs, msg)
	return nil
}

func TestSendVerifyFailure_CallsSendMessageWithCorrectFields(t *testing.T) {
	sender := &mockMessageSender{}
	rej := &verifyRejection{
		TaskID:   "TASK-042",
		Gate:     "pre-task-done",
		Hook:     "10-build.sh",
		ExitCode: 1,
		Stderr:   "compile error",
	}
	sendVerifyFailure(context.Background(), sender, rej)
	if len(sender.msgs) != 1 {
		t.Fatalf("expected 1 SendMessage call, got %d", len(sender.msgs))
	}
	msg := sender.msgs[0]
	if msg.FromRole != "verify-gate" {
		t.Errorf("FromRole: got %q, want verify-gate", msg.FromRole)
	}
	if msg.ToRole != "all" {
		t.Errorf("ToRole: got %q, want all", msg.ToRole)
	}
	if msg.TaskID != "TASK-042" {
		t.Errorf("TaskID: got %q, want TASK-042", msg.TaskID)
	}
	if !strings.Contains(msg.Content, "TASK-042") {
		t.Errorf("Content should mention task ID, got: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "compile error") {
		t.Errorf("Content should include stderr first line, got: %q", msg.Content)
	}
}

func TestSendVerifyFailure_NoStderr_ContentOmitsDetail(t *testing.T) {
	sender := &mockMessageSender{}
	rej := &verifyRejection{TaskID: "TASK-001", Gate: "pre-task-done", Hook: "10.sh", ExitCode: 1}
	sendVerifyFailure(context.Background(), sender, rej)
	if len(sender.msgs) != 1 {
		t.Fatalf("expected 1 message")
	}
	if strings.Contains(sender.msgs[0].Content, " — ") {
		t.Errorf("no stderr: content should not have ' — ' separator, got: %q", sender.msgs[0].Content)
	}
}

// ── TASK-174: post-hook advisory fires after pre-hook success ─────────────────

func TestRunTaskDoneHooks_FiresAfterPreHookSuccess(t *testing.T) {
	ggDir := setupGGDir(t)

	// Install a post-hook that writes a sentinel file to prove it ran.
	hookDir := filepath.Join(ggDir, "hooks", "task-done.d")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir task-done.d: %v", err)
	}
	sentinel := filepath.Join(t.TempDir(), "post-hook-ran")
	hookBody := "#!/bin/sh\ntouch " + sentinel + "\n"
	if err := os.WriteFile(filepath.Join(hookDir, "10-log.sh"), []byte(hookBody), 0o755); err != nil {
		t.Fatalf("write post-hook: %v", err)
	}

	// Call runTaskDoneHooks directly — no store required.
	cmd := &cobra.Command{}
	cmd.SetErr(&bytes.Buffer{})
	if err := runTaskDoneHooks(cmd, nil, "TASK-001", "test summary"); err != nil {
		t.Fatalf("runTaskDoneHooks: %v", err)
	}

	if _, err := os.Stat(sentinel); os.IsNotExist(err) {
		t.Error("post-hook did not run: sentinel file not created")
	}
}

func TestFirstLine(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello\nworld", "hello"},
		{"\n\n  first real line  \nsecond", "first real line"},
		{"", ""},
		{"only one", "only one"},
	}
	for _, tc := range cases {
		if got := firstLine(tc.in); got != tc.want {
			t.Errorf("firstLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
