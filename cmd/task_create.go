package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

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
another agent use 'gg message send'.

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

	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	embedText := title
	if taskDetail != "" {
		embedText = title + " " + taskDetail
	}
	vector, err := d.embedder.Generate(ctx, embedText)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	if promptIfDuplicate(ctx, d, "tasks", vector) {
		fmt.Println("Aborted — no task created.")
		return nil
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

	id, err := d.store.CreateTask(ctx, t, vector)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}

	notifyTaskLifecycle(ctx, d.store, id, "created", title)

	return printJSON(map[string]any{"id": id, "title": title}, func() {
		fmt.Printf("✓ Task created: %s — %s\n", id, title)
	})
}
