// Package cmd — tests for refetch-rate warning and sessions rendering in gg status output.
package cmd

import (
	"os"
	"strings"
	"testing"

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
