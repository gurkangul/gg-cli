package cmd

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

var brainBackfillApply bool

var brainBackfillCmd = &cobra.Command{
	Use:   "backfill",
	Short: "Migrate tag-based Task↔Decision links to Memgraph edges",
	Long: `Scan Qdrant for implicit Task↔Decision relationships and write them as
explicit Memgraph edges — (Decision)-[:DECIDES]->(Task) — tagged with
created_by=backfill_v1 for rollback identification.

Two sources are evaluated:

  Source 1 — Direct links: Decision.task_id field carries an explicit task ID.
             These are unambiguous and always migrated.

  Source 2 — Tag overlap: if exactly one decision and exactly one task share a
             tag, that pair is unambiguous and is migrated. Tags matched by
             multiple decisions or tasks are reported as ambiguous and skipped.

By default the command runs in dry-run mode and prints a report without
writing anything. Pass --apply to execute the migration.

Examples:
  gg brain backfill              # audit only
  gg brain backfill --apply      # write edges to Memgraph`,
	RunE: runBrainBackfill,
}

func init() {
	brainBackfillCmd.Flags().BoolVar(&brainBackfillApply, "apply", false, "write edges to Memgraph (default: dry-run only)")
}

// backfillLink is a resolved Task↔Decision pair ready to be written as an edge.
type backfillLink struct {
	decisionID   string // Qdrant UUID of the decision
	decisionText string
	taskID       string // logical task ID like "TASK-003"
	source       string // "direct" or tag value
}

