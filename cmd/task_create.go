package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

// metaTaskPattern matches task titles that fall into the self-feeding
// meta-backlog (AGENTS.md edits, CHANGELOG bumps, tracker-rule enforcement,
// hook wiring, parity work). These are frozen under stabilization mode.
// Override with GG_ALLOW_META=<reason>.
var metaTaskPattern = regexp.MustCompile(`(?i)(AGENTS\.md|CHANGELOG|tracker.?rule|hook|parity|meta|enforcement)`)

// validRequesters are the allowed values for --requester.
var validRequesters = map[string]bool{"user": true, "agent": true, "system": true}

var taskCreateCmd = &cobra.Command{
	Use:   `create "title"`,
	Short: "Create a task to track a discrete unit of work",
	Long: `Create a task in the shared brain. Tasks coordinate work across agents.

WHEN TO USE: you have a concrete action item — something that can be done, verified,
and marked done. Use --priority to signal urgency and --depends-on to declare ordering.

WHEN NOT TO USE: for open-ended exploration use 'gg record'; for async questions to
another agent use 'gg tell <to> <msg> --from <role>'.

See also: gg task list (view tasks), gg task done (close a task), gg task deps (check blockers)`,
	Args: cobra.ExactArgs(1),
	RunE: runTaskCreate,
}

var (
	taskDetail    string
	taskPriority  string
	taskTags      string
	taskDependsOn string
	taskBlocks    string
	taskDeadline  string
	taskRequester string
)

func init() {
	taskCreateCmd.Flags().StringVar(&taskDetail, "detail", "", "task description")
	taskCreateCmd.Flags().StringVar(&taskPriority, "priority", "", "priority: high, medium, low (omit to leave unset)")
	taskCreateCmd.Flags().StringVar(&taskTags, "tags", "", "comma-separated tags")
	taskCreateCmd.Flags().StringVar(&taskDependsOn, "depends-on", "", "comma-separated task IDs this task depends on (e.g. TASK-001,TASK-002)")
	taskCreateCmd.Flags().StringVar(&taskBlocks, "blocks", "", "comma-separated task IDs that this task is blocking")
	taskCreateCmd.Flags().StringVar(&taskDeadline, "deadline", "", "deadline date (YYYY-MM-DD)")
	taskCreateCmd.Flags().StringVar(&taskRequester, "requester", "", "who initiated this task: user, agent, or system (required)")
	_ = taskCreateCmd.MarkFlagRequired("requester")
	addFromFlag(taskCreateCmd)
	taskCmd.AddCommand(taskCreateCmd)
}

