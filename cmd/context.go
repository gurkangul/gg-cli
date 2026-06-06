package cmd

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/cache"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

var contextCmd = &cobra.Command{
	Use:   `context [topic]`,
	Short: "Fetch a unified context bundle for the project, a topic, or a task",
	Long: `Without a topic, prints a compact project onboarding bundle: recent decisions,
rejections, active tasks, open discussions, notes, and context artifacts.

With a topic, searches decisions, rejections, tasks, and discussions using
semantic similarity and returns a bundled context package for agent consumption.

Use --for-task TASK-NNN to get a task-scoped context bundle: the task itself,
its dependencies, and semantically related decisions/rejections, useful for
resuming work after a conversation compaction.`,
	Args: cobra.RangeArgs(0, 1),
	RunE: runContext,
}

var contextLimit uint64
var contextIncludeResolved bool
var contextFullTranscript bool
var contextCompact bool
var contextBudget int
var contextForTask string
var contextIncludeLinked bool

func init() {
	contextCmd.Flags().Uint64Var(&contextLimit, "limit", 5, "max results per collection")
	contextCmd.Flags().BoolVar(&contextIncludeResolved, "include-resolved", false, "include resolved/dismissed discussions and done/blocked tasks")
	contextCmd.Flags().BoolVar(&contextFullTranscript, "full", false, "print full deliberation transcript for each discussion")
	contextCmd.Flags().BoolVar(&contextCompact, "compact", false, "emit one line per item — drops reasons/details/transcripts to preserve agent context window")
	contextCmd.Flags().IntVar(&contextBudget, "budget", 0, "token budget: emit P1 items first, drop lower-priority tiers when over budget (0 = no limit; implies --compact)")
	contextCmd.Flags().StringVar(&contextForTask, "for-task", "", "task-scoped rehydration: fetch the task, its dependencies, and related decisions/rejections")
	contextCmd.Flags().BoolVar(&contextIncludeLinked, "include-linked", false, "also search read-only linked projects from .gg/config.yaml")
	rootCmd.AddCommand(contextCmd)
}

const contextCacheKind = "context"

func runContext(cmd *cobra.Command, args []string) error {
	if contextForTask != "" {
		return runContextForTask(cmd, contextForTask)
	}
	if len(args) == 0 {
		return runProjectContext(cmd)
	}
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
		// BUG-075: prefer a live JSONL scan over the up-to-7-day LKG cache,
		// matching `gg search`'s offline behaviour. Falls back to cache when
		// JSONL has nothing for the query.
		return serveContextFromJSONL(cmd, query)
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
	wg.Add(6)

	go func() {
		defer wg.Done()
		bundle.decisions, bundle.decErr = d.store.SearchDecisions(ctx, vector, contextLimit, false)
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
	go func() {
		defer wg.Done()
		bundle.artifacts, bundle.artifactErr = searchContextArtifacts(query, contextLimit)
	}()

	wg.Wait()

	// Persist a full successful bundle to the LKG cache (best-effort).
	if bundle.decErr == nil && bundle.rejErr == nil && bundle.taskErr == nil &&
		bundle.discErr == nil && bundle.noteErr == nil && bundle.artifactErr == nil {
		if cfg, cfgErr := config.Load(); cfgErr == nil {
			if rtDir, rtErr := cfg.RuntimeDir(); rtErr == nil {
				_ = cache.Put(rtDir, contextCacheKind, query, contextPayload{
					Decisions:   bundle.decisions,
					Rejections:  bundle.rejections,
					Tasks:       bundle.tasks,
					Discussions: bundle.discussions,
					Notes:       bundle.notes,
					Artifacts:   bundle.artifacts,
				})
			}
		}
	}

	var linkedWarnings []string
	if contextIncludeLinked {
		cfg, cfgErr := config.Load()
		if cfgErr != nil {
			return cfgErr
		}
		markBundleSource(&bundle, cfg.ProjectID)
		linkedWarnings = appendLinkedContext(ctx, cfg, &bundle, vector)
	}

	errs := collectBundleErrors(bundle)
	errs = append(errs, linkedWarnings...)

	total := len(bundle.decisions) + len(bundle.rejections) + len(bundle.tasks) + len(bundle.discussions) + len(bundle.notes) + len(bundle.artifacts)
	if total == 0 && len(errs) > 0 {
		return fmt.Errorf("all searches failed:\n  %s", strings.Join(errs, "\n  "))
	}

	return printContextBundle(cmd, query, bundle, errs, time.Time{})
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
		banner := "⚠ Qdrant unreachable — no cached context available for this topic"
		fmt.Fprintln(cmd.OutOrStderr(), banner)
		bundle := contextBundle{}
		artifacts, artifactErr := searchContextArtifacts(query, contextLimit)
		if artifactErr == nil {
			bundle.artifacts = artifacts
		} else {
			bundle.artifactErr = artifactErr
		}
		if len(bundle.artifacts) == 0 && bundle.artifactErr == nil {
			return nil
		}
		errs := append([]string{banner}, collectBundleErrors(bundle)...)
		return printContextBundle(cmd, query, bundle, errs, time.Time{})
	}

	banner := fmt.Sprintf("⚠ Qdrant unreachable — cache may be stale; last update at %s", cachedAt.Local().Format("2006-01-02 15:04:05"))
	fmt.Fprintln(cmd.OutOrStderr(), banner)

	bundle := contextBundle{
		decisions:   payload.Decisions,
		rejections:  payload.Rejections,
		tasks:       payload.Tasks,
		discussions: payload.Discussions,
		notes:       payload.Notes,
		artifacts:   payload.Artifacts,
	}
	artifacts, artifactErr := searchContextArtifacts(query, contextLimit)
	if artifactErr == nil {
		bundle.artifacts = artifacts
	} else {
		bundle.artifactErr = artifactErr
	}
	// Pass banner as a warning and cachedAt so --json consumers see the stale signal.
	errs := append([]string{banner}, collectBundleErrors(bundle)...)
	return printContextBundle(cmd, query, bundle, errs, cachedAt)
}

