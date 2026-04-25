package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/enforcement"
)

// taskDoneVerifier is the role flag for the verifier-separation gate in
// `gg task done`. Read only when .gg/config.yaml has
// tasks.verifier_separation: true — otherwise ignored for back-compat.
var taskDoneVerifier string

var taskDoneCmd = &cobra.Command{
	Use:   `done TASK-ID "summary"`,
	Short: "Mark a task done — include a one-sentence summary of what was accomplished",
	Long: `Close a task and record a completion summary in the shared brain.

WHEN TO USE: you have finished the work described in the task. The summary is stored
and surfaced in 'gg status' and 'gg search' — write it for the next agent that reads it.

VERIFY GATE: before writing the new state, gg runs every executable *.sh in
.gg/hooks/pre-task-done.d/ in lexicographic order. Any non-zero exit aborts
the transition with exit code 7 (ExitVerifyFailed); the task stays in its
current state and a machine-parseable {"event":"verify_failed",...} line is
emitted to stderr along with an internal 'gg tell' to all agents.
Install starter scripts with 'gg doctor --install-task-hooks' (auto-detects
Go and Node/Bun).

READY-FOR-LIVE GATE (opt-in): when .gg/config.yaml has
tasks.require_ready_for_live: true, this command refuses unless the task is
already in status "ready_for_live" (transition it with 'gg task ready-for-live'
after local checks pass). Combined with tasks.verifier_separation: true the
command also requires --verifier <role> and rejects when the verifier is the
same actor that performed the ready-for-live transition. Prevents the
premature-closure / same-actor-verification pattern surfaced by the
dogfood audit 2026-04-19.

See also: gg task review (request peer review), gg record (capture design decisions made during the work)`,
	Args: cobra.ExactArgs(2),
	RunE: runTaskDone,
}

var taskBlockCmd = &cobra.Command{
	Use:   `block TASK-ID "reason"`,
	Short: "Mark a task blocked — state what specific dependency is missing",
	Long: `Signal that work is stalled because of an external dependency or unresolved question.

WHEN TO USE: you cannot make progress without input from another agent or an external
resource. The reason is stored and shown in 'gg status --blocked'.

WHEN NOT TO USE: for long-term deprioritization, update priority instead.`,
	Args: cobra.ExactArgs(2),
	RunE: runTaskBlock,
}

var taskDepsCmd = &cobra.Command{
	Use:   "deps TASK-ID",
	Short: "Show dependency status for a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskDeps,
}

func init() {
	taskDoneCmd.Flags().StringVar(&taskDoneVerifier, "verifier",
		"", "actor role that verified the live run (required when tasks.verifier_separation is true)")
	taskCmd.AddCommand(taskDoneCmd)
	taskCmd.AddCommand(taskBlockCmd)
	taskCmd.AddCommand(taskDepsCmd)
}

