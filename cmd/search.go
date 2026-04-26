package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/cache"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

var searchCmd = &cobra.Command{
	Use:   `search "query"`,
	Short: "Find relevant context — semantic search across decisions, tasks, and messages",
	Long: `Retrieve the most relevant brain records for a query using vector similarity.

WHEN TO USE: before starting work — ask 'has this been decided before?' or 'what context
exists around this area?'. Use --compact when passing results into an agent context window.

WHEN NOT TO USE: for exact-match lookups (task IDs, tag filters) use 'gg task list'.

See also: gg status (project overview), gg task get (task details)`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

var searchLimit uint64
var searchCompact bool

func init() {
	searchCmd.Flags().Uint64Var(&searchLimit, "limit", 5, "max results to return")
	searchCmd.Flags().BoolVar(&searchCompact, "compact", false, "one line per item — drops reasons/tags/author to preserve agent context window")
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

	if d.qdrantSlow {
		return fmt.Errorf("qdrant health check timed out — Qdrant may be overloaded; retry or check qdrant status")
	}
	if d.qdrantDown {
		// AC-4: fall back to JSONL scan first, then LKG cache.
		return serveSearchFromJSONL(cmd, query)
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
	if cfg, cfgErr := config.Load(); cfgErr == nil {
		if rtDir, rtErr := cfg.RuntimeDir(); rtErr == nil {
			_ = cache.Put(rtDir, "search", query, searchPayload{Decisions: decisions, Rejections: rejections})
		}
	}

	return printSearchResults(cmd, decisions, rejections, "", time.Time{})
}

// serveSearchFromJSONL performs a text-scan of .gg/brain/decisions.jsonl and
// .gg/brain/rejections.jsonl as a lightweight offline fallback when Qdrant is
// unreachable.  It falls through to the LKG cache when the JSONL files are
// absent (i.e., the brain has not been written yet in JSONL format).
func serveSearchFromJSONL(cmd *cobra.Command, query string) error {
	const banner = "⚠ Qdrant unreachable — read served from JSONL (may miss cross-project context)"

	ggDir := config.GGDirOrEmpty()
	if ggDir == "" {
		return serveSearchFromCache(cmd, query)
	}

	decEntries, decErr := brain.SearchByText(ggDir, "decisions", query)
	rejEntries, rejErr := brain.SearchByText(ggDir, "rejections", query)

	// Both absent → fall through to LKG cache (pre-JSONL brain).
	if decErr != nil && rejErr != nil {
		return serveSearchFromCache(cmd, query)
	}

	var decisions []store.Decision
	for _, e := range decEntries {
		d := store.Decision{
			ID:     e.UUID,
			Author: e.Author,
		}
		if v, ok := e.Payload["text"].(string); ok {
			d.Text = v
		}
		if v, ok := e.Payload["reason"].(string); ok {
			d.Reason = v
		}
		if v, ok := e.Payload["status"].(string); ok {
			d.Status = v
		}
		if v, ok := e.Payload["task_id"].(string); ok {
			d.TaskID = v
		}
		if v, ok := e.Payload["created_at"].(string); ok {
			d.CreatedAt = v
		}
		if tags, ok := e.Payload["tags"].([]any); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					d.Tags = append(d.Tags, s)
				}
			}
		}
		decisions = append(decisions, d)
	}
	var rejections []store.Rejection
	for _, e := range rejEntries {
		r := store.Rejection{
			ID:     e.UUID,
			Author: e.Author,
		}
		if v, ok := e.Payload["approach"].(string); ok {
			r.Approach = v
		}
		if v, ok := e.Payload["reason"].(string); ok {
			r.Reason = v
		}
		if v, ok := e.Payload["task_id"].(string); ok {
			r.TaskID = v
		}
		if v, ok := e.Payload["created_at"].(string); ok {
			r.CreatedAt = v
		}
		if tags, ok := e.Payload["tags"].([]any); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					r.Tags = append(r.Tags, s)
				}
			}
		}
		rejections = append(rejections, r)
	}

	return printSearchResults(cmd, decisions, rejections, banner, time.Time{})
}

