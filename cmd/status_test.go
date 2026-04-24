// Package cmd — tests for refetch-rate warning in gg telemetry summary output.
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
