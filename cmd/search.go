package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
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
	Tasks      []store.Task      `json:"tasks"`
	Bugs       []store.Bug       `json:"bugs"`
	Notes      []store.Note      `json:"notes"`
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

	semanticLimit := searchLimit * 4
	if semanticLimit < 20 {
		semanticLimit = 20
	}
	var decisions []store.Decision
	var rejections []store.Rejection
	var tasks []store.Task
	var bugs []store.Bug
	var notes []store.Note
	var decErr, rejErr, taskErr, bugErr, noteErr error
	var wg sync.WaitGroup
	wg.Add(5)
	go func() { defer wg.Done(); decisions, decErr = d.store.SearchDecisions(ctx, vector, semanticLimit) }()
	go func() { defer wg.Done(); rejections, rejErr = d.store.SearchRejections(ctx, vector, semanticLimit) }()
	go func() { defer wg.Done(); tasks, taskErr = d.store.SearchTasks(ctx, vector, semanticLimit, false) }()
	go func() { defer wg.Done(); bugs, bugErr = d.store.SearchBugs(ctx, vector, semanticLimit) }()
	go func() { defer wg.Done(); notes, noteErr = d.store.SearchNotes(ctx, vector, semanticLimit) }()
	wg.Wait()
	if decErr != nil {
		return fmt.Errorf("search decisions: %w", decErr)
	}
	if rejErr != nil {
		return fmt.Errorf("search rejections: %w", rejErr)
	}
	if taskErr != nil {
		return fmt.Errorf("search tasks: %w", taskErr)
	}
	if bugErr != nil {
		return fmt.Errorf("search bugs: %w", bugErr)
	}
	if noteErr != nil {
		return fmt.Errorf("search notes: %w", noteErr)
	}
	tasks = prependExactTask(ctx, d.store, query, tasks)
	bugs = prependExactBug(ctx, d.store, query, bugs)

	// Write results to the LKG cache for future offline use (best-effort).
	if cfg, cfgErr := config.Load(); cfgErr == nil {
		if rtDir, rtErr := cfg.RuntimeDir(); rtErr == nil {
			_ = cache.Put(rtDir, "search", query, searchPayload{
				Decisions: decisions, Rejections: rejections, Tasks: tasks, Bugs: bugs, Notes: notes,
			})
		}
	}

	return printSearchResults(cmd, query, decisions, rejections, tasks, bugs, notes, "", time.Time{})
}

