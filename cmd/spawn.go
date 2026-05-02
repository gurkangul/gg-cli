package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
)

var spawnCmd = &cobra.Command{
	Use:   "spawn",
	Short: "Multi-agent orchestration: spawn worker panes, run queue, track liveness",
	Long: `Orchestrate multiple agent sessions from a single master terminal.

The master session owns the queue and maintains a liveness heartbeat.
Worker sessions run individual tasks in isolated terminal panes.

Subcommands:
  worker     — open a new pane and run an agent against a specific task
  queue      — drain pending tasks by spawning sequential workers
  heartbeat  — record master liveness (call every ~60s from master session)
  status     — show active sessions, workers, and heartbeat age

Typical flow:
  # Master terminal — start heartbeat loop and queue runner
  export GG_AGENT=claude-code
  gg spawn heartbeat          # initial heartbeat
  gg spawn queue start --agent gsd  # drains pending tasks

  # Open an interactive GSD pane directly (no queue required)
  gg gsd open

  # Worker terminals are opened automatically by ` + "`" + `gg spawn queue` + "`" + `.
  # Workers call ` + "`" + `gg spawn heartbeat` + "`" + ` via the master-guard hook to
  # ensure the master is still alive before closing tasks.`,
}

// spawnAgentDefault is the agent command used when --agent is not specified.
// Reads GG_SPAWN_AGENT env var, falls back to "gsd".
func spawnAgentDefault() string {
	if v := os.Getenv("GG_SPAWN_AGENT"); v != "" {
		return v
	}
	return "gsd"
}

// spawnRuntimeDir resolves the runtime dir or returns a user-friendly error.
func spawnRuntimeDir() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	rt, err := cfg.RuntimeDir()
	if err != nil {
		return "", fmt.Errorf("runtime dir: %w", err)
	}
	return rt, nil
}

func init() {
	rootCmd.AddCommand(spawnCmd)
}
