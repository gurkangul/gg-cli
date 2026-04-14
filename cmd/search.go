package cmd

import (
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
	query, err := requireNonEmpty("query", args[0])
	if err != nil {
		return err
	}

	d, err := loadDepsReadOnly(true)
	if err != nil {
		return err
	}
	defer d.Close()

	if d.qdrantDown {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ Qdrant unreachable — read-only fallback mode (no vector search available)")
		return nil
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	vector, err := d.embedder.Generate(ctx, query)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	decisions, err := d.store.SearchDecisions(ctx, vector, searchLimit)
	if err != nil {
		return fmt.Errorf("search decisions: %w", err)
	}

	rejections, err := d.store.SearchRejections(ctx, vector, searchLimit)
	if err != nil {
		return fmt.Errorf("search rejections: %w", err)
	}

	return printJSON(map[string]any{
		"decisions":  decisions,
		"rejections": rejections,
	}, func() {
		if len(decisions) == 0 && len(rejections) == 0 {
			fmt.Println("No results found.")
			return
		}
		if len(decisions) > 0 {
			fmt.Println("DECISIONS:")
			for _, dec := range decisions {
				fmt.Printf("  • %s\n", dec.Text)
				if dec.Reason != "" {
					fmt.Printf("    Reason: %s\n", dec.Reason)
				}
				if len(dec.Tags) > 0 {
					fmt.Printf("    Tags: %s\n", strings.Join(dec.Tags, ", "))
				}
				if dec.TaskID != "" {
					fmt.Printf("    Task: %s\n", dec.TaskID)
				}
				if dec.Author != "" {
					fmt.Printf("    By: %s\n", dec.Author)
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
				if len(r.Tags) > 0 {
					fmt.Printf("    Tags: %s\n", strings.Join(r.Tags, ", "))
				}
				if r.TaskID != "" {
					fmt.Printf("    Task: %s\n", r.TaskID)
				}
				if r.Author != "" {
					fmt.Printf("    By: %s\n", r.Author)
				}
			}
		}
	})
}