// serveSearchFromJSONL performs a text-scan of the brain JSONL files as a
// lightweight offline fallback when Qdrant is unreachable. Scans decisions,
// rejections, tasks, and bugs. Falls through to the LKG cache when no JSONL
// files are found (pre-JSONL brain).
func serveSearchFromJSONL(cmd *cobra.Command, query string) error {
	const banner = "⚠ Qdrant unreachable — read served from JSONL (may miss cross-project context)"

	ggDir := config.GGDirOrEmpty()
	if ggDir == "" {
		return serveSearchFromCache(cmd, query)
	}

	decEntries, decErr := brain.SearchByText(ggDir, "decisions", query)
	rejEntries, rejErr := brain.SearchByText(ggDir, "rejections", query)
	taskEntries, taskErr := brain.SearchByText(ggDir, "tasks", query)
	bugEntries, bugErr := brain.SearchByText(ggDir, "bugs", query)

	// All absent → fall through to LKG cache (pre-JSONL brain).
	if decErr != nil && rejErr != nil && taskErr != nil && bugErr != nil {
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

	// Include tasks and bugs as synthetic decisions so they surface in the
	// offline results. This is a best-effort representation — full task/bug
	// objects are not available without Qdrant.
	for _, e := range taskEntries {
		d := store.Decision{ID: e.UUID, Author: e.Author}
		if v, ok := e.Payload["title"].(string); ok {
			d.Text = "[task] " + v
		}
		if v, ok := e.Payload["detail"].(string); ok {
			d.Reason = v
		}
		if v, ok := e.Payload["task_id"].(string); ok {
			d.TaskID = v
		}
		if v, ok := e.Payload["status"].(string); ok {
			d.Status = v
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
	for _, e := range bugEntries {
		d := store.Decision{ID: e.UUID, Author: e.Author}
		if v, ok := e.Payload["title"].(string); ok {
			d.Text = "[bug] " + v
		}
		if v, ok := e.Payload["detail"].(string); ok {
			d.Reason = v
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

	return printSearchResults(cmd, query, decisions, rejections, nil, nil, nil, banner, time.Time{})
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
	return printSearchResults(cmd, query, payload.Decisions, payload.Rejections, payload.Tasks, payload.Bugs, payload.Notes, banner, cachedAt)
}

func printSearchResults(cmd *cobra.Command, query string, decisions []store.Decision, rejections []store.Rejection, tasks []store.Task, bugs []store.Bug, notes []store.Note, banner string, cachedAt time.Time) error {
	results := trimSearchResults(buildSearchResults(query, decisions, rejections, tasks, bugs, notes), searchLimit)
	jsonMap := map[string]any{
		"decisions":  decisions,
		"rejections": rejections,
		"tasks":      tasks,
		"bugs":       bugs,
		"notes":      notes,
		"matches":    results,
		"ranking":    "semantic results with deterministic lexical exact-match boost; BM25/sparse fallback not required",
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
				func(w io.Writer) { renderSearchResultsDefault(w, results) },
				func(w io.Writer) { renderSearchResultsCompact(w, results) },
				compactRendererV_search,
			)
			return
		}
		renderSearchResultsDefault(os.Stdout, results)
	})
}

func renderSearchResultsDefault(w io.Writer, results []searchResult) {
	if len(results) == 0 {
		fmt.Fprintln(w, "No results found.")
		return
	}
	fmt.Fprintln(w, "RESULTS:")
	for _, result := range results {
		switch {
		case result.Decision != nil:
			dec := *result.Decision
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
		case result.Rejection != nil:
			r := *result.Rejection
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
		case result.Task != nil:
			t := *result.Task
			fmt.Fprintf(w, "  %s [%s] %s — %s\n", taskStatusIcon(t.Status), t.ID, t.Title, t.Priority)
			if t.Detail != "" {
				fmt.Fprintf(w, "    %s\n", compactTrim(t.Detail, 120))
			}
		case result.Bug != nil:
			b := *result.Bug
			fmt.Fprintf(w, "  ● [%s] %s — %s/%s\n", b.ID, b.Title, b.Severity, b.Status)
			if b.Detail != "" {
				fmt.Fprintf(w, "    %s\n", compactTrim(b.Detail, 120))
			}
		case result.Note != nil:
			n := *result.Note
			fmt.Fprintf(w, "  [%s]", shortDate(n.CreatedAt))
			if n.TaskID != "" {
				fmt.Fprintf(w, " (%s)", n.TaskID)
			}
			fmt.Fprintf(w, " %s\n", n.Text)
		}
	}
}

func renderSearchCompact(w io.Writer, decisions []store.Decision, rejections []store.Rejection) {
	renderSearchResultsCompact(w, buildSearchResults("", decisions, rejections, nil, nil, nil))
}

func renderSearchResultsCompact(w io.Writer, results []searchResult) {
	counts := map[string]int{}
	for _, r := range results {
		counts[r.Kind]++
	}
	fmt.Fprintf(w, "search — %dD %dR %dT %dB %dN\n\n", counts["decision"], counts["rejection"], counts["task"], counts["bug"], counts["note"])
	if len(results) == 0 {
		fmt.Fprintln(w, "(no results)")
		return
	}
	for _, r := range results {
		fmt.Fprintln(w, compactSearchResultLine(r))
	}
}

func prependExactTask(ctx context.Context, c *store.Client, query string, tasks []store.Task) []store.Task {
	id := exactID(query, "TASK")
	if id == "" {
		return tasks
	}
	t, err := c.GetTask(ctx, id)
	if err != nil || t == nil {
		return tasks
	}
	out := []store.Task{*t}
	for _, task := range tasks {
		if task.ID != id {
			out = append(out, task)
		}
	}
	return out
}

func prependExactBug(ctx context.Context, c *store.Client, query string, bugs []store.Bug) []store.Bug {
	id := exactID(query, "BUG")
	if id == "" {
		return bugs
	}
	b, err := c.GetBug(ctx, id)
	if err != nil || b == nil {
		return bugs
	}
	out := []store.Bug{*b}
	for _, bug := range bugs {
		if bug.ID != id {
			out = append(out, bug)
		}
	}
	return out
}

func exactID(query, prefix string) string {
	for _, match := range exactSearchID.FindAllString(strings.ToUpper(query), -1) {
		if strings.HasPrefix(match, prefix+"-") {
			return match
		}
	}
	return ""
}
