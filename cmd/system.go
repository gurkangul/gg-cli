package cmd

import (
	"github.com/spf13/cobra"
)

// systemCmd groups cross-project host-level operations that act on the
// registry at ~/.gg/projects.json rather than a single project root.
var systemCmd = &cobra.Command{
	Use:   "system",
	Short: "Host-level gg operations (cross-project registry + sync)",
	Long: `Commands that operate on all gg-registered projects at once.

The registry lives at ~/.gg/projects.json and is populated automatically
by 'gg init'. Use 'gg system register' to add an existing project that
was initialized before the registry existed.`,
}

func init() {
	rootCmd.AddCommand(systemCmd)
}
