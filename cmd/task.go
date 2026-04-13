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
	taskDetail   string
	taskPriority string
	taskTags     string
)

var validPriorities = map[string]bool{"high": true, "medium": true, "low": true}
var validStatuses = map[string]bool{"pending": true, "in_progress": true, "done": true, "blocked": true}

func init() {
	taskCreateCmd.Flags().StringVar(&taskDetail, "detail", "", "task description")
	taskCreateCmd.Flags().StringVar(&taskPriority, "priority", "medium", "priority: high, medium, low")
	taskCreateCmd.Flags().StringVar(&taskTags, "tags", "", "comma-separated tags")

	taskListCmd.Flags().StringVar(&taskListStatus, "status", "", "filter by status: pending, in_progress, done, blocked")

	taskCmd.AddCommand(taskCreateCmd)
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskGetCmd)
	taskCmd.AddCommand(taskDoneCmd)
	taskCmd.AddCommand(taskBlockCmd)
	rootCmd.AddCommand(taskCmd)
}

func runTaskCreate(cmd *cobra.Command, args []string) error {
	title := args[0]

	if !validPriorities[taskPriority] {
		return fmt.Errorf("invalid priority %q — use high, medium, or low", taskPriority)
	}

	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	embedText := title
	if taskDetail != "" {
		embedText = title + " " + taskDetail
	}
	vector, err := d.embedder.Generate(embedText)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	ctx, cancel := cmdContext()
	defer cancel()

	t := store.Task{
		Title:    title,
		Detail:   taskDetail,
		Priority: taskPriority,
		Tags:     parseTags(taskTags),
	}

	id, err := d.store.CreateTask(ctx, t, vector)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}

	fmt.Printf("✓ Task created: %s — %s\n", id, title)
	return nil
}

// --- task list ---

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	RunE:  runTaskList,
}

var taskListStatus string

func runTaskList(cmd *cobra.Command, args []string) error {
	if taskListStatus != "" && !validStatuses[taskListStatus] {
		return fmt.Errorf("invalid status %q — use pending, in_progress, done, or blocked", taskListStatus)
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := cmdContext()
	defer cancel()

	tasks, err := d.store.ListTasks(ctx, taskListStatus)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found.")
		return nil
	}

	for _, t := range tasks {
		status := statusIcon(t.Status)
		fmt.Printf("%s %s [%s] %s\n", status, t.ID, t.Priority, t.Title)
		if t.Status == "blocked" && t.BlockReason != "" {
			fmt.Printf("    ⚠ Blocked: %s\n", t.BlockReason)
		}
	}
	return nil
}

// --- task get ---

var taskGetCmd = &cobra.Command{
	Use:   "get TASK-ID",
	Short: "Get task details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskGet,
}

func runTaskGet(cmd *cobra.Command, args []string) error {
	taskID := strings.ToUpper(args[0])

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := cmdContext()
	defer cancel()

	t, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

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
	fmt.Printf("  Created: %s\n", t.CreatedAt)
	return nil
}

// --- task done ---

var taskDoneCmd = &cobra.Command{
	Use:   `done TASK-ID "summary"`,
	Short: "Mark a task as done",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskDone,
}

func runTaskDone(cmd *cobra.Command, args []string) error {
	taskID := strings.ToUpper(args[0])
	summary := args[1]

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := cmdContext()
	defer cancel()

	if err := d.store.UpdateTaskStatus(ctx, taskID, "done", summary); err != nil {
		return err
	}

	fmt.Printf("✓ %s marked as done\n", taskID)
	return nil
}

// --- task block ---

var taskBlockCmd = &cobra.Command{
	Use:   `block TASK-ID "reason"`,
	Short: "Mark a task as blocked",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskBlock,
}

func runTaskBlock(cmd *cobra.Command, args []string) error {
	taskID := strings.ToUpper(args[0])
	reason := args[1]

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := cmdContext()
	defer cancel()

	if err := d.store.UpdateTaskStatus(ctx, taskID, "blocked", reason); err != nil {
		return err
	}

	fmt.Printf("⚠ %s marked as blocked: %s\n", taskID, reason)
	return nil
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
