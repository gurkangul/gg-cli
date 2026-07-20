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

var (
	searchLimit             uint64
	searchCompact           bool
	searchIncludeLinked     bool
	searchIncludeSuperseded bool
)

func init() {
	searchCmd.Flags().Uint64Var(&searchLimit, "limit", 5, "max results to return")
	searchCmd.Flags().BoolVar(&searchCompact, "compact", false, "one line per item — drops reasons/tags/author to preserve agent context window")
	searchCmd.Flags().BoolVar(&searchIncludeLinked, "include-linked", false, "also search read-only linked projects from .gg/config.yaml")
	searchCmd.Flags().BoolVar(&searchIncludeSuperseded, "include-superseded", false, "include superseded/rejected decisions and fixed/wontfix bugs in results")
	rootCmd.AddCommand(searchCmd)
}

// searchPayload is the struct persisted to / read from the LKG cache.
type searchPayload struct {
	Decisions  []store.Decision  `json:"decisions"`
	Rejections []store.Rejection `json:"rejections"`
	Tasks      []store.Task      `json:"tasks"`
	Bugs       []store.Bug       `json:"bugs"`
	Notes      []store.Note      `json:"notes"`
	Messages   []store.Message   `json:"messages"`
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
		return fmt.Errorf("%s", withServiceHint("vector store health check timed out — retry or run gg doctor", svcVectorStore))
	}
	if d.qdrantDown {
		// AC-4: fall back to JSONL scan first, then LKG cache.
		return serveSearchFromJSONL(cmd, query)
	}

	// TASK-504: surface a one-line stderr notice when the code graph is stale or
	// empty so an agent searching for context knows the graph-backed signals
	// (impact, dependents) may be out of date. Best-effort and bounded — never
	// blocks or fails search; stderr keeps the stdout payload clean.
	emitSearchGraphNotice(cmd)

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// TASK-516: warn when part of the brain is missing from the semantic index
	// (outbox backlog / degraded placeholder vectors). Without this a thin result
	// set is indistinguishable from "nothing was ever recorded".
	emitSemanticCoverageNotice(ctx, cmd, d.store)

	vector, err := d.embedder.Generate(ctx, query)
	if err != nil {
		return embedErr("generate embedding", err)
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
	go func() {
		defer wg.Done()
		decisions, decErr = d.store.SearchDecisions(ctx, vector, semanticLimit, searchIncludeSuperseded)
	}()
	go func() { defer wg.Done(); rejections, rejErr = d.store.SearchRejections(ctx, vector, semanticLimit) }()
	go func() { defer wg.Done(); tasks, taskErr = d.store.SearchTasks(ctx, vector, semanticLimit, false) }()
	go func() {
		defer wg.Done()
		bugs, bugErr = d.store.SearchBugs(ctx, vector, semanticLimit, searchIncludeSuperseded)
	}()
	go func() { defer wg.Done(); notes, noteErr = d.store.SearchNotes(ctx, vector, semanticLimit) }()
	wg.Wait()
	// Embedded-brain resilience: with GG_VECTOR_BACKEND=sqlite the store is always
	// reachable, so a fresh user who has not run `gg reembed` yet has no collection
	// to query and the read returns a raw NotFound instead of triggering the
	// store-down branch above. Treat collection-not-found exactly like store-down —
	// fall back to the offline JSONL lexical scan and return results, never a raw
	// NotFound. The qdrant path is unaffected: when Qdrant is up its collections
	// already exist (gg init/reembed created them), and when it is down the
	// d.qdrantDown branch handled it before we reached here.
	for _, e := range []error{decErr, rejErr, taskErr, bugErr, noteErr} {
		if store.IsCollectionNotFoundError(e) {
			return serveSearchFromJSONL(cmd, query)
		}
	}
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

	// TASK-516: always-on lexical tier over brain records. Until now the JSONL
	// lexical scan only ran when the store was down or the collection missing,
	// so a record the vector tier could not see — never embedded (written while
	// the embedder was down), carrying a degraded zero-vector, or simply below
	// the semantic cutoff — produced a silent "No results found." Union the
	// lexical hits into the candidate set so a verbatim match can never be
	// silently invisible. Ranking stays vector-primary and the status filters
	// mirror the vector path (BUG-064 must not regress).
	// messages start empty: the store has no vector search for them, so the
	// lexical tier is their only path into a healthy-path result set.
	var messages []store.Message
	decisions, rejections, tasks, bugs, notes, messages = hybridCandidates(
		query, decisions, rejections, tasks, bugs, notes, messages,
		searchIncludeSuperseded, int(semanticLimit),
	)

	if searchIncludeLinked {
		cfg, cfgErr := config.Load()
		if cfgErr != nil {
			return cfgErr
		}
		// Carry messages here too, or --include-linked would silently drop the one
		// kind that has no vector path at all.
		results := labelSearchResults(
			buildSearchResultsWithBackendScoresAndMessages(query, decisions, rejections, tasks, bugs, notes, messages, "sqlite", nil),
			cfg.ProjectID)
		linkedResults, warnings := linkedSearchResults(ctx, cfg, query, vector, semanticLimit)
		results = trimSearchResults(rankSearchResults(append(results, linkedResults...)), searchLimit)
		return printSearchMatches(cmd, query, results, "", time.Time{}, warnings)
	}

	// Write results to the LKG cache for future offline use (best-effort).
	if cfg, cfgErr := config.Load(); cfgErr == nil {
		if rtDir, rtErr := cfg.RuntimeDir(); rtErr == nil {
			_ = cache.Put(rtDir, "search", query, searchPayload{
				Decisions: decisions, Rejections: rejections, Tasks: tasks, Bugs: bugs, Notes: notes, Messages: messages,
			})
		}
	}

	// TASK-505: lexical symbol tier. Surface exact/keyword code-symbol matches
	// (bm25-ranked FTS5 over the graph store) alongside the semantic results, so
	// an identifier like "ForwardCallFlow" — or a camelCase fragment — ranks by
	// keyword instead of losing to embedding noise. Best-effort: a missing graph
	// or empty index is silent and never blocks the semantic answer.
	symbols := lexicalSymbolMatches(ctx, query, int(searchLimit))

	printErr := printSearchResultsWithSymbols(cmd, query, decisions, rejections, tasks, bugs, notes, messages, symbols, "", time.Time{})

	// TASK-521: repair a bounded batch of degraded vectors now that the answer is
	// already on stdout, so a slow embedder can never delay the read. Coverage
	// converges across several reads instead of requiring a human to run reembed.
	healSemanticCoverageOnRead(cmd, d)

	return printErr
}

