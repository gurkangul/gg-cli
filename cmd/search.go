package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   `search "query"`,
	Short: "Semantic search across decisions and rejections",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

var searchLimit uint64

func init() {
	searchCmd.Flags().Uint64Var(&searchLimit, "limit", 5, "max results to return")
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	embedder, err := newEmbedder()
	if err != nil {
		return err
	}
	vector, err := embedder.Generate(query)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	client, err := newStoreClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx := context.Background()

	// Search decisions
	decisions, err := client.SearchDecisions(ctx, vector, searchLimit)
	if err != nil {
		return fmt.Errorf("search decisions: %w", err)
	}

	// Search rejections
	rejections, err := client.SearchRejections(ctx, vector, searchLimit)
	if err != nil {
		return fmt.Errorf("search rejections: %w", err)
	}

	if len(decisions) == 0 && len(rejections) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	if len(decisions) > 0 {
		fmt.Println("DECISIONS:")
		for _, d := range decisions {
			fmt.Printf("  • %s\n", d.Text)
			if d.Reason != "" {
				fmt.Printf("    Reason: %s\n", d.Reason)
			}
			if len(d.Tags) > 0 {
				fmt.Printf("    Tags: %s\n", strings.Join(d.Tags, ", "))
			}
			if d.TaskID != "" {
				fmt.Printf("    Task: %s\n", d.TaskID)
			}
		}
	}

	if len(rejections) > 0 {
		fmt.Println("REJECTIONS:")
		for _, r := range rejections {
			fmt.Printf("  ✗ %s\n", r.Approach)
			if r.Reason != "" {
				fmt.Printf("    Reason: %s\n", r.Reason)
			}
			if r.TaskID != "" {
				fmt.Printf("    Task: %s\n", r.TaskID)
			}
		}
	}

	return nil
}
