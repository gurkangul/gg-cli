package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

// decisionsCmd is the top-level `gg decisions [query]` alias (BUG-092c).
// Agents reach for `gg decisions` (75+ telemetry hits) but it used to be an
// "unknown command". This is a thin alias over the existing decisions read
// path: with a query it reuses the semantic search store call (the same
// store.SearchDecisions that `gg search` uses, filtered to decisions); with
// no query it lists the most recent active decisions. Rendering reuses the
// shared search renderer so output matches `gg search`.
var decisionsCmd = &cobra.Command{
	Use:   "decisions [query]",
	Short: "List or search decisions",
	Long: `Surface project decisions.

With a query, runs semantic search over decisions (the same path as
'gg search', filtered to the decisions kind). With no query, lists the most
recent active decisions.

This is the top-level shortcut for the decisions kind. For the decisions
linked to a specific task, use 'gg task decisions TASK-ID'.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDecisions,
}

var (
	decisionsLimit             uint64
	decisionsCompact           bool
	decisionsIncludeSuperseded bool
)

func init() {
	decisionsCmd.Flags().Uint64Var(&decisionsLimit, "limit", 10, "max decisions to return")
	decisionsCmd.Flags().BoolVar(&decisionsCompact, "compact", false, "one line per decision — drops reasons/tags/author to preserve agent context window")
	decisionsCmd.Flags().BoolVar(&decisionsIncludeSuperseded, "include-superseded", false, "include superseded/rejected decisions in results")
	rootCmd.AddCommand(decisionsCmd)
}

func runDecisions(cmd *cobra.Command, args []string) error {
	query := ""
	if len(args) == 1 {
		query = args[0]
	}

	// Reuse the search flag globals so the shared renderer (printSearchResults)
	// honours the same limit/compact semantics as `gg search`.
	searchLimit = decisionsLimit
	searchCompact = decisionsCompact
	searchIncludeSuperseded = decisionsIncludeSuperseded

	d, err := loadDepsReadOnly(query != "")
	if err != nil {
		return err
	}
	defer d.Close()

	if d.qdrantSlow {
		return fmt.Errorf("%s", withServiceHint("vector store health check timed out — retry or run gg doctor", svcVectorStore))
	}
	if d.qdrantDown {
		// Offline fallback reuses the search JSONL scan; an empty query scans
		// recent decisions, a non-empty query scores by text match.
		return serveSearchFromJSONL(cmd, query)
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	var decisions []store.Decision
	if query == "" {
		decisions, err = d.store.ListDecisions(ctx, int(decisionsLimit), decisionsIncludeSuperseded)
		if err != nil {
			if store.IsCollectionNotFoundError(err) {
				return serveSearchFromJSONL(cmd, query)
			}
			return fmt.Errorf("list decisions: %w", err)
		}
	} else {
		vector, genErr := d.embedder.Generate(ctx, query)
		if genErr != nil {
			return embedErr("generate embedding", genErr)
		}
		semanticLimit := decisionsLimit * 4
		if semanticLimit < 20 {
			semanticLimit = 20
		}
		decisions, err = d.store.SearchDecisions(ctx, vector, semanticLimit, decisionsIncludeSuperseded)
		if err != nil {
			if store.IsCollectionNotFoundError(err) {
				return serveSearchFromJSONL(cmd, query)
			}
			return fmt.Errorf("search decisions: %w", err)
		}
	}

	// Reuse the shared search renderer — decisions only (no rejections/tasks/etc.).
	return printSearchResults(cmd, query, decisions, nil, nil, nil, nil, "", time.Time{})
}