func runBrainBackfill(cmd *cobra.Command, _ []string) error {
	printProjectBanner()

	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	// Load all tasks and decisions from Qdrant.
	tasks, err := d.store.ListTasks(ctx, "")
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}
	decisions, err := d.store.ListDecisions(ctx, 0)
	if err != nil {
		return fmt.Errorf("list decisions: %w", err)
	}

	// Build indexes.
	taskByID := make(map[string]store.Task, len(tasks))
	for _, t := range tasks {
		taskByID[t.ID] = t
	}

	tagToTasks := make(map[string][]store.Task)
	for _, t := range tasks {
		for _, tag := range t.Tags {
			tagToTasks[tag] = append(tagToTasks[tag], t)
		}
	}

	tagToDecisions := make(map[string][]store.Decision)
	for _, dec := range decisions {
		for _, tag := range dec.Tags {
			tagToDecisions[tag] = append(tagToDecisions[tag], dec)
		}
	}

	// --- Source 1: Decision.TaskID direct links ---
	var directLinks []backfillLink
	for _, dec := range decisions {
		if dec.TaskID == "" {
			continue
		}
		if _, ok := taskByID[dec.TaskID]; !ok {
			// TaskID set but task not found — orphan reference, skip silently.
			continue
		}
		directLinks = append(directLinks, backfillLink{
			decisionID:   dec.ID,
			decisionText: dec.Text,
			taskID:       dec.TaskID,
			source:       "direct",
		})
	}

	// --- Source 2: Tag-based links ---
	// Build dedup set from direct links so we don't double-count.
	directPairs := make(map[string]bool) // key: "decID|taskID"
	for _, l := range directLinks {
		directPairs[l.decisionID+"|"+l.taskID] = true
	}

	allTags := make([]string, 0, len(tagToDecisions))
	for tag := range tagToDecisions {
		allTags = append(allTags, tag)
	}
	sort.Strings(allTags)

	var tagLinks []backfillLink
	type ambiguous struct {
		tag        string
		nDecisions int
		nTasks     int
	}
	var ambiguousTags []ambiguous
	var orphanTags []string // tags with decisions but no tasks (or vice versa)

	for _, tag := range allTags {
		decs := tagToDecisions[tag]
		tsks := tagToTasks[tag]

		if len(tsks) == 0 {
			orphanTags = append(orphanTags, tag)
			continue
		}
		if len(decs) > 1 || len(tsks) > 1 {
			ambiguousTags = append(ambiguousTags, ambiguous{tag, len(decs), len(tsks)})
			continue
		}
		// Exactly one decision, one task.
		dec := decs[0]
		tsk := tsks[0]
		pair := dec.ID + "|" + tsk.ID
		if directPairs[pair] {
			continue // already covered by Source 1
		}
		directPairs[pair] = true // prevent duplicate from multiple shared tags
		tagLinks = append(tagLinks, backfillLink{
			decisionID:   dec.ID,
			decisionText: dec.Text,
			taskID:       tsk.ID,
			source:       tag,
		})
	}

	// Also gather orphan task-side tags (tasks with tags that no decision shares).
	for tag := range tagToTasks {
		if _, ok := tagToDecisions[tag]; !ok {
			orphanTags = append(orphanTags, tag)
		}
	}
	sort.Strings(orphanTags)
	// deduplicate orphanTags (a tag could appear from both sides above).
	orphanTags = deduplicateStrings(orphanTags)

	allLinks := make([]backfillLink, 0, len(directLinks)+len(tagLinks))
	allLinks = append(allLinks, directLinks...)
	allLinks = append(allLinks, tagLinks...)

	mode := "DRY RUN"
	if brainBackfillApply {
		mode = "APPLYING"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "=== gg brain backfill — %s ===\n\n", mode)

	// Print Source 1.
	fmt.Fprintf(cmd.OutOrStdout(), "── Source 1: Decision.task_id direct links (%d) ─────────────────\n", len(directLinks))
	if len(directLinks) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "  (none)")
	}
	for _, l := range directLinks {
		short := l.decisionText
		if len(short) > 60 {
			short = short[:57] + "..."
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  ✓  %s ← \"%s\" [dec:%s]\n", l.taskID, short, l.decisionID[:8])
	}

	// Print Source 2.
	fmt.Fprintf(cmd.OutOrStdout(), "\n── Source 2: Tag-based links (%d unambiguous) ─────────────────\n", len(tagLinks))
	if len(tagLinks) == 0 && len(ambiguousTags) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "  (none)")
	}
	for _, l := range tagLinks {
		short := l.decisionText
		if len(short) > 50 {
			short = short[:47] + "..."
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  ✓  tag %q: \"%s\" → %s\n", l.source, short, l.taskID)
	}
	for _, a := range ambiguousTags {
		fmt.Fprintf(cmd.OutOrStdout(), "  ⚠  tag %q: %d decision(s) × %d task(s) — AMBIGUOUS (skipped)\n", a.tag, a.nDecisions, a.nTasks)
	}

	// Pre-flight summary.
	fmt.Fprintf(cmd.OutOrStdout(), "\n── Pre-flight Summary ─────────────────────────────────────────\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Unambiguous links ready: %d (%d direct + %d tag-based)\n", len(allLinks), len(directLinks), len(tagLinks))
	fmt.Fprintf(cmd.OutOrStdout(), "  Ambiguous tags skipped:  %d\n", len(ambiguousTags))
	fmt.Fprintf(cmd.OutOrStdout(), "  Orphan tags (no match):  %d\n", len(orphanTags))

	if len(allLinks) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nNothing to migrate.")
		return nil
	}

	if !brainBackfillApply {
		fmt.Fprintf(cmd.OutOrStdout(), "\nTo apply, run: gg brain backfill --apply\n")
		return nil
	}

	// --- Apply: write Memgraph edges ---
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.Memgraph.URI == "" {
		return fmt.Errorf("memgraph not configured — set memgraph.uri in .gg/config.yaml")
	}
	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		return fmt.Errorf("connect Memgraph: %w", err)
	}
	defer func() { _ = gc.Close(cmd.Context()) }()

	fmt.Fprintf(cmd.OutOrStdout(), "\n── Writing edges ──────────────────────────────────────────────\n")
	written, skipped := 0, 0
	var writeErrors []string
	for _, l := range allLinks {
		// Ensure both nodes exist before writing the edge.
		if uErr := gc.UpsertDecisionNode(ctx, l.decisionID, l.decisionText); uErr != nil {
			writeErrors = append(writeErrors, fmt.Sprintf("Decision node %s: %v", l.decisionID[:8], uErr))
			skipped++
			continue
		}
		if uErr := gc.UpsertTaskNode(ctx, l.taskID, l.taskID); uErr != nil {
			writeErrors = append(writeErrors, fmt.Sprintf("Task node %s: %v", l.taskID, uErr))
			skipped++
			continue
		}
		if eErr := gc.UpsertDecidesEdgeBackfill(ctx, l.decisionID, l.taskID); eErr != nil {
			writeErrors = append(writeErrors, fmt.Sprintf("DECIDES edge %s→%s: %v", l.decisionID[:8], l.taskID, eErr))
			skipped++
			continue
		}
		src := l.source
		if src == "direct" {
			fmt.Fprintf(cmd.OutOrStdout(), "  ✓  (Decision %s…) -[:DECIDES]-> (%s)  [direct]\n", l.decisionID[:8], l.taskID)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "  ✓  (Decision %s…) -[:DECIDES]-> (%s)  [tag:%s]\n", l.decisionID[:8], l.taskID, src)
		}
		written++
	}

	// Post-migrate audit.
	fmt.Fprintf(cmd.OutOrStdout(), "\n── Post-migrate Audit ─────────────────────────────────────────\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Edges written:  %d\n", written)
	fmt.Fprintf(cmd.OutOrStdout(), "  Edges skipped:  %d (write error)\n", skipped)
	fmt.Fprintf(cmd.OutOrStdout(), "  Ambiguous tags: %d (manual review required)\n", len(ambiguousTags))
	fmt.Fprintf(cmd.OutOrStdout(), "  Orphan tags:    %d (no matching task or decision)\n", len(orphanTags))

	if len(writeErrors) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nErrors:\n")
		for _, e := range writeErrors {
			fmt.Fprintf(cmd.OutOrStdout(), "  ✗ %s\n", e)
		}
	}

	if written > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nRollback: edges carry created_by=backfill_v1 — query Memgraph to identify and delete if needed.\n")
	}

	return nil
}

// deduplicateStrings returns a sorted slice with duplicates removed.
func deduplicateStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := ss[:0]
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