// emitSearchGraphNotice prints the one-line stale/empty code-graph notice to
// stderr (TASK-504). It is fully best-effort: any config/root failure or a
// fresh/not-applicable graph is silent, and the bounded status collection
// cannot stall search.
func emitSearchGraphNotice(cmd *cobra.Command) {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	root, err := config.FindRoot()
	if err != nil {
		return
	}
	status := codeGraphStatusWithTimeout(root, root+"/"+config.DirName, cfg)
	emitGraphStatusNotice(cmd.OutOrStderr(), status)
}

// serveSearchFromJSONL performs a text-scan of the brain JSONL files as a
// lightweight offline fallback when Qdrant is unreachable. Scans decisions,
// rejections, tasks, and bugs. Falls through to the LKG cache when no JSONL
// files are found (pre-JSONL brain).
func serveSearchFromJSONL(cmd *cobra.Command, query string) error {
	const banner = "⚠ vector index not built — read served from JSONL (run gg reembed); may miss cross-project context"

	ggDir := config.GGDirOrEmpty()
	if ggDir == "" {
		return serveSearchFromCache(cmd, query)
	}

	decEntries, decErr := brain.SearchByTextScored(ggDir, "decisions", query)
	rejEntries, rejErr := brain.SearchByTextScored(ggDir, "rejections", query)
	taskEntries, taskErr := brain.SearchByTextScored(ggDir, "tasks", query)
	bugEntries, bugErr := brain.SearchByTextScored(ggDir, "bugs", query)
	noteEntries, noteErr := brain.SearchByTextScored(ggDir, "notes", query)
	msgEntries, msgErr := brain.SearchByTextScored(ggDir, "messages", query)

	// All absent → fall through to LKG cache (pre-JSONL brain).
	if decErr != nil && rejErr != nil && taskErr != nil && bugErr != nil && noteErr != nil && msgErr != nil {
		return serveSearchFromCache(cmd, query)
	}

	scores := searchScoreOverrides{}
	var decisions []store.Decision
	for _, match := range decEntries {
		d := decisionFromJSONLEntry(match.Entry)
		decisions = append(decisions, d)
		scores.set("decision", d.ID, match.Score)
	}
	var rejections []store.Rejection
	for _, match := range rejEntries {
		r := rejectionFromJSONLEntry(match.Entry)
		rejections = append(rejections, r)
		scores.set("rejection", r.ID, match.Score)
	}

	var tasks []store.Task
	for _, match := range taskEntries {
		t := taskFromJSONLEntry(match.Entry)
		tasks = append(tasks, t)
		scores.set("task", t.ID, match.Score)
	}

	var bugs []store.Bug
	for _, match := range bugEntries {
		b := bugFromJSONLEntry(match.Entry)
		bugs = append(bugs, b)
		scores.set("bug", b.ID, match.Score)
	}

	var notes []store.Note
	for _, match := range noteEntries {
		n := noteFromJSONLEntry(match.Entry)
		notes = append(notes, n)
		scores.set("note", n.ID, match.Score)
	}

	var messages []store.Message
	for _, match := range msgEntries {
		m := messageFromJSONLEntry(match.Entry)
		messages = append(messages, m)
		scores.set("message", m.ID, match.Score)
	}

	return printSearchResultsWithBackendScoresAndMessages(cmd, query, decisions, rejections, tasks, bugs, notes, messages, banner, time.Time{}, "jsonl", scores)
}

