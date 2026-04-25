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

// TestRolesBlock_QueueNotStarted verifies that renderRolesBlock shows the
// "not started" queue hint when no queue.json exists (AC-5b).
func TestRolesBlock_QueueNotStarted(t *testing.T) {
	rtDir := t.TempDir()
	dev := &config.DeveloperConfig{Agent: "gsd", Transport: "cmux"}
	out := renderRolesBlock(dev, rtDir)
	if !strings.Contains(out, "Queue") {
		t.Errorf("AC-5b: expected 'Queue' line in Roles block; got:\n%s", out)
	}
	if !strings.Contains(out, "not started") {
		t.Errorf("AC-5b: expected 'not started' when no queue.json; got:\n%s", out)
	}
}

// TestRolesBlock_QueueRunning verifies that renderRolesBlock shows "running"
// when a queue session exists and is not paused (AC-5b).
func TestRolesBlock_QueueRunning(t *testing.T) {
	rtDir := t.TempDir()
	sess := &spawn.QueueSession{
		Agent:     "gsd",
		Completed: []string{"TASK-001", "TASK-002"},
		Skipped:   []string{},
	}
	if err := spawn.WriteQueue(rtDir, sess); err != nil {
		t.Fatalf("WriteQueue: %v", err)
	}
	dev := &config.DeveloperConfig{Agent: "gsd"}
	out := renderRolesBlock(dev, rtDir)
	if !strings.Contains(out, "running") {
		t.Errorf("AC-5b: expected 'running' when queue session active; got:\n%s", out)
	}
	if !strings.Contains(out, "completed: 2") {
		t.Errorf("AC-5b: expected 'completed: 2' in queue line; got:\n%s", out)
	}
}

// TestRolesBlock_QueuePaused verifies that renderRolesBlock shows "paused"
// when a queue session is paused (AC-5b).
func TestRolesBlock_QueuePaused(t *testing.T) {
	rtDir := t.TempDir()
	sess := &spawn.QueueSession{
		Agent:     "gsd",
		Completed: []string{"TASK-001"},
		Paused:    true,
	}
	if err := spawn.WriteQueue(rtDir, sess); err != nil {
		t.Fatalf("WriteQueue: %v", err)
	}
	dev := &config.DeveloperConfig{Agent: "gsd"}
	out := renderRolesBlock(dev, rtDir)
	if !strings.Contains(out, "paused") {
		t.Errorf("AC-5b: expected 'paused' when queue paused; got:\n%s", out)
	}
}

// TestRolesBlock_EmptyRtDir verifies that renderRolesBlock degrades gracefully
// when rtDir is empty (no runtime dir available).
func TestRolesBlock_EmptyRtDir(t *testing.T) {
	dev := &config.DeveloperConfig{Agent: "gsd"}
	out := renderRolesBlock(dev, "")
	if !strings.Contains(out, "Queue") {
		t.Errorf("AC-5b: expected Queue line even with empty rtDir; got:\n%s", out)
	}
}
