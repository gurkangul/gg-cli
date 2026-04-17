package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/cache"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

var contextCmd = &cobra.Command{
	Use:   `context "topic"`,
	Short: "Fetch a unified context bundle for a topic",
	Long: `Searches decisions, rejections, tasks, and discussions for the given topic
using semantic similarity and returns a bundled context package for agent consumption.

Phase 2 will add Memgraph structural queries (affected files/symbols).`,
	Args: cobra.ExactArgs(1),
	RunE: runContext,
}

var contextLimit uint64
var contextIncludeResolved bool
var contextFullTranscript bool
var contextCompact bool

func init() {
	contextCmd.Flags().Uint64Var(&contextLimit, "limit", 5, "max results per collection")
	contextCmd.Flags().BoolVar(&contextIncludeResolved, "include-resolved", false, "include resolved/dismissed discussions and done/blocked tasks")
	contextCmd.Flags().BoolVar(&contextFullTranscript, "full", false, "print full deliberation transcript for each discussion")
	contextCmd.Flags().BoolVar(&contextCompact, "compact", false, "emit one line per item — drops reasons/details/transcripts to preserve agent context window")
	rootCmd.AddCommand(contextCmd)
}

type contextBundle struct {
	decisions   []store.Decision
	rejections  []store.Rejection
	tasks       []store.Task
	discussions []store.Discussion
	notes       []store.Note
	decErr      error
	rejErr      error
	taskErr     error
	discErr     error
	noteErr     error
}

// contextPayload is the cacheable form of a context bundle (exported fields only).
type contextPayload struct {
	Decisions   []store.Decision   `json:"decisions"`
	Rejections  []store.Rejection  `json:"rejections"`
	Tasks       []store.Task       `json:"tasks"`
	Discussions []store.Discussion `json:"discussions"`
	Notes       []store.Note       `json:"notes"`
}

const contextCacheKind = "context"

func runContext(cmd *cobra.Command, args []string) error {
	query, err := requireNonEmpty("topic", args[0])
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
		return serveContextFromCache(cmd, query)
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	vector, err := d.embedder.Generate(ctx, query)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	// Run all searches in parallel.
	var bundle contextBundle
	var wg sync.WaitGroup
	wg.Add(5)

	go func() {
		defer wg.Done()
		bundle.decisions, bundle.decErr = d.store.SearchDecisions(ctx, vector, contextLimit)
	}()
	go func() {
		defer wg.Done()
		bundle.rejections, bundle.rejErr = d.store.SearchRejections(ctx, vector, contextLimit)
	}()
	go func() {
		defer wg.Done()
		bundle.tasks, bundle.taskErr = d.store.SearchTasks(ctx, vector, contextLimit, contextIncludeResolved)
	}()
	go func() {
		defer wg.Done()
		bundle.discussions, bundle.discErr = d.store.SearchDiscussions(ctx, vector, contextLimit, contextIncludeResolved)
	}()
	go func() {
		defer wg.Done()
		bundle.notes, bundle.noteErr = d.store.SearchNotes(ctx, vector, contextLimit)
	}()

	wg.Wait()

	// Persist a full successful bundle to the LKG cache (best-effort).
	if bundle.decErr == nil && bundle.rejErr == nil && bundle.taskErr == nil &&
		bundle.discErr == nil && bundle.noteErr == nil {
		if cfg, cfgErr := config.Load(); cfgErr == nil {
			if rtDir, rtErr := cfg.RuntimeDir(); rtErr == nil {
				_ = cache.Put(rtDir, contextCacheKind, query, contextPayload{
					Decisions:   bundle.decisions,
					Rejections:  bundle.rejections,
					Tasks:       bundle.tasks,
					Discussions: bundle.discussions,
					Notes:       bundle.notes,
				})
			}
		}
	}

	// Collect any errors as warnings — partial results are still useful.
	var errs []string
	if bundle.decErr != nil {
		errs = append(errs, fmt.Sprintf("decisions: %v", bundle.decErr))
	}
	if bundle.rejErr != nil {
		errs = append(errs, fmt.Sprintf("rejections: %v", bundle.rejErr))
	}
	if bundle.taskErr != nil {
		errs = append(errs, fmt.Sprintf("tasks: %v", bundle.taskErr))
	}
	if bundle.discErr != nil {
		errs = append(errs, fmt.Sprintf("discussions: %v", bundle.discErr))
	}
	if bundle.noteErr != nil {
		errs = append(errs, fmt.Sprintf("notes: %v", bundle.noteErr))
	}

	total := len(bundle.decisions) + len(bundle.rejections) + len(bundle.tasks) + len(bundle.discussions) + len(bundle.notes)
	if total == 0 && len(errs) > 0 {
		return fmt.Errorf("all searches failed:\n  %s", strings.Join(errs, "\n  "))
	}

	return printContextBundle(cmd, query, bundle, errs, time.Time{})
}

