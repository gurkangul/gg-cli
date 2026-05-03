package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/contextartifacts"
)

var contextArtifactsCmd = &cobra.Command{
	Use:   "artifacts",
	Short: "Manage project-local context artifacts",
}

var contextArtifactsIndexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index configured context artifacts and record content hashes",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := config.FindRoot()
		if err != nil {
			return err
		}
		result, err := contextartifacts.Index(root)
		if err != nil {
			return err
		}
		if !result.Configured {
			return printJSON(result, func() {
				fmt.Fprintln(cmd.OutOrStdout(), "No .gg/context-artifacts.yaml configured")
			})
		}
		return printJSON(result, func() {
			fmt.Fprintf(cmd.OutOrStdout(), "Indexed %d context artifacts\n", result.Indexed)
		})
	},
}

func init() {
	contextArtifactsCmd.AddCommand(contextArtifactsIndexCmd)
	contextCmd.AddCommand(contextArtifactsCmd)
}

func searchContextArtifacts(query string, limit uint64) ([]contextartifacts.Snippet, error) {
	root, err := config.FindRoot()
	if err != nil {
		return nil, err
	}
	n := int(limit)
	if n <= 0 {
		n = 5
	}
	return contextartifacts.Search(root, query, n)
}
