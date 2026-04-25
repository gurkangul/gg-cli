package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
)

var becomeForceReset bool

// masterPrompt is the paste-ready prompt printed to stdout after `gg become master`
// so the operator can immediately orient their AI session into the master role.
const masterPrompt = "You are now the MASTER session for this project. Read CLAUDE.md (gg:master-role:begin v3) for your responsibilities. Your job: spawn workers via gg spawn worker, review their commits, never write production code (≤5 line cosmetic exception only). Heartbeat starting now."

var becomeCmd = &cobra.Command{
	Use:   "become",
	Short: "Adopt a project role (e.g. become master)",
	Long:  `Adopt a coordination role in the current project.`,
	RunE:  runBecomeNoArg,
}

var becomeMasterCmd = &cobra.Command{
	Use:   "master",
	Short: "Install master-role-extras block and record liveness heartbeat",
	Long: `Opt-in: master discipline (review gate, worker lifecycle, bypass audit) applies only after this command runs.

Opt this session into the master role for the current project.

Two things happen:

  1. The master-role-extras block is installed (or updated) in CLAUDE.md.
     This block contains the master orchestration protocol: worker lifecycle,
     pane management, review responsibilities, resume protocol, and bypass
     discipline. If the block is already current, no change is made.

  2. A heartbeat is written to the project's runtime directory so worker
     sessions know a master is present. Workers read this liveness signal
     via the 46-master-guard hook before closing tasks.

Run this once per master session. Wire 'gg spawn heartbeat' into a loop
(e.g. every 60 s) to maintain liveness across a long session.

After running, open the first worker with:
  gg spawn worker --task TASK-NNN`,
	RunE: runBecomeMaster,
}

func init() {
	becomeMasterCmd.Flags().BoolVar(&becomeForceReset, "force-reset", false, "overwrite DRIFTED (malformed) master-role markers")
	becomeCmd.AddCommand(becomeMasterCmd)
	rootCmd.AddCommand(becomeCmd)
}

func runBecomeNoArg(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(out, "no role declared (run gg become master to opt in)")
	} else if cfg.Developer.Agent != "" && cfg.Developer.Agent != "unconfigured" {
		fmt.Fprintf(out, "Current role hint: %s\n", cfg.Developer.Agent)
	} else {
		fmt.Fprintln(out, "no role declared (run gg become master to opt in)")
	}
	return cmd.Help()
}

func runBecomeMaster(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()

	projectRoot, err := config.FindRoot()
	if err != nil {
		return err
	}

	// Step 1: install / update master-role block.
	lines, fixErr := agenthooks.FixMasterRole(projectRoot, becomeForceReset)
	for _, l := range lines {
		fmt.Fprintln(out, l)
	}
	if fixErr != nil {
		return fixErr
	}

	// Report final block status.
	r := agenthooks.CheckMasterRole(projectRoot)
	shortPath := r.Path
	if rel, relErr := filepath.Rel(projectRoot, r.Path); relErr == nil {
		shortPath = rel
	}
	marker := "✓"
	if r.Status != agenthooks.MasterRoleOK {
		marker = "✗"
	}
	fmt.Fprintf(out, "%s  Master-role block  %-8s  %s  (version %s)\n",
		marker, r.Status, shortPath, agenthooks.MasterRoleVersion()[:12])
	fmt.Fprintln(out, strings.Repeat("─", 50))

	if r.Status != agenthooks.MasterRoleOK {
		return fmt.Errorf("master-role block not OK after fix — run `gg doctor --check-master-role --fix` for details")
	}

	// Step 2: write heartbeat so workers know a master is present.
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	agent := os.Getenv("GG_AGENT")
	if agent == "" {
		agent = os.Getenv("GG_ROLE")
	}

	if err := spawn.WriteHeartbeat(rt, agent); err != nil {
		return fmt.Errorf("write heartbeat: %w", err)
	}

	hb, _ := spawn.ReadHeartbeat(rt)
	if jsonOutput {
		return writeJSON(map[string]any{
			"status":        "ok",
			"block_state":   r.Status.String(),
			"agent":         agent,
			"updated_at":    hb.UpdatedAt,
			"master_prompt": masterPrompt,
		})
	}
	fmt.Fprintf(out, "✓  Heartbeat recorded (agent: %s)\n", agent)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "── Paste this prompt into your master session ──────────────────")
	fmt.Fprintln(out, masterPrompt)
	fmt.Fprintln(out, "────────────────────────────────────────────────────────────────")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Master role active. Next steps:")
	fmt.Fprintln(out, "  gg spawn heartbeat        # refresh liveness every ~60 s")
	fmt.Fprintln(out, "  gg spawn worker --task T  # open a worker pane")
	return nil
}
