// Package cmd — tests for refetch-rate warning and sessions rendering in gg status output.
package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

func TestStatus_RefetchRateWarn(t *testing.T) {
	setupGGDir(t)
	rtDir := testRuntimeDir(t)
	if err := os.MkdirAll(rtDir, 0o755); err != nil {
		t.Fatalf("mkdir rtDir: %v", err)
	}

	// Seed: 2 compact calls + 2 hydration calls → 100% re-fetch rate (>50% threshold).
	telemetry.RecordCompact(rtDir, "get", "", 100, 200, 2, "")
	telemetry.RecordCompact(rtDir, "get", "", 100, 200, 2, "")
	telemetry.RecordHydration(rtDir, "get", "", 200)
	telemetry.RecordHydration(rtDir, "get", "", 200)

	out := captureStdout(t, func() {
		_, _, _ = execCmd(t, "telemetry", "summary")
	})

	if !strings.Contains(out, "warning: re-fetch rate >50%") {
		t.Errorf("expected refetch-rate warning in output when rate=100%%; got:\n%s", out)
	}
}

func TestStatus_RefetchRateNoWarn(t *testing.T) {
	setupGGDir(t)
	rtDir := testRuntimeDir(t)
	if err := os.MkdirAll(rtDir, 0o755); err != nil {
		t.Fatalf("mkdir rtDir: %v", err)
	}

	// Seed: 4 compact calls + 1 hydration call → 25% re-fetch rate (<50% threshold).
	telemetry.RecordCompact(rtDir, "get", "", 100, 200, 2, "")
	telemetry.RecordCompact(rtDir, "get", "", 100, 200, 2, "")
	telemetry.RecordCompact(rtDir, "get", "", 100, 200, 2, "")
	telemetry.RecordCompact(rtDir, "get", "", 100, 200, 2, "")
	telemetry.RecordHydration(rtDir, "get", "", 200)

	out := captureStdout(t, func() {
		_, _, _ = execCmd(t, "telemetry", "summary")
	})

	if strings.Contains(out, "re-fetch rate >50%") {
		t.Errorf("expected NO refetch-rate warning when rate=25%%; got:\n%s", out)
	}
}

// ── Sessions warning line (TASK-296) ─────────────────────────────────────────

// TestStatus_SessionsWarning_Shown verifies that renderSessionsBlock emits the
// ⚠ Sessions warning line when OverThresholdCount > 0.
func TestStatus_SessionsWarning_Shown(t *testing.T) {
	ssum := &telemetry.SessionSummary{
		ActiveSessions:            2,
		AvgCompactCallsPerSession: 3.0,
		P50CumulativeKB:           50.0,
		P95CumulativeKB:           120.0,
		OverThresholdCount:        1,
	}
	out := renderSessionsBlock(ssum)
	if !strings.Contains(out, "⚠ Sessions") {
		t.Errorf("expected '⚠ Sessions' warning line when OverThresholdCount=1; got:\n%s", out)
	}
	if !strings.Contains(out, "1 session(s) exceeded 100 KB") {
		t.Errorf("expected count in warning line; got:\n%s", out)
	}
}

// TestStatus_SessionsWarning_NotShown verifies that renderSessionsBlock does NOT
// emit the ⚠ Sessions warning line when all sessions are below the threshold.
func TestStatus_SessionsWarning_NotShown(t *testing.T) {
	ssum := &telemetry.SessionSummary{
		ActiveSessions:            2,
		AvgCompactCallsPerSession: 2.0,
		P50CumulativeKB:           10.0,
		P95CumulativeKB:           30.0,
		OverThresholdCount:        0,
	}
	out := renderSessionsBlock(ssum)
	if strings.Contains(out, "⚠ Sessions") {
		t.Errorf("expected NO '⚠ Sessions' warning when OverThresholdCount=0; got:\n%s", out)
	}
	if !strings.Contains(out, "Sessions    2 active") {
		t.Errorf("expected summary line to be present; got:\n%s", out)
	}
}