// runContextForTask fetches the anchor task, its DependsOn dependencies, and
// semantically related decisions/rejections, then renders a compact resumption bundle.
func runContextForTask(cmd *cobra.Command, taskID string) error {
	d, err := loadDepsReadOnly(true)
	if err != nil {
		return err
	}
	defer d.Close()

	if d.qdrantDown {
		return runContextForTaskJSONLFallback(cmd, taskID, "qdrant unreachable")
	}
	if d.qdrantSlow {
		return runContextForTaskJSONLFallback(cmd, taskID, "qdrant health check timed out")
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	anchor, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("task %s: %w", taskID, err)
	}

	// Fetch dependency tasks in parallel.
	var deps []store.Task
	if len(anchor.DependsOn) > 0 {
		type result struct {
			t   *store.Task
			err error
		}
		ch := make(chan result, len(anchor.DependsOn))
		for _, depID := range anchor.DependsOn {
			depID := depID
			go func() {
				t, err := d.store.GetTask(ctx, depID)
				ch <- result{t, err}
			}()
		}
		for range anchor.DependsOn {
			r := <-ch
			if r.err == nil {
				deps = append(deps, *r.t)
			}
		}
	}

	// Build semantic query from the anchor task's title and detail.
	queryText := anchor.Title
	if anchor.Detail != "" {
		queryText += " " + anchor.Detail
	}

	vector, err := d.embedder.Generate(ctx, queryText)
	if err != nil {
		return runContextForTaskJSONLFallback(cmd, taskID, fmt.Sprintf("generate embedding: %v", err))
	}

	var bundle contextBundle
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		bundle.decisions, bundle.decErr = d.store.SearchDecisions(ctx, vector, contextLimit, false)
	}()
	go func() {
		defer wg.Done()
		bundle.rejections, bundle.rejErr = d.store.SearchRejections(ctx, vector, contextLimit)
	}()
	wg.Wait()

	// Boost decisions whose TaskID matches the anchor or whose tags overlap.
	anchorTags := make(map[string]bool, len(anchor.Tags))
	for _, tag := range anchor.Tags {
		anchorTags[tag] = true
	}
	bundle.decisions = boostRelatedDecisions(bundle.decisions, anchor.ID, anchorTags)

	tb := taskContextBundle{anchor: *anchor, deps: deps, bundle: bundle}

	var errs []string
	if bundle.decErr != nil {
		errs = append(errs, fmt.Sprintf("decisions: %v", bundle.decErr))
	}
	if bundle.rejErr != nil {
		errs = append(errs, fmt.Sprintf("rejections: %v", bundle.rejErr))
	}

	return printJSON(map[string]any{
		"task":       anchor,
		"deps":       deps,
		"decisions":  bundle.decisions,
		"rejections": bundle.rejections,
		"warnings":   errs,
	}, func() {
		if isCompactActive(cmd) {
			emitCompact(cmd, "context",
				func(w io.Writer) { renderForTask(w, tb, errs) },
				func(w io.Writer) { renderForTaskCompact(w, tb, errs) },
				compactRendererV_context,
			)
			return
		}
		emitHydration(cmd, "context", func(w io.Writer) { renderForTask(w, tb, errs) })
		renderForTask(cmd.OutOrStdout(), tb, errs)
	})
}

// renderForTaskCompact emits the --for-task bundle as one line per item.
func renderForTaskCompact(w io.Writer, tb taskContextBundle, errs []string) {
	a := tb.anchor
	extra := ""
	if len(tb.bundle.notes) > 0 {
		extra += fmt.Sprintf(" %dN", len(tb.bundle.notes))
	}
	if len(tb.messages) > 0 {
		extra += fmt.Sprintf(" %dM", len(tb.messages))
	}
	fmt.Fprintf(w, "for-task: %s [%s/%s] %s — %d deps %dD %dR%s\n\n",
		a.ID, a.Status, a.Priority, compactTrim(a.Title, compactLineWidth),
		len(tb.deps), len(tb.bundle.decisions), len(tb.bundle.rejections), extra)
	for _, dep := range tb.deps {
		fmt.Fprintln(w, compactTaskLine(dep))
	}
	writeCompactDecisions(w, tb.bundle.decisions)
	writeCompactRejections(w, tb.bundle.rejections)
	writeCompactNotes(w, tb.bundle.notes)
	for _, m := range tb.messages {
		fmt.Fprintf(w, "M  %s [%s→%s] %s\n", shortDate(m.CreatedAt), m.FromRole, m.ToRole, compactTrim(m.Content, compactLineWidth))
	}
	if len(errs) > 0 {
		fmt.Fprintf(w, "\n! %s\n", strings.Join(errs, "; "))
	}
}
