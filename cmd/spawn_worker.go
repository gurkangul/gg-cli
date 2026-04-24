package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
	"github.com/gurkangul/gg-cli/internal/store"
)

var spawnWorkerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Open a new terminal pane and run an agent against a task",
	Long: `Spawn a worker agent in a new terminal pane.

The worker pane inherits the current environment (GG_AGENT, GG_ROLE, etc.)
plus any additional KEY=VALUE pairs supplied via --env. A startup command is
sent to the pane to orient the agent: it exports GG_AGENT, exports
GG_TASK_ID, and runs 'gg task get <task-id>' to load task context.

The spawned pane is registered in the runtime spawn directory so
'gg spawn status' can list active workers.

Requires a terminal backend (GG_TERMINAL=cmux is default when cmux is in PATH).`,
	RunE: runSpawnWorker,
}

var (
	spawnWorkerAgent  string
	spawnWorkerTaskID string
	spawnWorkerEnvs   []string
	spawnWorkerDir    string // split direction: horizontal or vertical
)

func init() {
	spawnWorkerCmd.Flags().StringVar(&spawnWorkerAgent, "agent", "", "agent command to run in the new pane (default: $GG_SPAWN_AGENT or 'claude')")
	spawnWorkerCmd.Flags().StringVar(&spawnWorkerTaskID, "task", "", "task ID to assign to this worker (e.g. TASK-042)")
	spawnWorkerCmd.Flags().StringArrayVar(&spawnWorkerEnvs, "env", nil, "KEY=VALUE env vars to set in the worker pane (repeatable)")
	spawnWorkerCmd.Flags().StringVar(&spawnWorkerDir, "split", "horizontal", "pane split direction: horizontal or vertical")
	spawnCmd.AddCommand(spawnWorkerCmd)
}

func runSpawnWorker(cmd *cobra.Command, _ []string) error {
	// Validate task ID if provided.
	taskID := strings.ToUpper(strings.TrimSpace(spawnWorkerTaskID))
	if taskID != "" {
		if _, err := store.ParseTaskID(taskID); err != nil {
			return fmt.Errorf("--task: %w", err)
		}
	}

	agentCmd := spawnWorkerAgent
	if agentCmd == "" {
		agentCmd = spawnAgentDefault()
	}

	splitDir := terminal.SplitHorizontal
	if strings.ToLower(spawnWorkerDir) == "vertical" {
		splitDir = terminal.SplitVertical
	}

	// Build the env slice to pass to the pane.
	env := buildWorkerEnv(taskID, spawnWorkerEnvs)

	term, err := terminal.NewFromEnv()
	if err != nil {
		return fmt.Errorf("terminal backend: %w", err)
	}

	ctx := cmd.Context()
	surfaceID, err := term.NewSplit(ctx, terminal.SplitOpts{
		Dir:  splitDir,
		Env:  env,
		Cmd:  agentCmd,
	})
	if err != nil {
		return fmt.Errorf("open worker pane: %w", err)
	}

	// Send startup orientation to the new pane.
	if taskID != "" {
		startup := buildWorkerStartup(taskID)
		if sErr := term.Send(ctx, surfaceID, startup); sErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ could not send startup to pane %s: %v\n", surfaceID, sErr)
		}
		if kErr := term.SendKey(ctx, surfaceID, "Enter"); kErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ could not send Enter to pane %s: %v\n", surfaceID, kErr)
		}
	}

	// Register the worker pane in panes.json.
	rt, rtErr := spawnRuntimeDir()
	if rtErr == nil {
		w := spawn.WorkerPane{
			SurfaceID: string(surfaceID),
			TaskID:    taskID,
			Agent:     agentCmd,
			SpawnedAt: time.Now().UTC(),
		}
		if regErr := spawn.RegisterPane(rt, w); regErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ pane registration failed: %v\n", regErr)
		}
	}

	return printJSON(map[string]any{
		"surface_id": string(surfaceID),
		"task_id":    taskID,
		"agent":      agentCmd,
	}, func() {
		if taskID != "" {
			fmt.Printf("✓ Worker pane %s opened for %s (agent: %s)\n", surfaceID, taskID, agentCmd)
		} else {
			fmt.Printf("✓ Worker pane %s opened (agent: %s)\n", surfaceID, agentCmd)
		}
	})
}

// buildWorkerEnv constructs the env slice for the new pane.
// Always exports GG_TASK_ID (if set) and inherits GG_AGENT from the current env.
func buildWorkerEnv(taskID string, extra []string) []string {
	var env []string
	if v := os.Getenv("GG_AGENT"); v != "" {
		env = append(env, "GG_AGENT="+v)
	}
	if v := os.Getenv("GG_ROLE"); v != "" {
		env = append(env, "GG_ROLE="+v)
	}
	if taskID != "" {
		env = append(env, "GG_TASK_ID="+taskID)
	}
	// Propagate project root so the worker doesn't need to cd.
	if root, err := config.FindRoot(); err == nil {
		env = append(env, "GG_PROJECT_ROOT="+root)
	}
	env = append(env, extra...)
	return env
}

// buildWorkerStartup returns the shell commands that orient the worker agent.
// The startup is sent as text to stdin of the new pane.
func buildWorkerStartup(taskID string) string {
	return fmt.Sprintf("gg task get %s && echo 'GG_TASK_ID=%s ready'", taskID, taskID)
}