// TestStatus_SessionsWarning_MultipleOverThreshold verifies the count in the
// warning line when more than one session exceeds the threshold.
func TestStatus_SessionsWarning_MultipleOverThreshold(t *testing.T) {
	ssum := &telemetry.SessionSummary{
		ActiveSessions:            3,
		AvgCompactCallsPerSession: 5.0,
		P50CumulativeKB:           110.0,
		P95CumulativeKB:           200.0,
		OverThresholdCount:        3,
	}
	out := renderSessionsBlock(ssum)
	if !strings.Contains(out, "3 session(s) exceeded 100 KB") {
		t.Errorf("expected '3 session(s)' in warning; got:\n%s", out)
	}
}

// ── AC-5b: renderRolesBlock shows queue state ─────────────────────────────────

// TestRolesBlock_QueueNotStarted verifies that renderRolesBlock shows the full
// "not started — single-pane mode (run gg spawn queue start...)" hint when no
// queue.json exists (AC-5b). Also verifies Developer line includes transport.
func TestRolesBlock_QueueNotStarted(t *testing.T) {
	rtDir := t.TempDir()
	dev := &config.DeveloperConfig{Command: "gsd", Transport: "cmux"}
	out := renderRolesBlock(dev, rtDir)
	if !strings.Contains(out, "Queue") {
		t.Errorf("AC-5b: expected 'Queue' line in Roles block; got:\n%s", out)
	}
	if !strings.Contains(out, "not started") {
		t.Errorf("AC-5b: expected 'not started' when no queue.json; got:\n%s", out)
	}
	// Full hint text must be present so operators know how to enable parallel pickup.
	if !strings.Contains(out, "gg spawn queue start") {
		t.Errorf("AC-5b: expected 'gg spawn queue start' hint in not-started output; got:\n%s", out)
	}
	// Developer line must show transport in parentheses.
	if !strings.Contains(out, "gsd (cmux)") {
		t.Errorf("AC-5b: expected 'gsd (cmux)' developer line with transport; got:\n%s", out)
	}
}

// TestRolesBlock_QueueRunning verifies that renderRolesBlock shows "running"
// with completed and skipped counts when a queue session is active (AC-5b).
func TestRolesBlock_QueueRunning(t *testing.T) {
	rtDir := t.TempDir()
	sess := &spawn.QueueSession{
		Agent:     "gsd",
		Completed: []string{"TASK-001", "TASK-002"},
		Skipped:   []string{"TASK-003"},
	}
	if err := spawn.WriteQueue(rtDir, sess); err != nil {
		t.Fatalf("WriteQueue: %v", err)
	}
	dev := &config.DeveloperConfig{Command: "gsd"}
	out := renderRolesBlock(dev, rtDir)
	if !strings.Contains(out, "running") {
		t.Errorf("AC-5b: expected 'running' when queue session active; got:\n%s", out)
	}
	if !strings.Contains(out, "completed: 2") {
		t.Errorf("AC-5b: expected 'completed: 2' in queue line; got:\n%s", out)
	}
	if !strings.Contains(out, "skipped: 1") {
		t.Errorf("AC-5b: expected 'skipped: 1' in queue line; got:\n%s", out)
	}
}

// TestRolesBlock_QueuePaused verifies that renderRolesBlock shows "paused"
// with completed and skipped counts when a queue session is paused (AC-5b).
func TestRolesBlock_QueuePaused(t *testing.T) {
	rtDir := t.TempDir()
	sess := &spawn.QueueSession{
		Agent:     "gsd",
		Completed: []string{"TASK-001"},
		Skipped:   []string{"TASK-002", "TASK-003"},
		Paused:    true,
	}
	if err := spawn.WriteQueue(rtDir, sess); err != nil {
		t.Fatalf("WriteQueue: %v", err)
	}
	dev := &config.DeveloperConfig{Command: "gsd"}
	out := renderRolesBlock(dev, rtDir)
	if !strings.Contains(out, "paused") {
		t.Errorf("AC-5b: expected 'paused' when queue paused; got:\n%s", out)
	}
	if !strings.Contains(out, "completed: 1") {
		t.Errorf("AC-5b: expected 'completed: 1' in paused queue line; got:\n%s", out)
	}
	if !strings.Contains(out, "skipped: 2") {
		t.Errorf("AC-5b: expected 'skipped: 2' in paused queue line; got:\n%s", out)
	}
}