func runTaskDone(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	summary, err := requireNonEmpty("summary", args[1])
	if err != nil {
		return err
	}

	// Shared per-command config cache — loaded lazily on first use so the
	// pre-hook and post-hook paths don't each pay for a separate
	// config.GGDir + config.Load pair.
	hookCfg := &hookConfig{}

	// Pre-done verify gate: run .gg/hooks/pre-task-done.d/*.sh BEFORE touching
	// the store. Always strict by design — a gate that passes on failure is
	// not a gate. If any hook fails, the task stays in its current state and
	// we return ExitVerifyFailed so agents can detect the blocked transition.
	//
	// Opt-out: skipped when GG_ENFORCEMENT=off. Set it to opt out for a session.
	// Agent lifecycle gate: GSD / Sonnet agents are not permitted to close tasks.
	if !enforcement.Enabled() {
		if rej := emitGuardSkipEvent("agent-lifecycle-done", taskID); rej != nil {
			return rej
		}
	} else if rej := checkAgentLifecycleGate("done"); rej != nil {
		return rej
	}

	if !enforcement.Enabled() {
		// Emit an audit line so telemetry can count how often the gate was bypassed.
		if rej := emitGuardSkipEvent("pre-task-done", taskID); rej != nil {
			return rej
		}
	} else if rej := runGateHooks(cmd, hookCfg, "pre-task-done", taskID, summary); rej != nil {
		emitGateFailedEvent(cmd.ErrOrStderr(), rej)
		notifyGateFailure(cmd, rej) // best-effort: no-op if store unreachable or opted out
		return &ExitError{
			Code:    ExitVerifyFailed,
			Message: fmt.Sprintf("%s hook rejected %s: %s exited %d (task state unchanged)", rej.Gate, rej.TaskID, rej.Hook, rej.ExitCode),
		}
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// Ready-for-live gate (opt-in via .gg/config.yaml tasks.*). Runs AFTER
	// pre-task-done.d hooks so callers see hook failures before state-machine
	// complaints — cheaper feedback when both would reject. Same
	// GG_ENFORCEMENT=off escape hatch applies for emergencies.
	if _, cfg, cfgErr := hookCfg.load(cmd.ErrOrStderr()); cfgErr == nil && cfg != nil && cfg.Tasks.RequireReadyForLive {
		if !enforcement.Enabled() {
			if rej := emitGuardSkipEvent("pre-task-done-ready-for-live", taskID); rej != nil {
				return rej
			}
		} else {
			t, getErr := d.store.GetTask(ctx, taskID)
			if getErr != nil {
				return getErr
			}
			if rej := checkReadyForLiveGate(t, &cfg.Tasks, taskDoneVerifier); rej != nil {
				return rej
			}
		}
	}

	if err := d.store.UpdateTaskStatus(ctx, taskID, "done", summary); err != nil {
		return err
	}

	notifyTaskLifecycle(ctx, d.store, taskID, "done", summary)

	// Run post-done hooks from .gg/hooks/task-done.d/*.sh (warn-only unless hooks.strict=true).
	if hookErr := runTaskDoneHooks(cmd, hookCfg, taskID, summary); hookErr != nil {
		return hookErr // only non-nil when strict mode is enabled and a hook failed
	}

	warnBinaryStale()

	return printJSON(map[string]any{"id": taskID, "status": "done", "summary": summary}, func() {
		fmt.Printf("✓ %s marked as done\n", taskID)
	})
}

func runTaskBlock(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	reason, err := requireNonEmpty("reason", args[1])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.UpdateTaskStatus(ctx, taskID, "blocked", reason); err != nil {
		return err
	}

	notifyTaskLifecycle(ctx, d.store, taskID, "blocked", reason)

	return printJSON(map[string]any{"id": taskID, "status": "blocked", "reason": reason}, func() {
		fmt.Printf("⚠ %s marked as blocked: %s\n", taskID, reason)
	})
}

func runTaskDeps(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	t, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return notFound(err.Error())
	}

	type depEntry struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Priority string `json:"priority,omitempty"`
		Title    string `json:"title,omitempty"`
		Found    bool   `json:"found"`
	}

	var deps []depEntry
	allDone := true
	for _, depID := range t.DependsOn {
		dep, err := d.store.GetTask(ctx, depID)
		if err != nil {
			deps = append(deps, depEntry{ID: depID, Found: false})
			allDone = false
			continue
		}
		deps = append(deps, depEntry{ID: dep.ID, Status: dep.Status, Priority: dep.Priority, Title: dep.Title, Found: true})
		if dep.Status != "done" {
			allDone = false
		}
	}

	payload := map[string]any{
		"task_id":  taskID,
		"all_done": allDone,
		"deps":     deps,
	}
	return printJSON(payload, func() {
		if len(deps) == 0 {
			fmt.Printf("%s has no dependencies.\n", taskID)
			return
		}
		fmt.Printf("Dependencies of %s:\n", taskID)
		for _, dep := range deps {
			if !dep.Found {
				fmt.Printf("  ! %-12s (not found)\n", dep.ID)
				continue
			}
			fmt.Printf("  %s %-12s [%s] %s\n", statusIcon(dep.Status), dep.ID, dep.Priority, dep.Title)
		}
		fmt.Println()
		if allDone {
			fmt.Printf("✓ All dependencies done — %s is ready to start.\n", taskID)
		} else {
			fmt.Printf("○ Not all dependencies are done — %s is blocked by the above.\n", taskID)
		}
	})
}
