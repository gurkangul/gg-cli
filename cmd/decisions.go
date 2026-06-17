package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
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
		// Offline fallback scans the decisions JSONL only — same kind as the
		// online path (no rejections/tasks/bugs/notes leak). An empty query
		// returns recent decisions, a non-empty query scores by text match.
		return serveDecisionsFromJSONL(cmd, query)
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	var decisions []store.Decision
	if query == "" {
		decisions, err = d.store.ListDecisions(ctx, int(decisionsLimit), decisionsIncludeSuperseded)
		if err != nil {
			if store.IsCollectionNotFoundError(err) {
				return serveDecisionsFromJSONL(cmd, query)
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
				return serveDecisionsFromJSONL(cmd, query)
			}
			return fmt.Errorf("search decisions: %w", err)
		}
	}

	// Reuse the shared search renderer — decisions only (no rejections/tasks/etc.).
	return printSearchResults(cmd, query, decisions, nil, nil, nil, nil, "", time.Time{})
}

// serveDecisionsFromJSONL is the offline fallback for `gg decisions`. Unlike the
// generic serveSearchFromJSONL (used by `gg search`), it scans ONLY the decisions
// JSONL so the offline output matches the online ListDecisions/SearchDecisions
// path — no rejections/tasks/bugs/notes/messages leak (BUG-092c part 2). An empty
// query returns recent decisions; a non-empty query scores by text match. The
// renderer is the same one the online decisions output uses, with every non-decision
// slice nil.
func serveDecisionsFromJSONL(cmd *cobra.Command, query string) error {
	const banner = "⚠ vector index not built — decisions served from JSONL (run gg reembed); may miss cross-project context"

	ggDir := config.GGDirOrEmpty()
	if ggDir == "" {
		// No brain dir to scan — fall back to the shared LKG cache reader.
		return serveSearchFromCache(cmd, query)
	}

	matches, err := brain.SearchByTextScored(ggDir, "decisions", query)
	if err != nil {
		// No decisions JSONL (pre-JSONL brain) — fall back to the LKG cache.
		return serveSearchFromCache(cmd, query)
	}

	scores := searchScoreOverrides{}
	var decisions []store.Decision
	for _, match := range matches {
		dec := decisionFromJSONLEntry(match.Entry)
		// Mirror the online suppression: hide superseded/rejected decisions
		// unless --include-superseded was passed.
		if !decisionsIncludeSuperseded && dec.Status != "" && dec.Status != "active" {
			continue
		}
		decisions = append(decisions, dec)
		scores.set("decision", dec.ID, match.Score)
	}

	return printSearchResultsWithBackendScoresAndMessages(cmd, query, decisions, nil, nil, nil, nil, nil, banner, time.Time{}, "jsonl", scores)
}