// serveSearchFromCache looks up the last-known-good cache entry for query
// and prints stale results with an offline banner.
func serveSearchFromCache(cmd *cobra.Command, query string) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ Qdrant unreachable — no cached results available")
		return nil
	}
	rtDir, err := cfg.RuntimeDir()
	if err != nil {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ Qdrant unreachable — no cached results available")
		return nil
	}

	var payload searchPayload
	cachedAt, found, err := cache.Get(rtDir, "search", query, &payload)
	if err != nil || !found {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ Qdrant unreachable — no cached results available for this query")
		return nil
	}

	banner := fmt.Sprintf("⚠ Qdrant unreachable — cache may be stale; last update at %s", cachedAt.Local().Format("2006-01-02 15:04:05"))
	return printSearchResults(cmd, payload.Decisions, payload.Rejections, banner, cachedAt)
}

func printSearchResults(cmd *cobra.Command, decisions []store.Decision, rejections []store.Rejection, banner string, cachedAt time.Time) error {
	jsonMap := map[string]any{
		"decisions":  decisions,
		"rejections": rejections,
	}
	if banner != "" {
		jsonMap["warning"] = banner // include stale signal for agents parsing --json output
		if !cachedAt.IsZero() {
			jsonMap["cached_at"] = cachedAt.UTC().Format(time.RFC3339)
			jsonMap["stale_seconds"] = int(time.Since(cachedAt).Seconds())
		}
	}
	return printJSON(jsonMap, func() {
		if banner != "" {
			fmt.Fprintln(cmd.OutOrStderr(), banner)
		}
		if isCompactActive(cmd) {
			emitCompact(cmd, "search",
				func(w io.Writer) { renderSearchDefault(w, decisions, rejections) },
				func(w io.Writer) { renderSearchCompact(w, decisions, rejections) },
				compactRendererV_search,
			)
			return
		}
		renderSearchDefault(os.Stdout, decisions, rejections)
	})
}

func renderSearchDefault(w io.Writer, decisions []store.Decision, rejections []store.Rejection) {
	if len(decisions) == 0 && len(rejections) == 0 {
		fmt.Fprintln(w, "No results found.")
		return
	}
	if len(decisions) > 0 {
		fmt.Fprintln(w, "DECISIONS:")
		for _, dec := range decisions {
			fmt.Fprintf(w, "  • %s\n", dec.Text)
			if dec.Reason != "" {
				fmt.Fprintf(w, "    Reason: %s\n", dec.Reason)
			}
			if len(dec.Tags) > 0 {
				fmt.Fprintf(w, "    Tags: %s\n", strings.Join(dec.Tags, ", "))
			}
			if dec.TaskID != "" {
				fmt.Fprintf(w, "    Task: %s\n", dec.TaskID)
			}
			if dec.Author != "" {
				fmt.Fprintf(w, "    By: %s\n", dec.Author)
			}
		}
	}
	if len(rejections) > 0 {
		fmt.Fprintln(w, "REJECTIONS:")
		for _, r := range rejections {
			fmt.Fprintf(w, "  ✗ %s\n", r.Approach)
			if r.Reason != "" {
				fmt.Fprintf(w, "    Reason: %s\n", r.Reason)
			}
			if len(r.Tags) > 0 {
				fmt.Fprintf(w, "    Tags: %s\n", strings.Join(r.Tags, ", "))
			}
			if r.TaskID != "" {
				fmt.Fprintf(w, "    Task: %s\n", r.TaskID)
			}
			if r.Author != "" {
				fmt.Fprintf(w, "    By: %s\n", r.Author)
			}
		}
	}
}

func renderSearchCompact(w io.Writer, decisions []store.Decision, rejections []store.Rejection) {
	fmt.Fprintf(w, "search — %dD %dR\n\n", len(decisions), len(rejections))
	if len(decisions) == 0 && len(rejections) == 0 {
		fmt.Fprintln(w, "(no results)")
		return
	}
	writeCompactDecisions(w, decisions)
	writeCompactRejections(w, rejections)
}
