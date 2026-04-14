package cmd

import (
	"fmt"
	"strings"

	"github.com/gurkangul/gg/internal/store"
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Manage tasks",
}

// --- task create ---

var taskCreateCmd = &cobra.Command{
	Use:   `create "title"`,
	Short: "Create a new task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskCreate,
}

var (
	taskDetail    string
	taskPriority  string
	taskTags      string
	taskDependsOn string
)

var validPriorities = map[string]bool{"high": true, "medium": true, "low": true}
var validStatuses = map[string]bool{"pending": true, "in_progress": true, "done": true, "blocked": true}

func init() {
	taskCreateCmd.Flags().StringVar(&taskDetail, "detail", "", "task description")
	taskCreateCmd.Flags().StringVar(&taskPriority, "priority", "medium", "priority: high, medium, low")
	taskCreateCmd.Flags().StringVar(&taskTags, "tags", "", "comma-separated tags")
	taskCreateCmd.Flags().StringVar(&taskDependsOn, "depends-on", "", "comma-separated task IDs this task depends on (e.g. TASK-001,TASK-002)")
	addFromFlag(taskCreateCmd)

	taskListCmd.Flags().StringVar(&taskListStatus, "status", "", "filter by status: pending, in_progress, done, blocked")
	taskListCmd.Flags().BoolVar(&taskListReady, "ready", false, "show only pending tasks whose dependencies are all done")

	taskCmd.AddCommand(taskCreateCmd)
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskGetCmd)
	taskCmd.AddCommand(taskDoneCmd)
	taskCmd.AddCommand(taskBlockCmd)
	taskCmd.AddCommand(taskDepsCmd)
	rootCmd.AddCommand(taskCmd)
}

func runTaskCreate(cmd *cobra.Command, args []string) error {
	title, err := requireNonEmpty("title", args[0])
	if err != nil {
		return err
	}
	if !validPriorities[taskPriority] {
		return fmt.Errorf("invalid priority %q — use high, medium, or low", taskPriority)
	}

	// Validate and normalise depends-on task IDs.
	deps, err := parseTaskIDList(taskDependsOn)
	if err != nil {
		return fmt.Errorf("--depends-on: %w", err)
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

	t := store.Task{
		Title:     title,
		Detail:    strings.TrimSpace(taskDetail),
		Priority:  taskPriority,
		Tags:      parseTags(taskTags),
		DependsOn: deps,
		Author:    resolveAuthor(cmd),
	}

	id, err := d.store.CreateTask(ctx, t, vector)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}

	return printJSON(map[string]any{"id": id, "title": title}, func() {
		fmt.Printf("✓ Task created: %s — %s\n", id, title)
	})
}

// --- task list ---

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	RunE:  runTaskList,
}

var (
	taskListStatus string
	taskListReady  bool
)

func runTaskList(cmd *cobra.Command, args []string) error {
	if taskListStatus != "" && !validStatuses[taskListStatus] {
		return fmt.Errorf("invalid status %q — use pending, in_progress, done, or blocked", taskListStatus)
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

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
		for _, t := range tasks {
			author := ""
			if t.Author != "" {
				author = " (" + t.Author + ")"
			}
			fmt.Printf("%s %s [%s] %s%s\n", statusIcon(t.Status), t.ID, t.Priority, t.Title, author)
			if t.Status == "blocked" && t.BlockReason != "" {
				fmt.Printf("    ⚠ Blocked: %s\n", t.BlockReason)
			}
		}
	})
}

// --- task get ---

var taskGetCmd = &cobra.Command{
	Use:   "get TASK-ID",
	Short: "Get task details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskGet,
}

