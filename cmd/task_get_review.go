package cmd

import "github.com/spf13/cobra"

var taskGetReview bool

func init() {
	taskGetCmd.Flags().BoolVar(&taskGetReview, "review", false, "render the reviewer handoff packet (alias for gg task packet TASK-ID)")
	baseRun := taskGetCmd.RunE
	taskGetCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if taskGetReview {
			return runTaskPacket(cmd, args)
		}
		return baseRun(cmd, args)
	}
}
