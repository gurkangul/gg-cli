package cmd

import (
	"github.com/spf13/cobra"
)

var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: experimentalShort("Project health metrics"),
	Long: experimentalLong(`Compute and display project health metrics.

"Dogfood" metrics track how heavily agents actually use gg on this project
(velocity, rework/repeat-fix rate) — useful for teams running gg as their
durable memory and wanting a productivity/health scorecard.

Subcommands:
  dogfood   Per-project velocity and rework rate for dogfood sessions`),
}

func init() {
	rootCmd.AddCommand(metricsCmd)
}
