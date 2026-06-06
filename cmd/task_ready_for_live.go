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
func checkReadyForLiveGate(t *store.Task, tasksCfg *config.TasksConfig, verifier, currentAgent string) *ExitError {
	if t == nil || tasksCfg == nil || !tasksCfg.RequireReadyForLive {
		return nil
	}
	if t.Status != "ready_for_live" {
		return &ExitError{
			Code: ExitVerifyFailed,
			Message: fmt.Sprintf(
				"missing durable review handoff for %s: status is %q, expected \"ready_for_live\" before closure. Record reviewer plan and evidence with 'gg task ready-for-live %s --plan \"Reviewer: ... Evidence: ...\"' first (task state unchanged)",
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
				"missing verifier evidence for %s: --verifier <role> is required when tasks.verifier_separation is true so future reviewers can see who independently verified closure (task state unchanged)",
				t.ID),
		}
	}
	if v == t.ReadyForLiveBy {
		return &ExitError{
			Code: ExitVerifyFailed,
			Message: fmt.Sprintf(
				"missing independent verification for %s: --verifier=%q matches the actor that set ready_for_live; durable review evidence must come from a different role before closure (task state unchanged)",
				t.ID, v),
		}
	}
	// BUG-067: role strings are self-declared and spoofable (one runtime can set
	// --from implementer then --verifier reviewer). Reject when the runtime
	// identity closing the task is the same one that set ready_for_live — a
	// single process cannot be both implementer and independent verifier. This
	// is an advisory (not cryptographic) separation; with per-session GG_AGENT
	// (BUG-084) two real tabs have distinct identities and pass.
	ca := strings.TrimSpace(currentAgent)
	if ca != "" && t.ReadyForLiveAgent != "" && ca == t.ReadyForLiveAgent {
		return &ExitError{
			Code: ExitVerifyFailed,
			Message: fmt.Sprintf(
				"missing independent verification for %s: closer identity %q is the same runtime that set ready_for_live; an independent verifier (different runtime/agent) must close it (task state unchanged)",
				t.ID, ca),
		}
	}
	return nil
}

// taskReadyForLiveFrom is the actor-role flag for `gg task ready-for-live`.
// Falls back to $GG_ROLE / $GG_AGENT when the flag is empty.
var taskReadyForLiveFrom string

// taskReadyForLivePlan is an optional flag alias for the positional verify plan.
var taskReadyForLivePlan string

var taskReadyForLiveCmd = &cobra.Command{
	Use:   `ready-for-live TASK-ID ["verify plan"]`,
	Short: "Record implementation evidence for independent live verification",
	Long: `Record the durable handoff that tells a reviewer or future agent what evidence exists
and what still needs independent live verification. Writes the actor (from --from or $GG_ROLE)
alongside the timestamp so 'gg task done --verifier <role>' can require a separate verifier
when .gg/config.yaml has tasks.verifier_separation: true.

WHEN TO USE: you have finished implementing and local tests (unit + integration) are green,
but production-shaped verification (live e2e / make e2e-cold / manual smoke) has not yet
been run by an independent role. The verify plan should be one sentence describing what
the live-verifier is expected to exercise plus a compact evidence summary: commands run,
live smoke result, impacted files checked with gg impact, known gaps, and artifact paths.

The plan is stored on the task and surfaced by 'gg task get'. If a task is already
ready_for_live, running this command again updates that stored plan without
changing state; use either the positional plan or --plan.

Example:
  gg task ready-for-live TASK-123 --from "$GG_ROLE" --plan "Reviewer: inspect diff and rerun smoke. Evidence: commands=go test ./... -count=1; live=CLI smoke passed; impact=cmd/foo.go checked with gg impact; gaps=none; artifacts=.artifacts/TASK-123-smoke.txt"

See also: gg task done (close after verifier sign-off), gg tell --task (handoff message).`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runTaskReadyForLive,
}

func init() {
	taskReadyForLiveCmd.Flags().StringVar(&taskReadyForLiveFrom, "from",
		"", "role performing the transition (defaults to $GG_ROLE / $GG_AGENT)")
	taskReadyForLiveCmd.Flags().StringVar(&taskReadyForLivePlan, "plan",
		"", "verify plan/evidence summary to store on the task (alternative to positional plan)")
	taskCmd.AddCommand(taskReadyForLiveCmd)
}

func readyForLivePlanFromArgs(args []string, flagPlan string) (string, error) {
	positional := ""
	if len(args) > 0 {
		positional = strings.TrimSpace(args[0])
	}
	flagPlan = strings.TrimSpace(flagPlan)
	if positional != "" && flagPlan != "" {
		return "", fmt.Errorf("verify plan specified twice — use either positional plan or --plan, not both")
	}
	if flagPlan != "" {
		return requireNonEmpty("verify plan", flagPlan)
	}
	return requireNonEmpty("verify plan", positional)
}

func runTaskReadyForLive(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	plan, err := readyForLivePlanFromArgs(args[1:], taskReadyForLivePlan)
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
		return fmt.Errorf("--from is required (or set GG_ROLE / GG_AGENT) — ready-for-live evidence needs a durable actor for reviewer handoff")
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
