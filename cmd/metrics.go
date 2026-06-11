package cmd

import (
	"github.com/spf13/cobra"
)

var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: experimentalShort("Project health metrics"),
	Long: experimentalLong(`Compute and display project health metrics.

Subcommands:
  dogfood   Per-project velocity and rework rate for dogfood sessions`),
}

func init() {
	rootCmd.AddCommand(metricsCmd)
}
