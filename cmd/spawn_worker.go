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
	spawnWorkerCmd.Flags().StringVar(&spawnWorkerAgent, "agent", "", "agent command to run in the new pane (default: $GG_SPAWN_AGENT or 'gsd')")
	spawnWorkerCmd.Flags().StringVar(&spawnWorkerTaskID, "task", "", "task ID to assign to this worker (e.g. TASK-042)")
	spawnWorkerCmd.Flags().StringArrayVar(&spawnWorkerEnvs, "env", nil, "KEY=VALUE env vars to set in the worker pane (repeatable)")
	spawnWorkerCmd.Flags().StringVar(&spawnWorkerDir, "split", "vertical", "pane split direction: horizontal (below) or vertical (right, default)")
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
	// Note: cmux new-split does not run an initial command — we launch the agent
	// via Send() after the pane opens.
	surfaceID, err := term.NewSplit(ctx, terminal.SplitOpts{
		Dir: splitDir,
		Env: env,
	})
	if err != nil {
		return fmt.Errorf("open worker pane: %w", err)
	}

	// Bootstrap sequence:
	//   (1) Type the agent launcher (e.g. "gsd" or "claude") and press Enter.
	//   (2) Wait for the agent REPL to become interactive (heuristic sleep).
	//   (3) Type the task-orientation prompt and press Enter.
	// Without this, the pane would stay as a raw shell and the worker would never
	// pick up the task. Warnings on failure are best-effort — a human can always
	// complete the handshake manually.
	if sErr := term.Send(ctx, surfaceID, agentCmd); sErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ could not launch agent in pane %s: %v\n", surfaceID, sErr)
	}
	if kErr := term.SendKey(ctx, surfaceID, "enter"); kErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ could not send Enter after agent launch: %v\n", kErr)
	}

	if taskID != "" {
		// Give the agent REPL a couple seconds to initialize before typing the prompt.
		// TODO: replace with ReadScreen polling once the agents expose a stable ready marker.
		time.Sleep(3 * time.Second)

		prompt := buildWorkerPrompt(taskID)
		if sErr := term.Send(ctx, surfaceID, prompt); sErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ could not send task prompt to pane %s: %v\n", surfaceID, sErr)
		}
		if kErr := term.SendKey(ctx, surfaceID, "enter"); kErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ could not send Enter after prompt: %v\n", kErr)
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

// buildWorkerStartup returns a shell command that orients an agentless pane —
// retained for tests / debugging flows that spawn a raw shell.
func buildWorkerStartup(taskID string) string {
	return fmt.Sprintf("gg task get %s && echo 'GG_TASK_ID=%s ready'", taskID, taskID)
}

// buildWorkerPrompt returns the chat prompt to send to a running agent REPL.
// Unlike buildWorkerStartup, this is plain English orientation — the agent
// reads it as a user message, not as a shell command.
func buildWorkerPrompt(taskID string) string {
	return fmt.Sprintf("You are working on %s. Run 'gg task get %s' to load the full spec, then implement it and commit. Signal completion via: gg tell claude-code \"%s commit <sha>, tests green\" --from developer", taskID, taskID, taskID)
}
