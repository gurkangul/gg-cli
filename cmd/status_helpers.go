package cmd

import (
	"fmt"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
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
// developer command, transport, and queue state. rtDir may be empty (queue line
// defaults to "not started" when the runtime dir is unavailable).
func renderRolesBlock(dev *config.DeveloperConfig, rtDir string) string {
	if dev == nil {
		return ""
	}
	developerLine := developerCommand(dev)
	if developerLine == "" {
		developerLine = "⚠ unconfigured"
	} else if dev.Transport != "" {
		developerLine = developerLine + " (" + dev.Transport + ")"
	}
	queueLine := queueStatusLine(rtDir)
	return fmt.Sprintf("Roles\n  Developer  %s\n  Queue      %s\n\n", developerLine, queueLine)
}

func roleCommand(cfg *config.Config, role string) string {
	if cfg == nil {
		return ""
	}
	if role == "" || role == "developer" {
		return developerCommand(&cfg.Developer)
	}
	if cfg.Roles != nil {
		if rc, ok := cfg.Roles[role]; ok && rc.Command != "" && rc.Command != "unconfigured" {
			return rc.Command
		}
	}
	return ""
}

func developerCommand(dev *config.DeveloperConfig) string {
	if dev == nil {
		return ""
	}
	if dev.Command != "" && dev.Command != "unconfigured" {
		return dev.Command
	}
	if dev.SpawnCommand != "" && dev.SpawnCommand != "unconfigured" {
		return dev.SpawnCommand
	}
	if dev.Agent == "gsd-sonnet-4.6" {
		return "gsd"
	}
	return ""
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
	if sess.Done {
		return fmt.Sprintf("complete (completed: %d, skipped: %d)", len(sess.Completed), len(sess.Skipped))
	}
	return fmt.Sprintf("running (completed: %d, skipped: %d)", len(sess.Completed), len(sess.Skipped))
}