func runTaskCreate(cmd *cobra.Command, args []string) error {
	printProjectBanner()
	title, err := requireNonEmpty("title", args[0])
	if err != nil {
		return err
	}
	if !validRequesters[taskRequester] {
		return fmt.Errorf("--requester must be one of: user, agent, system")
	}
	if metaTaskPattern.MatchString(title) {
		if reason := os.Getenv("GG_ALLOW_META"); reason == "" {
			return &ExitError{
				Code:    ExitNotFound,
				Message: fmt.Sprintf("meta-task blocked: title matches stabilization freeze pattern — set GG_ALLOW_META=<reason> to override: %q", title),
			}
		}
	}
	if taskPriority != "" && !validPriorities[taskPriority] {
		return fmt.Errorf("invalid priority %q — use high, medium, or low (or omit to leave unset)", taskPriority)
	}

	// Validate and normalise depends-on task IDs.
	deps, err := parseTaskIDList(taskDependsOn)
	if err != nil {
		return fmt.Errorf("--depends-on: %w", err)
	}

	blocks, err := parseTaskIDList(taskBlocks)
	if err != nil {
		return fmt.Errorf("--blocks: %w", err)
	}

	if err := requireAgentIdentity(); err != nil {
		return err
	}

	// AC-2: Use offline-safe loader so task creation survives Qdrant downtime.
	d, err := loadDepsOfflineSafe(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// AC-4: InboxGatePreflight fails-open when Qdrant is unreachable.
	if err := runInboxGatePreflight(ctx, d.store, "task-create"); err != nil {
		return err
	}

	var vector []float32
	if !d.qdrantDown {
		embedText := title
		if taskDetail != "" {
			embedText = title + " " + taskDetail
		}
		vector, err = d.embedder.Generate(ctx, embedText)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ embedding unavailable — JSONL write will continue and semantic indexing will be queued: %v\n", err)
		} else {
			// AC-4: skip dedup prompt when Qdrant is down or embedding failed.
			if promptIfDuplicate(ctx, d, "tasks", vector) {
				fmt.Println("Aborted — no task created.")
				return nil
			}
		}
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "⚠ Qdrant unreachable — read served from JSONL (may miss cross-project context)")
	}

	t := store.Task{
		Title:     title,
		Detail:    strings.TrimSpace(taskDetail),
		Priority:  taskPriority,
		Tags:      parseTags(taskTags),
		DependsOn: deps,
		Blocks:    blocks,
		Deadline:  strings.TrimSpace(taskDeadline),
		Author:    resolveAuthor(cmd),
		Requester: taskRequester,
	}

	id, createErr := d.store.CreateTask(ctx, t, vector)
	if createErr != nil {
		var oq *store.OutboxQueued
		if errors.As(createErr, &oq) {
			queueBrainOutbox(oq, config.GGDirOrEmpty())
			warnBrainOutboxQueued(cmd.ErrOrStderr(), oq.Cause)
			if isSandboxPermissionError(oq.Cause) {
				fmt.Fprintln(cmd.ErrOrStderr(), "⚠ sandbox EPERM detected — outbox entry queued, but the agent should rerun outside sandbox to avoid drift")
			}
		} else {
			return fmt.Errorf("create task: %w", createErr)
		}
	}

	upsertTaskGraphNode(cmd, ctx, id, title)
	upsertTaskDependencyEdges(cmd, ctx, id, deps, blocks)

	notifyTaskLifecycle(ctx, d.store, id, "created", title)

	return printJSON(map[string]any{"id": id, "title": title}, func() {
		fmt.Printf("✓ Task created: %s — %s\n", id, title)
	})
}

// upsertTaskGraphNode mirrors the Qdrant Task write into Memgraph so that
// `gg impact` and graph traversals see every task, not only ones later
// referenced by `gg record --implements`. Failures are non-fatal: Qdrant is
// the source of truth; Memgraph is a derived view rebuildable via
// `gg brain backfill`. Matches the pattern in cmd/record.go:writeGraphEdges.
func upsertTaskGraphNode(cmd *cobra.Command, ctx context.Context, id, title string) {
	cfg, err := config.Load()
	if err != nil || cfg.Memgraph.URI == "" {
		return
	}

	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph init failed (%v) — Task node not created for %s\n", err, id)
		return
	}
	defer func() { _ = gc.Close(ctx) }()

	if uErr := gc.UpsertTaskNode(ctx, id, title); uErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph Task node upsert failed for %s: %v\n", id, uErr)
	}
}

// upsertTaskDependencyEdges writes (Task)-[:DEPENDS_ON]->(Task) edges for every
// --depends-on target and (Task)-[:BLOCKS]->(Task) edges for every --blocks
// target. Stub-upserts the target Task node first so the edge MATCH succeeds
// even when the other task predates TASK-225's dual-write. Non-fatal.
func upsertTaskDependencyEdges(cmd *cobra.Command, ctx context.Context, id string, deps, blocks []string) {
	if len(deps) == 0 && len(blocks) == 0 {
		return
	}
	cfg, err := config.Load()
	if err != nil || cfg.Memgraph.URI == "" {
		return
	}
	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph init failed (%v) — dependency edges for %s skipped\n", err, id)
		return
	}
	defer func() { _ = gc.Close(ctx) }()

	for _, dep := range deps {
		if dep == "" {
			continue
		}
		if uErr := gc.UpsertTaskNode(ctx, dep, dep); uErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph stub Task node upsert failed for %s: %v\n", dep, uErr)
			continue
		}
		if eErr := gc.UpsertDependsOnEdge(ctx, id, dep); eErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph DEPENDS_ON edge %s→%s failed: %v\n", id, dep, eErr)
		}
	}
	for _, bl := range blocks {
		if bl == "" {
			continue
		}
		if uErr := gc.UpsertTaskNode(ctx, bl, bl); uErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph stub Task node upsert failed for %s: %v\n", bl, uErr)
			continue
		}
		if eErr := gc.UpsertBlocksEdge(ctx, id, bl); eErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph BLOCKS edge %s→%s failed: %v\n", id, bl, eErr)
		}
	}
}