// serveSearchFromCache looks up the last-known-good cache entry for query
// and prints stale results with an offline banner.
func serveSearchFromCache(cmd *cobra.Command, query string) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ vector store unavailable — no cached results available")
		return nil
	}
	rtDir, err := cfg.RuntimeDir()
	if err != nil {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ vector store unavailable — no cached results available")
		return nil
	}

	var payload searchPayload
	cachedAt, found, err := cache.Get(rtDir, "search", query, &payload)
	if err != nil || !found {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ vector store unavailable — no cached results available for this query")
		return nil
	}

	banner := fmt.Sprintf("⚠ vector store unavailable — cache may be stale; last update at %s", cachedAt.Local().Format("2006-01-02 15:04:05"))
	return printSearchResultsWithBackendScoresAndMessages(cmd, query, payload.Decisions, payload.Rejections, payload.Tasks, payload.Bugs, payload.Notes, payload.Messages, banner, cachedAt, "cache", nil)
}

func printSearchResults(cmd *cobra.Command, query string, decisions []store.Decision, rejections []store.Rejection, tasks []store.Task, bugs []store.Bug, notes []store.Note, banner string, cachedAt time.Time) error {
	return printSearchResultsWithBackend(cmd, query, decisions, rejections, tasks, bugs, notes, banner, cachedAt, "sqlite")
}

func printSearchResultsWithBackend(cmd *cobra.Command, query string, decisions []store.Decision, rejections []store.Rejection, tasks []store.Task, bugs []store.Bug, notes []store.Note, banner string, cachedAt time.Time, backend string) error {
	return printSearchResultsWithBackendAndScores(cmd, query, decisions, rejections, tasks, bugs, notes, banner, cachedAt, backend, nil)
}

func printSearchResultsWithBackendAndScores(cmd *cobra.Command, query string, decisions []store.Decision, rejections []store.Rejection, tasks []store.Task, bugs []store.Bug, notes []store.Note, banner string, cachedAt time.Time, backend string, scores searchScoreOverrides) error {
	return printSearchResultsWithBackendScoresAndMessages(cmd, query, decisions, rejections, tasks, bugs, notes, nil, banner, cachedAt, backend, scores)
}

func printSearchResultsWithBackendScoresAndMessages(cmd *cobra.Command, query string, decisions []store.Decision, rejections []store.Rejection, tasks []store.Task, bugs []store.Bug, notes []store.Note, messages []store.Message, banner string, cachedAt time.Time, backend string, scores searchScoreOverrides) error {
	results := trimSearchResults(buildSearchResultsWithBackendScoresAndMessages(query, decisions, rejections, tasks, bugs, notes, messages, backend, scores), searchLimit)
	jsonMap := map[string]any{
		"decisions":      decisions,
		"rejections":     rejections,
		"tasks":          tasks,
		"bugs":           bugs,
		"notes":          notes,
		"messages":       messages,
		"matches":        results,
		"ranking":        "semantic results with deterministic lexical exact-match boost; BM25/sparse fallback not required",
		"source_backend": backend,
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
			fmt.Fprintf(w, "  • %s%s\n", sourcePrefix(result.SourceProjectID), dec.Text)
			if dec.Status != "" && dec.Status != "active" {
				fmt.Fprintf(w, "    Status: %s\n", dec.Status)
			}
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
			fmt.Fprintf(w, "  ✗ %s%s\n", sourcePrefix(result.SourceProjectID), r.Approach)
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
			fmt.Fprintf(w, "  %s %s[%s] %s — %s\n", taskStatusIcon(t.Status), sourcePrefix(result.SourceProjectID), t.ID, t.Title, t.Priority)
			if t.Detail != "" {
				fmt.Fprintf(w, "    %s\n", compactTrim(t.Detail, 120))
			}
		case result.Bug != nil:
			b := *result.Bug
			fmt.Fprintf(w, "  ● %s[%s] %s — %s/%s\n", sourcePrefix(result.SourceProjectID), b.ID, b.Title, b.Severity, b.Status)
			if b.Detail != "" {
				fmt.Fprintf(w, "    %s\n", compactTrim(b.Detail, 120))
			}
		case result.Message != nil:
			m := *result.Message
			fmt.Fprintf(w, "  M %s→%s %s\n", m.FromRole, m.ToRole, m.Content)
			if m.TaskID != "" {
				fmt.Fprintf(w, "    Task: %s\n", m.TaskID)
			}
		case result.Note != nil:
			n := *result.Note
			fmt.Fprintf(w, "  %s[%s]", sourcePrefix(result.SourceProjectID), shortDate(n.CreatedAt))
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
	header := fmt.Sprintf("search — %dD %dR %dT %dB %dN", counts["decision"], counts["rejection"], counts["task"], counts["bug"], counts["note"])
	if counts["message"] > 0 {
		header += fmt.Sprintf(" %dM", counts["message"])
	}
	fmt.Fprintf(w, "%s\n\n", header)
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
