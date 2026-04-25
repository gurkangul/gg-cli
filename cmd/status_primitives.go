package cmd

import (
	"fmt"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

func pct(part, total int) int {
	if total == 0 {
		return 0
	}
	return (part * 100) / total
}

func fmtCount(n uint64, err error) string {
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%d", n)
}

// renderRolesBlock formats the Roles section for gg status, showing the
// developer agent, transport, and queue state. rtDir may be empty (queue line
// defaults to "not started" when the runtime dir is unavailable).
func renderRolesBlock(dev *config.DeveloperConfig, rtDir string) string {
	if dev == nil {
		return ""
	}
	developerLine := dev.Agent
	if developerLine == "" {
		developerLine = "⚠ unconfigured"
	} else if dev.Transport != "" {
		developerLine = dev.Agent + " (" + dev.Transport + ")"
	}
	queueLine := queueStatusLine(rtDir)
	return fmt.Sprintf("Roles\n  Developer  %s\n  Queue      %s\n\n", developerLine, queueLine)
}

// queueStatusLine returns a one-line summary of the current queue state.
// rtDir may be empty; in that case "not started" is returned.
func queueStatusLine(rtDir string) string {
	if rtDir == "" {
		return "not started — single-pane mode"
	}
	sess, err := spawn.ReadQueue(rtDir)
	if err != nil {
		// ErrNoQueue or any read error → not started.
		return "not started — single-pane mode (run gg spawn queue start for parallel pickup)"
	}
	if sess.Paused {
		return fmt.Sprintf("paused (completed: %d, skipped: %d)", len(sess.Completed), len(sess.Skipped))
	}
	return fmt.Sprintf("running (completed: %d, skipped: %d)", len(sess.Completed), len(sess.Skipped))
}

// renderSessionsBlock formats the Sessions summary line and, when
// OverThresholdCount > 0, the ⚠ Sessions warning line. Callers must
// only call this when ssum.ActiveSessions > 0.
func renderSessionsBlock(ssum *telemetry.SessionSummary) string {
	out := fmt.Sprintf("  Sessions    %d active, avg %.1f compact calls/session, p50 %.1f KB p95 %.1f KB cumulative\n",
		ssum.ActiveSessions, ssum.AvgCompactCallsPerSession,
		ssum.P50CumulativeKB, ssum.P95CumulativeKB)
	if ssum.OverThresholdCount > 0 {
		out += fmt.Sprintf("  ⚠ Sessions  %d session(s) exceeded 100 KB compact output — agent context filling fast\n",
			ssum.OverThresholdCount)
	}
	return out
}