// printContextBundle renders a context bundle as human-readable text or JSON.
// errs is a list of non-fatal collection errors to show as warnings.
// cachedAt, when non-zero, indicates the results are from the LKG cache and
// adds "cached_at" and "stale_seconds" to the JSON output so agents know the
// data may be stale.
func printContextBundle(cmd *cobra.Command, query string, bundle contextBundle, errs []string, cachedAt time.Time) error {
	jsonPayload := map[string]any{
		"query":       query,
		"decisions":   bundle.decisions,
		"rejections":  bundle.rejections,
		"tasks":       bundle.tasks,
		"discussions": bundle.discussions,
		"notes":       bundle.notes,
		"warnings":    errs,
	}
	if !cachedAt.IsZero() {
		jsonPayload["cached_at"] = cachedAt.UTC().Format(time.RFC3339)
		jsonPayload["stale_seconds"] = int(time.Since(cachedAt).Seconds())
	}

	return printJSON(jsonPayload, func() {
		if contextCompact {
			emitCompact(cmd, "context",
				func(w io.Writer) { renderContextDefault(w, query, bundle, errs) },
				func(w io.Writer) { renderContextCompact(w, query, bundle, errs) },
			)
			return
		}
		renderContextDefault(os.Stdout, query, bundle, errs)
	})
}

func renderContextDefault(w io.Writer, query string, bundle contextBundle, errs []string) {
	fmt.Fprintf(w, "CONTEXT BUNDLE: %q\n", query)
	fmt.Fprintln(w, strings.Repeat("─", 60))

	if len(bundle.decisions) > 0 {
		fmt.Fprintln(w, "\nDECISIONS:")
		for _, dec := range bundle.decisions {
			fmt.Fprintf(w, "  • [%s] %s\n", shortDate(dec.CreatedAt), dec.Text)
			if dec.Reason != "" {
				fmt.Fprintf(w, "    Reason: %s\n", dec.Reason)
			}
			if len(dec.Tags) > 0 {
				fmt.Fprintf(w, "    Tags: %s\n", strings.Join(dec.Tags, ", "))
			}
			if dec.TaskID != "" {
				fmt.Fprintf(w, "    Task: %s\n", dec.TaskID)
			}
		}
	}

	if len(bundle.rejections) > 0 {
		fmt.Fprintln(w, "\nREJECTIONS:")
		for _, r := range bundle.rejections {
			fmt.Fprintf(w, "  ✗ [%s] %s\n", shortDate(r.CreatedAt), r.Approach)
			if r.Reason != "" {
				fmt.Fprintf(w, "    Reason: %s\n", r.Reason)
			}
			if len(r.Tags) > 0 {
				fmt.Fprintf(w, "    Tags: %s\n", strings.Join(r.Tags, ", "))
			}
			if r.TaskID != "" {
				fmt.Fprintf(w, "    Task: %s\n", r.TaskID)
			}
		}
	}

	if len(bundle.tasks) > 0 {
		fmt.Fprintln(w, "\nTASKS:")
		for _, t := range bundle.tasks {
			fmt.Fprintf(w, "  %s [%s] %s — %s\n", taskStatusIcon(t.Status), t.ID, t.Title, t.Priority)
			if t.Detail != "" {
				fmt.Fprintf(w, "    %s\n", compactTrim(t.Detail, 120))
			}
		}
	}

	if len(bundle.discussions) > 0 {
		fmt.Fprintln(w, "\nDISCUSSIONS:")
		for _, disc := range bundle.discussions {
			fmt.Fprintf(w, "  %s [%s] %s\n", discStatusMark(disc.Status), disc.ID, disc.Topic)
			if disc.Detail != "" {
				fmt.Fprintf(w, "    %s\n", compactTrim(disc.Detail, 120))
			}
			if disc.Status == "resolved" && disc.ResolvedNote != "" {
				fmt.Fprintf(w, "    Resolved: %s\n", disc.ResolvedNote)
			}
			if contextFullTranscript && len(disc.Turns) > 0 {
				fmt.Fprintf(w, "    Transcript (%d turns):\n", len(disc.Turns))
				for i, t := range disc.Turns {
					fmt.Fprintf(w, "      [%d] %s (%s): %s\n", i+1, t.By, t.Role, t.Text)
				}
			} else if len(disc.Turns) > 0 {
				last := disc.Turns[len(disc.Turns)-1]
				fmt.Fprintf(w, "    Latest: %s (%s) — %s\n", last.By, last.Role, last.Text)
				if len(disc.Turns) > 1 {
					fmt.Fprintf(w, "    (%d more turns — use --full to show all)\n", len(disc.Turns)-1)
				}
			}
		}
	}

	if len(bundle.notes) > 0 {
		fmt.Fprintln(w, "\nNOTES:")
		for _, n := range bundle.notes {
			fmt.Fprintf(w, "  [%s]", shortDate(n.CreatedAt))
			if n.TaskID != "" {
				fmt.Fprintf(w, " (%s)", n.TaskID)
			}
			fmt.Fprintf(w, " %s\n", n.Text)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %d decisions  %d rejections  %d tasks  %d discussions  %d notes\n",
		len(bundle.decisions), len(bundle.rejections), len(bundle.tasks), len(bundle.discussions), len(bundle.notes))

	if len(errs) > 0 {
		fmt.Fprintf(w, "\nWarnings:\n  %s\n", strings.Join(errs, "\n  "))
	}
}

// serveContextFromCache looks up the last-known-good cache entry for query
// and prints stale results with an offline banner.
func serveContextFromCache(cmd *cobra.Command, query string) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ Qdrant unreachable — no cached context available")
		return nil
	}
	rtDir, err := cfg.RuntimeDir()
	if err != nil {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ Qdrant unreachable — no cached context available")
		return nil
	}

	var payload contextPayload
	cachedAt, found, err := cache.Get(rtDir, contextCacheKind, query, &payload)
	if err != nil || !found {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ Qdrant unreachable — no cached context available for this topic")
		return nil
	}

	banner := fmt.Sprintf("⚠ Qdrant unreachable — cache may be stale; last update at %s", cachedAt.Local().Format("2006-01-02 15:04:05"))
	fmt.Fprintln(cmd.OutOrStderr(), banner)

	bundle := contextBundle{
		decisions:   payload.Decisions,
		rejections:  payload.Rejections,
		tasks:       payload.Tasks,
		discussions: payload.Discussions,
		notes:       payload.Notes,
	}
	// Pass banner as a warning and cachedAt so --json consumers see the stale signal.
	return printContextBundle(cmd, query, bundle, []string{banner}, cachedAt)
}

