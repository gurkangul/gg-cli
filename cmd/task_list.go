package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	RunE:  runTaskList,
}

var taskGetCmd = &cobra.Command{
	Use:   "get TASK-ID",
	Short: "Get task details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskGet,
}

var (
	taskListStatus      string
	taskListReady       bool
	taskListNeedsReview bool
	taskListBlockers    bool
	taskListCompact     bool
	taskGetCompact      bool
	taskGetWithCtx      bool
)

func init() {
	taskListCmd.Flags().StringVar(&taskListStatus, "status", "", "filter by status: pending, in_progress, done, blocked")
	taskListCmd.Flags().BoolVar(&taskListReady, "ready", false, "show only pending tasks whose dependencies are all done")
	taskListCmd.Flags().BoolVar(&taskListNeedsReview, "needs-review", false, "show done tasks awaiting review (review_status=none or pending)")
	taskListCmd.Flags().BoolVar(&taskListBlockers, "blockers", false, "show tasks that are blocking other tasks (have --blocks targets)")
	taskListCmd.Flags().BoolVar(&taskListCompact, "compact", false, "one line per task — drops author + block-reason detail to preserve agent context window")
	taskGetCmd.Flags().BoolVar(&taskGetCompact, "compact", false, "one line summary — drops detail/tags/author to preserve agent context window")
	taskGetCmd.Flags().BoolVar(&taskGetWithCtx, "with-context", false, "append === Related Context === block with top-3 semantically related items from the knowledge base")
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskGetCmd)
}

func runTaskList(cmd *cobra.Command, args []string) error {
	if taskListStatus != "" && !validStatuses[taskListStatus] {
		return fmt.Errorf("invalid status %q — use pending, in_progress, ready_for_live, done, or blocked", taskListStatus)
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if taskListNeedsReview {
		tasks, err := d.store.ListTasksNeedsReview(ctx)
		if err != nil {
			return fmt.Errorf("list tasks needing review: %w", err)
		}
		return printJSON(tasks, func() {
			if len(tasks) == 0 {
				fmt.Println("No tasks awaiting review.")
				return
			}
			for _, t := range tasks {
				fmt.Printf("○ %s [review_status=%s] %s\n", t.ID, t.ReviewStatus, t.Title)
			}
		})
	}

	if taskListBlockers {
		all, err := d.store.ListTasks(ctx, "")
		if err != nil {
			return fmt.Errorf("list tasks: %w", err)
		}
		var blockers []store.Task
		for _, t := range all {
			if len(t.Blocks) > 0 && t.Status != "done" {
				blockers = append(blockers, t)
			}
		}
		return printJSON(blockers, func() {
			if len(blockers) == 0 {
				fmt.Println("No active blocker tasks.")
				return
			}
			for _, t := range blockers {
				fmt.Printf("⚠ %s [%s] %s → blocks: %s\n", t.ID, t.Priority, t.Title, strings.Join(t.Blocks, ", "))
			}
		})
	}

	// --ready implicitly filters to pending tasks.
	statusFilter := taskListStatus
	if taskListReady {
		statusFilter = "pending"
	}

	tasks, err := d.store.ListTasks(ctx, statusFilter)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	// --ready: build a done-set, then keep only tasks with all deps satisfied.
	if taskListReady {
		doneTasks, listErr := d.store.ListTasks(ctx, "done")
		if listErr != nil {
			return fmt.Errorf("list done tasks: %w", listErr)
		}
		doneSet := make(map[string]bool, len(doneTasks))
		for _, t := range doneTasks {
			doneSet[t.ID] = true
		}
		var ready []store.Task
		for _, t := range tasks {
			allDone := true
			for _, dep := range t.DependsOn {
				if !doneSet[dep] {
					allDone = false
					break
				}
			}
			if allDone {
				ready = append(ready, t)
			}
		}
		tasks = ready
	}

	return printJSON(tasks, func() {
		if len(tasks) == 0 {
			if taskListReady {
				fmt.Println("No ready tasks — all pending tasks have unfinished dependencies.")
			} else {
				fmt.Println("No tasks found.")
			}
			return
		}
		if isCompactActive(cmd) {
			emitCompact(cmd, "list",
				func(w io.Writer) { renderTaskListDefault(w, tasks) },
				func(w io.Writer) { renderTaskListCompact(w, tasks) },
			)
			return
		}
		renderTaskListDefault(os.Stdout, tasks)
	})
}

func renderTaskListDefault(w io.Writer, tasks []store.Task) {
	for _, t := range tasks {
		author := ""
		if t.Author != "" {
			author = " (" + t.Author + ")"
		}
		fmt.Fprintf(w, "%s %s [%s] %s%s\n", statusIcon(t.Status), t.ID, t.Priority, t.Title, author)
		if t.Status == "blocked" && t.BlockReason != "" {
			fmt.Fprintf(w, "    ⚠ Blocked: %s\n", t.BlockReason)
		}
	}
}

func renderTaskListCompact(w io.Writer, tasks []store.Task) {
	for _, t := range tasks {
		fmt.Fprintln(w, compactTaskLine(t))
	}
}

func runTaskGet(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}

	d, err := loadDeps(taskGetWithCtx)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	t, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return notFound(err.Error())
	}

	// Fetch related context when --with-context is set and embedder is available.
	var relCtx *relatedContext
	if taskGetWithCtx && d.embedder != nil {
		relCtx = fetchRelatedContext(d, t)
	}

	return printJSON(t, func() {
		// Render the with-context block into a buffer up front so both the
		// compact and default paths can include it in their byte measurements
		// consistently — otherwise --compact + --with-context would compare
		// compact-no-ctx against default-no-ctx and overstate savings.
		var ctxBlock bytes.Buffer
		if taskGetWithCtx {
			renderRelatedContext(&ctxBlock, relCtx)
		}

		if isCompactActive(cmd) {
			emitCompact(cmd, "task",
				func(w io.Writer) {
					renderTaskGetDefault(w, t)
					_, _ = w.Write(ctxBlock.Bytes())
				},
				func(w io.Writer) {
					renderTaskGetCompact(w, t)
					_, _ = w.Write(ctxBlock.Bytes())
				},
			)
		} else {
			renderTaskGetDefault(os.Stdout, t)
			_, _ = os.Stdout.Write(ctxBlock.Bytes())
		}

		if taskGetWithCtx {
			if cfg, cfgErr := config.Load(); cfgErr == nil {
				if rtDir, rtErr := cfg.RuntimeDir(); rtErr == nil {
					telemetry.RecordWithContext(rtDir, "task", "", ctxBlock.Len())
				}
			}
		}
	})
}