func TestRolesBlock_QueueComplete(t *testing.T) {
	rtDir := t.TempDir()
	sess := &spawn.QueueSession{
		Agent:     "gsd",
		Completed: []string{"TASK-001"},
		Done:      true,
	}
	if err := spawn.WriteQueue(rtDir, sess); err != nil {
		t.Fatalf("WriteQueue: %v", err)
	}
	dev := &config.DeveloperConfig{Command: "gsd"}
	out := renderRolesBlock(dev, rtDir)
	if !strings.Contains(out, "complete") {
		t.Errorf("BUG-045: expected complete queue state; got:\n%s", out)
	}
	if strings.Contains(out, "running") {
		t.Errorf("BUG-045: completed queue must not render as running; got:\n%s", out)
	}
}

// TestRolesBlock_EmptyRtDir verifies that renderRolesBlock degrades gracefully
// when rtDir is empty (no runtime dir available), and still shows the Queue line
// with "not started" but without the spawn hint (no runtime = no dir to show).
func TestRolesBlock_EmptyRtDir(t *testing.T) {
	dev := &config.DeveloperConfig{Command: "gsd"}
	out := renderRolesBlock(dev, "")
	if !strings.Contains(out, "Queue") {
		t.Errorf("AC-5b: expected Queue line even with empty rtDir; got:\n%s", out)
	}
	if !strings.Contains(out, "not started") {
		t.Errorf("AC-5b: expected 'not started' when rtDir empty; got:\n%s", out)
	}
}

// TestRolesBlock_UnconfiguredDeveloper verifies that renderRolesBlock shows
// the "⚠ unconfigured" placeholder when no developer command is set.
func TestRolesBlock_UnconfiguredDeveloper(t *testing.T) {
	rtDir := t.TempDir()
	dev := &config.DeveloperConfig{}
	out := renderRolesBlock(dev, rtDir)
	if !strings.Contains(out, "unconfigured") {
		t.Errorf("AC-5b: expected 'unconfigured' when developer command is empty; got:\n%s", out)
	}
}

// TestRolesBlock_NilDeveloper verifies that renderRolesBlock returns empty
// string when dev is nil (defensive nil check).
func TestRolesBlock_NilDeveloper(t *testing.T) {
	out := renderRolesBlock(nil, t.TempDir())
	if out != "" {
		t.Errorf("expected empty string for nil developer config; got:\n%s", out)
	}
}

// TestQueueStatusLine_NotStartedWithHint verifies queueStatusLine returns the
// full spawn hint when rtDir has no queue.json.
func TestQueueStatusLine_NotStartedWithHint(t *testing.T) {
	line := queueStatusLine(t.TempDir())
	if !strings.Contains(line, "not started") {
		t.Errorf("expected 'not started' in line; got: %s", line)
	}
	if !strings.Contains(line, "single-pane mode") {
		t.Errorf("expected 'single-pane mode' in line; got: %s", line)
	}
	if !strings.Contains(line, "gg spawn queue start") {
		t.Errorf("expected 'gg spawn queue start' hint in line; got: %s", line)
	}
}

// TestQueueStatusLine_EmptyRtDir verifies queueStatusLine returns the short
// "not started — single-pane mode" form (no hint) when rtDir is empty.
func TestQueueStatusLine_EmptyRtDir(t *testing.T) {
	line := queueStatusLine("")
	if !strings.Contains(line, "not started") {
		t.Errorf("expected 'not started' when rtDir empty; got: %s", line)
	}
	if strings.Contains(line, "gg spawn queue start") {
		t.Errorf("expected NO spawn hint when rtDir empty; got: %s", line)
	}
}