func runTaskGet(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
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

	return printJSON(t, func() {
		fmt.Printf("%s %s [%s] %s\n", statusIcon(t.Status), t.ID, t.Priority, t.Title)
		if t.Detail != "" {
			fmt.Printf("  Detail: %s\n", t.Detail)
		}
		if len(t.Tags) > 0 {
			fmt.Printf("  Tags: %s\n", strings.Join(t.Tags, ", "))
		}
		if len(t.DependsOn) > 0 {
			fmt.Printf("  Depends on: %s\n", strings.Join(t.DependsOn, ", "))
		}
		if t.BlockReason != "" {
			fmt.Printf("  ⚠ Blocked: %s\n", t.BlockReason)
		}
		if t.DoneSummary != "" {
			fmt.Printf("  ✓ Done: %s\n", t.DoneSummary)
		}
		if t.Author != "" {
			fmt.Printf("  By: %s\n", t.Author)
		}
		fmt.Printf("  Created: %s\n", t.CreatedAt)
	})
}

// --- task done ---

var taskDoneCmd = &cobra.Command{
	Use:   `done TASK-ID "summary"`,
	Short: "Mark a task as done",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskDone,
}

func runTaskDone(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	summary, err := requireNonEmpty("summary", args[1])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.UpdateTaskStatus(ctx, taskID, "done", summary); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": taskID, "status": "done", "summary": summary}, func() {
		fmt.Printf("✓ %s marked as done\n", taskID)
	})
}

// --- task block ---

var taskBlockCmd = &cobra.Command{
	Use:   `block TASK-ID "reason"`,
	Short: "Mark a task as blocked",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskBlock,
}

func runTaskBlock(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	reason, err := requireNonEmpty("reason", args[1])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.UpdateTaskStatus(ctx, taskID, "blocked", reason); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": taskID, "status": "blocked", "reason": reason}, func() {
		fmt.Printf("⚠ %s marked as blocked: %s\n", taskID, reason)
	})
}

// --- task deps ---

var taskDepsCmd = &cobra.Command{
	Use:   "deps TASK-ID",
	Short: "Show dependency status for a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskDeps,
}

func runTaskDeps(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
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

	type depEntry struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Priority string `json:"priority,omitempty"`
		Title    string `json:"title,omitempty"`
		Found    bool   `json:"found"`
	}

	var deps []depEntry
	allDone := true
	for _, depID := range t.DependsOn {
		dep, err := d.store.GetTask(ctx, depID)
		if err != nil {
			deps = append(deps, depEntry{ID: depID, Found: false})
			allDone = false
			continue
		}
		deps = append(deps, depEntry{ID: dep.ID, Status: dep.Status, Priority: dep.Priority, Title: dep.Title, Found: true})
		if dep.Status != "done" {
			allDone = false
		}
	}

	payload := map[string]any{
		"task_id":  taskID,
		"all_done": allDone,
		"deps":     deps,
	}
	return printJSON(payload, func() {
		if len(deps) == 0 {
			fmt.Printf("%s has no dependencies.\n", taskID)
			return
		}
		fmt.Printf("Dependencies of %s:\n", taskID)
		for _, dep := range deps {
			if !dep.Found {
				fmt.Printf("  ! %-12s (not found)\n", dep.ID)
				continue
			}
			fmt.Printf("  %s %-12s [%s] %s\n", statusIcon(dep.Status), dep.ID, dep.Priority, dep.Title)
		}
		fmt.Println()
		if allDone {
			fmt.Printf("✓ All dependencies done — %s is ready to start.\n", taskID)
		} else {
			fmt.Printf("○ Not all dependencies are done — %s is blocked by the above.\n", taskID)
		}
	})
}

// parseTaskIDList parses a comma-separated list of task IDs, validating each.
func parseTaskIDList(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		id := strings.ToUpper(strings.TrimSpace(p))
		if id == "" {
			continue
		}
		if _, err := store.ParseTaskID(id); err != nil {
			return nil, fmt.Errorf("invalid task ID %q: %w", p, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func statusIcon(status string) string {
	switch status {
	case "done":
		return "✓"
	case "blocked":
		return "⚠"
	case "in_progress":
		return "→"
	default:
		return "○"
	}
}
