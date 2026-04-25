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

var becomeCmd = &cobra.Command{
	Use:   "become",
	Short: "Adopt a project role (e.g. become master)",
	Long:  `Adopt a coordination role in the current project.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

var becomeMasterCmd = &cobra.Command{
	Use:   "master",
	Short: "Install master-role-extras block and record liveness heartbeat",
	Long: `Opt this session into the master role for the current project.

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

func runBecomeMaster(_ *cobra.Command, _ []string) error {
	projectRoot, err := config.FindRoot()
	if err != nil {
		return err
	}

	// Step 1: install / update master-role block.
	lines, fixErr := agenthooks.FixMasterRole(projectRoot, becomeForceReset)
	for _, l := range lines {
		fmt.Println(l)
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
	fmt.Printf("%s  Master-role block  %-8s  %s  (version %s)\n",
		marker, r.Status, shortPath, agenthooks.MasterRoleVersion()[:12])
	fmt.Println(strings.Repeat("─", 50))

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
	return printJSON(map[string]any{
		"status":      "ok",
		"block_state": r.Status.String(),
		"agent":       agent,
		"updated_at":  hb.UpdatedAt,
	}, func() {
		fmt.Printf("✓  Heartbeat recorded (agent: %s)\n", agent)
		fmt.Println()
		fmt.Println("Master role active. Next steps:")
		fmt.Println("  gg spawn heartbeat        # refresh liveness every ~60 s")
		fmt.Println("  gg spawn worker --task T  # open a worker pane")
	})
}
