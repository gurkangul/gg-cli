package cmd

import (
	"context"
	"fmt"
	"io"
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

When --task is provided, gg checks task state before opening a pane. Blocked,
done, ready_for_live, and dependency-blocked tasks are refused so agents do not
start the wrong lifecycle step.

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

var (
	spawnAgentPromptDelay   = 3 * time.Second
	spawnAgentReadyTimeout  = 30 * time.Second
	spawnAgentReadyInterval = 250 * time.Millisecond
)

func init() {
	spawnWorkerCmd.Flags().StringVar(&spawnWorkerAgent, "agent", "", "agent command to run in the new pane (default: $GG_SPAWN_AGENT or developer.command)")
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
		d, err := loadDeps(false)
		if err != nil {
			return err
		}
		defer d.Close()
		if err := preflightSpawnWorkerTask(cmd.Context(), d.store, taskID); err != nil {
			return err
		}
	}

	agentCmd := spawnWorkerAgent
	if agentCmd == "" {
		agentCmd = spawnAgentDefault()
		if agentCmd == "" {
			return developerCommandUnconfiguredError()
		}
	}

	splitDir := terminal.SplitHorizontal
	if strings.ToLower(spawnWorkerDir) == "vertical" {
		splitDir = terminal.SplitVertical
	}

	// Build the env slice to pass to the pane.
	env := buildWorkerEnv(taskID, spawnWorkerEnvs, agentCmd)

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

	bootstrapAgentInPane(ctx, term, surfaceID, buildAgentLaunchCommand(agentCmd), taskID, cmd.ErrOrStderr())

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

// buildWorkerEnv constructs the env slice for the new pane. The worker gets
// its own runtime identity + developer role; the spawning session is preserved
// separately as GG_MASTER_* so completion messages can still route back.
func buildWorkerEnv(taskID string, extra []string, agentCmd string) []string {
	var env []string
	env = append(env, "GG_AGENT="+workerAgentIdentity(agentCmd))
	env = append(env, "GG_ROLE=developer")
	if v := os.Getenv("GG_AGENT"); v != "" {
		env = append(env, "GG_MASTER_AGENT="+v)
	}
	if v := os.Getenv("GG_ROLE"); v != "" {
		env = append(env, "GG_MASTER_ROLE="+v)
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

func workerAgentIdentity(agentCmd string) string {
	if isGSDLikeAgent(agentCmd) {
		return "gsd"
	}
	fields := strings.Fields(strings.TrimSpace(agentCmd))
	if len(fields) == 0 {
		return "developer"
	}
	base := fields[0]
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	switch base {
	case "codex", "claude", "claude-code", "cursor", "aider":
		return base
	default:
		return "developer"
	}
}

// buildWorkerStartup returns a shell command that orients an agentless pane —
// retained for tests / debugging flows that spawn a raw shell.
func buildWorkerStartup(taskID string) string {
	return fmt.Sprintf("gg task get %s && echo 'GG_TASK_ID=%s ready'", taskID, taskID)
}

func buildAgentLaunchCommand(agentCmd string) string {
	root, err := config.FindRoot()
	if err != nil {
		return "exec " + agentCmd
	}
	return "cd " + shellQuote(root) + " && exec " + agentCmd
}

// buildWorkerPrompt returns the chat prompt to send to a running agent REPL.
// Unlike buildWorkerStartup, this is plain English orientation — the agent
// reads it as a user message, not as a shell command. Keep it single-line:
// terminal backends deliver literal newlines as Enter keypresses, and GSD
// treats those as separate user prompts.
func buildWorkerPrompt(taskID string) string {
	return fmt.Sprintf(
		"You are working on %s. Before anything else, export your identity so gg commands are attributed correctly: "+
			"export GG_AGENT=${GG_AGENT:-developer} GG_ROLE=developer. "+
			"Then run 'gg task get %s --json' to load the full spec. Before writing code, paraphrase every acceptance criterion in your own words and send the ACK: "+
			"gg task ack %s \"AC-1: <my paraphrase>; AC-2: <my paraphrase>; AC-N: <my paraphrase>\". "+
			"Then wait for master to reply ACK-OK or ACK-FIX. If no reply arrives within 5 minutes, you may proceed, but your commit body must include ACK-IMPLICIT and expect higher review risk. "+
			"Impact analysis — before editing any file: extract file paths the spec explicitly mentions; "+
			"for each, run `gg impact --compact PATH` to see graph dependents (callers, exported symbols, historical bugs). "+
			"Note any dependents whose tests must still pass after your change. "+
			"Cite this in your commit body under an `Impact-Reviewed:` trailer line "+
			"(e.g. `Impact-Reviewed: cmd/spawn_worker.go — 2 callers, tests green`). "+
			"Before claiming ready, run a review-convergence pass: compare acceptance criteria, implementation diff, tests, docs/hooks, and prior review findings; "+
			"cite the result in your commit body under a `Review-Convergence:` trailer line. "+
			"After committing, transition lifecycle ownership as the implementer with `gg task ready-for-live %s \"<one-sentence verification plan>\" --from developer`, "+
			"then signal the master queue with `gg spawn advance --task %s --commit $(git rev-parse HEAD)` "+
			"and send completion via: gg tell %s \"%s commit <sha>, tests green\" --from developer --audience agents. "+
			"Do not stop at prose confirmation; run the next required shell command.",
		taskID, taskID, taskID, taskID, taskID, masterMessageTargetCSV(), taskID,
	)
}

type spawnTaskReader interface {
	GetTask(ctx context.Context, taskID string) (*store.Task, error)
}

func preflightSpawnWorkerTask(ctx context.Context, tasks spawnTaskReader, taskID string) error {
	task, err := tasks.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("spawn preflight: %w", err)
	}

	switch task.Status {
	case "blocked":
		reason := strings.TrimSpace(task.BlockReason)
		if reason == "" {
			reason = "no block reason recorded"
		}
		return fmt.Errorf("spawn preflight: %s is blocked: %s", taskID, reason)
	case "done":
		return fmt.Errorf("spawn preflight: %s is already done; refusing to open an implementation worker", taskID)
	case "ready_for_live":
		return fmt.Errorf("spawn preflight: %s is ready_for_live; assign a verifier/reviewer instead of opening an implementation worker", taskID)
	case "pending", "in_progress":
		// allowed after dependency checks below
	default:
		return fmt.Errorf("spawn preflight: %s has unsupported status %q", taskID, task.Status)
	}

	var unfinished []string
	for _, depID := range task.DependsOn {
		dep, err := tasks.GetTask(ctx, depID)
		if err != nil {
			unfinished = append(unfinished, fmt.Sprintf("%s (not found)", depID))
			continue
		}
		if dep.Status != "done" {
			unfinished = append(unfinished, fmt.Sprintf("%s (%s)", dep.ID, dep.Status))
		}
	}
	if len(unfinished) > 0 {
		return fmt.Errorf("spawn preflight: %s has unfinished dependencies: %s; run `gg task deps %s` and work the blocker first", taskID, strings.Join(unfinished, ", "), taskID)
	}

	return nil
}

// bootstrapAgentInPane launches the agent REPL in surfaceID and orients it to taskID.
// Sequence: send agentCmd + Enter, wait for GSD readiness when applicable,
// send orientation prompt + Enter.
// When taskID is empty, only the agent is launched (no orientation step).
// Send/SendKey failures are logged to errOut and skipped — a human can always finish manually.
// Both the single-worker (cmd/spawn_worker.go) and queue-pool (cmd/spawn_queue_pool.go) paths
// route through this helper so they cannot diverge again.
func bootstrapAgentInPane(ctx context.Context, term terminal.Terminal, surfaceID terminal.SurfaceID, agentCmd, taskID string, errOut io.Writer) {
	if sErr := term.Send(ctx, surfaceID, agentCmd); sErr != nil {
		fmt.Fprintf(errOut, "⚠ could not launch agent in pane %s: %v\n", surfaceID, sErr)
	}
	if kErr := term.SendKey(ctx, surfaceID, "enter"); kErr != nil {
		fmt.Fprintf(errOut, "⚠ could not send Enter after agent launch: %v\n", kErr)
	}
	if taskID == "" {
		return
	}
	if isGSDLikeAgent(agentCmd) && !waitForAgentPromptReady(ctx, term, surfaceID, agentCmd, errOut) {
		return
	}
	prompt := buildWorkerPromptForAgent(agentCmd, taskID)
	if sErr := term.Send(ctx, surfaceID, prompt); sErr != nil {
		fmt.Fprintf(errOut, "⚠ could not send task prompt to pane %s: %v\n", surfaceID, sErr)
	}
	if kErr := term.SendKey(ctx, surfaceID, "enter"); kErr != nil {
		fmt.Fprintf(errOut, "⚠ could not send Enter after prompt: %v\n", kErr)
	}
}

func buildWorkerPromptForAgent(agentCmd, taskID string) string {
	if isGGDevWorkerAgent(agentCmd) {
		return "RUN_TASK " + taskID
	}
	return buildWorkerPrompt(taskID)
}

func waitForAgentPromptReady(ctx context.Context, term terminal.Terminal, surfaceID terminal.SurfaceID, agentCmd string, errOut io.Writer) bool {
	if !isGSDLikeAgent(agentCmd) {
		time.Sleep(spawnAgentPromptDelay)
		return true
	}
	if !term.Capabilities().CanReadScreen {
		fmt.Fprintf(errOut, "⚠ GSD pane %s cannot be read (screen capability unavailable); skipping task prompt delivery\n", surfaceID)
		return false
	}

	deadline := time.NewTimer(spawnAgentReadyTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(spawnAgentReadyInterval)
	defer ticker.Stop()

	for {
		content, err := term.ReadScreen(ctx, surfaceID)
		if err != nil {
			fmt.Fprintf(errOut, "⚠ could not verify GSD ready state in pane %s: %v; skipping task prompt delivery\n", surfaceID, err)
			return false
		}
		if isGSDReadyScreen(content) {
			return true
		}

		select {
		case <-ctx.Done():
			fmt.Fprintf(errOut, "⚠ context canceled while waiting for GSD ready UI in pane %s; skipping task prompt delivery\n", surfaceID)
			return false
		case <-deadline.C:
			fmt.Fprintf(errOut, "⚠ GSD pane %s did not show ready UI within %s; skipping task prompt delivery\n", surfaceID, spawnAgentReadyTimeout)
			return false
		case <-ticker.C:
		}
	}
}

func isGSDLikeAgent(agentCmd string) bool {
	trimmed := strings.TrimSpace(agentCmd)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "exec gsd") {
		return true
	}
	if isGGDevWorkerAgent(trimmed) {
		return true
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	base := fields[0]
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	return base == "gsd"
}

func isGGDevWorkerAgent(agentCmd string) bool {
	return strings.Contains(strings.TrimSpace(agentCmd), "ggdev-worker")
}

func isGSDReadyScreen(screen []byte) bool {
	text := strings.ToLower(string(screen))
	if strings.Contains(text, "ggdev-worker ready") {
		return true
	}
	if !strings.Contains(text, "get shit done") {
		return false
	}
	return strings.Contains(text, "/gsd to begin") ||
		strings.Contains(text, "project initialized") ||
		strings.Contains(text, "system ok") ||
		strings.Contains(text, "mcp client ready")
}
