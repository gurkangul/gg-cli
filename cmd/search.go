package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/cache"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
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

// searchPayload is the struct persisted to / read from the LKG cache.
type searchPayload struct {
	Decisions  []store.Decision  `json:"decisions"`
	Rejections []store.Rejection `json:"rejections"`
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
		return serveSearchFromCache(cmd, query)
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

	// Write results to the LKG cache for future offline use (best-effort).
	if ggDir, dirErr := config.GGDir(); dirErr == nil {
		_ = cache.Put(ggDir, query, searchPayload{Decisions: decisions, Rejections: rejections})
	}

	return printSearchResults(cmd, decisions, rejections, "")
}

// serveSearchFromCache looks up the last-known-good cache entry for query
// and prints stale results with an offline banner.
func serveSearchFromCache(cmd *cobra.Command, query string) error {
	ggDir, err := config.GGDir()
	if err != nil {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ Qdrant unreachable — no cached results available")
		return nil
	}

	var payload searchPayload
	cachedAt, found, err := cache.Get(ggDir, query, &payload)
	if err != nil || !found {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ Qdrant unreachable — no cached results available for this query")
		return nil
	}

	banner := fmt.Sprintf("⚠ Qdrant unreachable — showing cached results from %s", cachedAt.Local().Format("2006-01-02 15:04:05"))
	return printSearchResults(cmd, payload.Decisions, payload.Rejections, banner)
}

func printSearchResults(cmd *cobra.Command, decisions []store.Decision, rejections []store.Rejection, banner string) error {
	return printJSON(map[string]any{
		"decisions":  decisions,
		"rejections": rejections,
	}, func() {
		if banner != "" {
			fmt.Fprintln(cmd.OutOrStderr(), banner)
		}
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