// relatedContext holds the top-3 semantically related items for a task.
type relatedContext struct {
	decisions  []store.Decision
	rejections []store.Rejection
	notes      []store.Note
}

const withContextLimit uint64 = 3
const withContextMaxBytes = 3072 // ~800 tokens hard cap

// fetchRelatedContext embeds the task title+detail and searches Qdrant for top-3 items.
func fetchRelatedContext(d *deps, t *store.Task) *relatedContext {
	query := t.Title
	if t.Detail != "" {
		query = t.Title + " " + t.Detail
	}

	// Use a fresh timeout so we don't share the parent's deadline.
	ctx, cancel := withTimeout(context.Background())
	defer cancel()

	vector, err := d.embedder.Generate(ctx, query)
	if err != nil {
		return nil
	}

	rc := &relatedContext{}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		rc.decisions, _ = d.store.SearchDecisions(ctx, vector, withContextLimit)
	}()
	go func() {
		defer wg.Done()
		rc.rejections, _ = d.store.SearchRejections(ctx, vector, withContextLimit)
	}()
	go func() {
		defer wg.Done()
		rc.notes, _ = d.store.SearchNotes(ctx, vector, withContextLimit)
	}()
	wg.Wait()
	return rc
}

func renderTaskGetDefault(w io.Writer, t *store.Task) {
	fmt.Fprintf(w, "%s %s [%s] %s\n", statusIcon(t.Status), t.ID, t.Priority, t.Title)
	if t.Detail != "" {
		fmt.Fprintf(w, "  Detail: %s\n", t.Detail)
	}
	if len(t.Tags) > 0 {
		fmt.Fprintf(w, "  Tags: %s\n", strings.Join(t.Tags, ", "))
	}
	if len(t.DependsOn) > 0 {
		fmt.Fprintf(w, "  Depends on: %s\n", strings.Join(t.DependsOn, ", "))
	}
	if t.BlockReason != "" {
		fmt.Fprintf(w, "  ⚠ Blocked: %s\n", t.BlockReason)
	}
	if t.ReadyForLiveBy != "" {
		fmt.Fprintf(w, "  ◉ Ready for live: %s (by %s)\n", t.ReadyForLivePlan, t.ReadyForLiveBy)
	}
	if t.DoneSummary != "" {
		fmt.Fprintf(w, "  ✓ Done: %s\n", t.DoneSummary)
	}
	if t.Author != "" {
		fmt.Fprintf(w, "  By: %s\n", t.Author)
	}
	fmt.Fprintf(w, "  Created: %s\n", t.CreatedAt)
}

func renderTaskGetCompact(w io.Writer, t *store.Task) {
	suffix := ""
	if t.Status == "blocked" && t.BlockReason != "" {
		suffix = " ⚠" + compactTrim(t.BlockReason, 60)
	} else if t.Status == "done" && t.DoneSummary != "" {
		suffix = " ✓" + compactTrim(t.DoneSummary, 60)
	}
	fmt.Fprintf(w, "%s %s [%s] %s%s\n",
		statusIcon(t.Status), t.ID, t.Priority, compactTrim(t.Title, compactLineWidth), suffix)
}

// renderRelatedContext writes the === Related Context === block to w.
// Each item is a single line (ID/date + 1-sentence summary). The block is
// capped at withContextMaxBytes to enforce the ≤800-token hard cap.
// When rc is nil (Qdrant unavailable or embed failed), writes a brief notice.
func renderRelatedContext(w *bytes.Buffer, rc *relatedContext) {
	fmt.Fprintln(w, "\n=== Related Context ===")

	if rc == nil {
		fmt.Fprintln(w, "  (unavailable — Qdrant unreachable or embedding failed)")
		return
	}

	total := len(rc.decisions) + len(rc.rejections) + len(rc.notes)
	if total == 0 {
		fmt.Fprintln(w, "  (no related items found)")
		return
	}

	var lines []string
	for _, dec := range rc.decisions {
		lines = append(lines, compactDecisionLine(dec))
	}
	for _, r := range rc.rejections {
		lines = append(lines, compactRejectionLine(r))
	}
	for _, n := range rc.notes {
		lines = append(lines, compactNoteLine(n))
	}

	// Enforce byte cap — stop adding lines once we'd exceed the limit.
	// Header already written; budget from current buffer length.
	budget := withContextMaxBytes - w.Len()
	for _, line := range lines {
		entry := "  " + line + "\n"
		if budget-len(entry) < 0 {
			fmt.Fprintln(w, "  … (truncated — context block size limit reached)")
			break
		}
		fmt.Fprint(w, entry)
		budget -= len(entry)
	}
}
