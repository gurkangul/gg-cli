package cmd

import (
	"github.com/spf13/cobra"
)

var masterCmd = &cobra.Command{
	Use:   "master",
	Short: "Master-session utilities: session handoff, resume state",
	Long: `Utilities for the master/orchestrator session that coordinates worker panes.

Subcommands:
  resume — print a structured session-handoff dump so a fresh master session
            can re-hydrate gg state without asking the user to re-explain.

Typical use:
  # At the start of a fresh master session, after the user types "devam" / "resume":
  gg master resume`,
}

func init() {
	rootCmd.AddCommand(masterCmd)
}
