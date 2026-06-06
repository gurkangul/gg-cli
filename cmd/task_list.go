package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/projectstate"
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
	taskListPendingAck  bool
	taskListCompact     bool
	taskGetCompact      bool
	taskGetShort        bool
	taskGetWithCtx      bool
)

func init() {
	taskListCmd.Flags().StringVar(&taskListStatus, "status", "", "filter by status: pending, in_progress, done, blocked")
	taskListCmd.Flags().BoolVar(&taskListReady, "ready", false, "show only pending tasks whose dependencies are all done")
	taskListCmd.Flags().BoolVar(&taskListNeedsReview, "needs-review", false, "show done tasks awaiting review (review_status=none or pending)")
	taskListCmd.Flags().BoolVar(&taskListBlockers, "blockers", false, "show tasks that are blocking other tasks (have --blocks targets)")
	taskListCmd.Flags().BoolVar(&taskListPendingAck, "pending-ack", false, "show in-progress tasks whose worker ACK is waiting for ACK-OK or ACK-FIX")
	taskListCmd.Flags().BoolVar(&taskListCompact, "compact", false, "one line per task — drops author + block-reason detail to preserve agent context window")
	taskGetCmd.Flags().BoolVar(&taskGetCompact, "compact", false, "one line summary — drops detail/tags/author to preserve agent context window")
	taskGetCmd.Flags().BoolVar(&taskGetShort, "short", false, "one line summary (alias for --compact)")
	taskGetCmd.Flags().BoolVar(&taskGetWithCtx, "with-context", false, "append === Related Context === block with top-3 semantically related items from the knowledge base")
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskGetCmd)
}

func runTaskList(cmd *cobra.Command, args []string) error {
	if taskListStatus != "" && !validStatuses[taskListStatus] {
		return fmt.Errorf("invalid status %q — use pending, in_progress, ready_for_live, done, or blocked", taskListStatus)
	}
	if taskListPendingAck && taskListStatus != "" && taskListStatus != "in_progress" {
		return fmt.Errorf("--pending-ack can only be combined with --status in_progress")
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
	if taskListPendingAck {
		statusFilter = "in_progress"
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
	if taskListPendingAck {
		msgs, msgErr := d.store.ListMessagesSince(ctx, time.Now().UTC().AddDate(0, 0, -30))
		if msgErr != nil {
			return fmt.Errorf("list messages for pending ack: %w", msgErr)
		}
		tasks = filterPendingAckTasks(tasks, msgs)
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
				compactRendererV_taskList,
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
		if t.Owner != "" {
			fmt.Fprintf(w, "    → Owner: %s (lease until %s)\n", t.Owner, t.LeaseUntil)
		}
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

// reAck and reResolved anchor on word-boundary TASK-NNN to prevent false
// matches on e.g. "TASK-1234" matching "TASK-12".
var (
	reAck      = regexp.MustCompile(`\bTASK-\d+\b ACK:`)
	reResolved = regexp.MustCompile(`\bTASK-\d+\b ACK(?:-OK|-FIX)`)
)

func filterPendingAckTasks(tasks []store.Task, msgs []store.Message) []store.Task {
	// Track the latest timestamp for ACK and resolution messages per task.
	// A task is pending-ack when its last ACK timestamp is strictly later than
	// its last resolution timestamp — this handles the re-ACK-after-ACK-FIX
	// case correctly (boolean presence would permanently resolve the task).
	lastAck := map[string]string{}      // task ID → latest ACK message CreatedAt
	lastResolved := map[string]string{} // task ID → latest ACK-OK/ACK-FIX CreatedAt
	for _, m := range msgs {
		id := strings.ToUpper(strings.TrimSpace(m.TaskID))
		content := strings.ToUpper(m.Content)
		if id == "" {
			for _, t := range tasks {
				if strings.Contains(content, t.ID) {
					id = t.ID
					break
				}
			}
		}
		if id == "" {
			continue
		}
		ts := m.CreatedAt
		if reAck.MatchString(content) && strings.Contains(content, id) {
			if ts > lastAck[id] {
				lastAck[id] = ts
			}
		}
		if reResolved.MatchString(content) && strings.Contains(content, id) {
			if ts > lastResolved[id] {
				lastResolved[id] = ts
			}
		}
	}
	out := make([]store.Task, 0, len(tasks))
	for _, t := range tasks {
		ackAt := lastAck[t.ID]
		resolvedAt := lastResolved[t.ID]
		// Pending-ack: worker sent an ACK and master has not replied yet, OR
		// master replied ACK-FIX and worker has since re-ACKed (ackAt > resolvedAt).
		if ackAt != "" && ackAt > resolvedAt {
			out = append(out, t)
		}
	}
	return out
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

	// task get normally shows the full Detail block — it is a targeted lookup,
	// not a list scan. Agent auto-compact (GG_AGENT/GG_ROLE env) must not
	// suppress the spec that workers need. --compact and --short are still
	// respected when explicitly provided (BUG-027/TASK-341).
	shortExplicit := taskGetShort || (cmd != nil && func() bool {
		for _, name := range []string{"compact", "short"} {
			f := cmd.Flags().Lookup(name)
			if f != nil && f.Changed && f.Value.String() == "true" {
				return true
			}
		}
		return false
	}())
	// BUG-074: only a full (non-compact) read proves hydration. Compact/--short
	// drop reasons/details, so they must not satisfy the hydration gate.
	if !shortExplicit && !isCompactActive(cmd) {
		recordTaskFullHydration(t.ID)
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

		if shortExplicit {
			emitCompact(cmd, "task",
				func(w io.Writer) {
					renderTaskGetDefault(w, t)
					_, _ = w.Write(ctxBlock.Bytes())
				},
				func(w io.Writer) {
					renderTaskGetCompact(w, t)
					_, _ = w.Write(ctxBlock.Bytes())
				},
				compactRendererV_taskGet,
			)
		} else {
			emitHydration(cmd, "get", func(w io.Writer) {
				renderTaskGetDefault(w, t)
				_, _ = w.Write(ctxBlock.Bytes())
			})
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
	fmt.Fprintf(w, "  Status: %s\n", t.Status)
	if t.Detail != "" {
		fmt.Fprintf(w, "  Detail: %s\n", t.Detail)
	}
	if len(t.Tags) > 0 {
		fmt.Fprintf(w, "  Tags: %s\n", strings.Join(t.Tags, ", "))
	}
	if len(t.DependsOn) > 0 {
		fmt.Fprintf(w, "  Depends on: %s\n", strings.Join(t.DependsOn, ", "))
	}
	if t.Owner != "" {
		fmt.Fprintf(w, "  Owner: %s\n", t.Owner)
		if t.ClaimedAt != "" {
			fmt.Fprintf(w, "  Claimed: %s\n", t.ClaimedAt)
		}
		if t.LeaseUntil != "" {
			fmt.Fprintf(w, "  Lease until: %s\n", t.LeaseUntil)
		}
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
	} else if t.Owner != "" {
		suffix = " @" + compactTrim(t.Owner, 24)
	}
	fmt.Fprintf(w, "%s %s [%s] %s%s\n",
		statusIcon(t.Status), t.ID, t.Priority, compactTrim(t.Title, compactLineWidth), suffix)
}

func recordTaskFullHydration(taskID string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ could not record task hydration proof: %v\n", err)
		return
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ could not record task hydration proof: %v\n", err)
		return
	}
	if err := projectstate.RecordHydration(runtimeDir, "task", taskID); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ could not record task hydration proof: %v\n", err)
	}
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
