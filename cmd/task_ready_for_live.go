package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/enforcement"
	"github.com/gurkangul/gg-cli/internal/store"
)

// checkReadyForLiveGate is the pure predicate enforcing the opt-in
// ready-for-live + verifier-separation gates configured via
// .gg/config.yaml (tasks.require_ready_for_live / tasks.verifier_separation).
// Returns nil when the transition is allowed, or an *ExitError with
// ExitVerifyFailed and a machine-parseable message describing why the
// transition is refused. The pure signature lets tests exercise every
// branch without a live store or hook runtime.
func checkReadyForLiveGate(t *store.Task, tasksCfg *config.TasksConfig, verifier string) *ExitError {
	if t == nil || tasksCfg == nil || !tasksCfg.RequireReadyForLive {
		return nil
	}
	if t.Status != "ready_for_live" {
		return &ExitError{
			Code: ExitVerifyFailed,
			Message: fmt.Sprintf(
				"ready_for_live gate rejected %s: status is %q, expected \"ready_for_live\" — run 'gg task ready-for-live %s \"<plan>\"' first (task state unchanged)",
				t.ID, t.Status, t.ID),
		}
	}
	if !tasksCfg.VerifierSeparation {
		return nil
	}
	v := strings.TrimSpace(verifier)
	if v == "" {
		return &ExitError{
			Code: ExitVerifyFailed,
			Message: fmt.Sprintf(
				"verifier-separation gate rejected %s: --verifier <role> is required when tasks.verifier_separation is true (task state unchanged)",
				t.ID),
		}
	}
	if v == t.ReadyForLiveBy {
		return &ExitError{
			Code: ExitVerifyFailed,
			Message: fmt.Sprintf(
				"verifier-separation gate rejected %s: --verifier=%q matches the actor that set ready_for_live — a different role must close (task state unchanged)",
				t.ID, v),
		}
	}
	return nil
}

// taskReadyForLiveFrom is the actor-role flag for `gg task ready-for-live`.
// Falls back to $GG_ROLE / $GG_AGENT when the flag is empty.
var taskReadyForLiveFrom string

var taskReadyForLiveCmd = &cobra.Command{
	Use:   `ready-for-live TASK-ID "verify plan"`,
	Short: "Mark a task as ready for live verification — transitions in_progress → ready_for_live",
	Long: `Record that an implementation is complete and ready for an independent live-verifier
to run against the live environment. Writes the actor (from --from or $GG_ROLE) alongside
the timestamp so 'gg task done --verifier <role>' can enforce same-actor-cannot-verify
when .gg/config.yaml has tasks.verifier_separation: true.

WHEN TO USE: you have finished implementing and local tests (unit + integration) are green,
but production-shaped verification (live e2e / make e2e-cold / manual smoke) has not yet
been run by an independent role. The verify plan should be one sentence describing what
the live-verifier is expected to exercise.

The plan is stored on the task and surfaced by 'gg task get'.

See also: gg task done (close after verifier sign-off).`,
	Args: cobra.ExactArgs(2),
	RunE: runTaskReadyForLive,
}

func init() {
	taskReadyForLiveCmd.Flags().StringVar(&taskReadyForLiveFrom, "from",
		"", "role performing the transition (defaults to $GG_ROLE / $GG_AGENT)")
	taskCmd.AddCommand(taskReadyForLiveCmd)
}

func runTaskReadyForLive(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	plan, err := requireNonEmpty("verify plan", args[1])
	if err != nil {
		return err
	}

	// Agent lifecycle gate: role-based and agent-agnostic. Implementation
	// workers may set ready-for-live; only reviewer/verifier transitions are
	// blocked for implementation roles.
	if !enforcement.Enabled() {
		if rej := emitGuardSkipEvent("agent-lifecycle-ready-for-live", ""); rej != nil {
			return rej
		}
	} else if rej := checkAgentLifecycleGate("ready-for-live"); rej != nil {
		return rej
	}

	actor := strings.TrimSpace(taskReadyForLiveFrom)
	if actor == "" {
		actor = os.Getenv("GG_ROLE")
	}
	if actor == "" {
		actor = os.Getenv("GG_AGENT")
	}
	if actor == "" {
		return fmt.Errorf("--from is required (or set GG_ROLE / GG_AGENT) — verifier-separation needs a non-empty actor")
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	if err := enforceTaskHydrationGate(cmd.ErrOrStderr(), nil, taskID, "task ready-for-live", "compact-hydration-task-ready-for-live"); err != nil {
		return err
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.SetReadyForLive(ctx, taskID, actor, plan); err != nil {
		return err
	}

	notifyTaskLifecycle(ctx, d.store, taskID, "ready_for_live", plan)

	return printJSON(map[string]any{
		"id":                  taskID,
		"status":              "ready_for_live",
		"ready_for_live_by":   actor,
		"ready_for_live_plan": plan,
	}, func() {
		fmt.Printf("✓ %s marked as ready for live verification\n", taskID)
		fmt.Printf("  By:   %s\n", actor)
		fmt.Printf("  Plan: %s\n", plan)
		fmt.Printf("\nNext: an independent verifier runs live checks, then:\n")
		fmt.Printf("  gg task done %s \"summary\" --verifier <role>\n", taskID)
	})
}