// shortDate returns the first 10 characters of an RFC3339 timestamp (YYYY-MM-DD).
// Returns "—" for empty or short strings to avoid panics.
func shortDate(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return "—"
}

func taskStatusIcon(status string) string {
	switch status {
	case "done":
		return "✓"
	case "in_progress":
		return "→"
	case "blocked":
		return "!"
	default:
		return "○"
	}
}

func discStatusMark(status string) string {
	switch status {
	case "resolved":
		return "✓"
	case "dismissed":
		return "–"
	default:
		return "?"
	}
}

// compactLineWidth caps per-item text in compact output so a bundle of
// long-titled items still fits in a standard terminal.
const compactLineWidth = 80

// renderContextCompact drops Reason/Detail/Tags/transcript bodies so an
// agent can scan a bundle and decide what to fetch in full. Typical 60-85%
// size reduction vs default rendering.
func renderContextCompact(w io.Writer, query string, bundle contextBundle, errs []string) {
	fmt.Fprintf(w, "context: %q — %dD %dR %dT %d? %dN\n\n",
		query,
		len(bundle.decisions), len(bundle.rejections),
		len(bundle.tasks), len(bundle.discussions), len(bundle.notes))

	for _, dec := range bundle.decisions {
		suffix := ""
		if dec.TaskID != "" {
			suffix = " →" + dec.TaskID
		}
		fmt.Fprintf(w, "D  %s  %s%s\n",
			shortDate(dec.CreatedAt), compactTrim(dec.Text, compactLineWidth), suffix)
	}
	for _, r := range bundle.rejections {
		suffix := ""
		if r.TaskID != "" {
			suffix = " →" + r.TaskID
		}
		fmt.Fprintf(w, "R  %s  %s%s\n",
			shortDate(r.CreatedAt), compactTrim(r.Approach, compactLineWidth), suffix)
	}
	for _, t := range bundle.tasks {
		fmt.Fprintf(w, "T %s %s  %s (%s)\n",
			taskStatusIcon(t.Status), t.ID, compactTrim(t.Title, compactLineWidth), t.Priority)
	}
	for _, disc := range bundle.discussions {
		suffix := ""
		if n := len(disc.Turns); n > 0 {
			suffix = fmt.Sprintf(" (%d turns)", n)
		}
		fmt.Fprintf(w, "? %s %s  %s%s\n",
			discStatusMark(disc.Status), disc.ID, compactTrim(disc.Topic, compactLineWidth), suffix)
	}
	for _, n := range bundle.notes {
		taskRef := ""
		if n.TaskID != "" {
			taskRef = "  (" + n.TaskID + ")"
		}
		fmt.Fprintf(w, "N  %s%s  %s\n",
			shortDate(n.CreatedAt), taskRef, compactTrim(n.Text, compactLineWidth))
	}

	if len(errs) > 0 {
		fmt.Fprintf(w, "\n! %s\n", strings.Join(errs, "; "))
	}
}

func compactTrim(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
