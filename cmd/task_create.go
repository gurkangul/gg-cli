package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

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

func init() {
	taskCreateCmd.Flags().StringVar(&taskDetail, "detail", "", "task description")
	taskCreateCmd.Flags().StringVar(&taskPriority, "priority", "medium", "priority: high, medium, low")
	taskCreateCmd.Flags().StringVar(&taskTags, "tags", "", "comma-separated tags")
	taskCreateCmd.Flags().StringVar(&taskDependsOn, "depends-on", "", "comma-separated task IDs this task depends on (e.g. TASK-001,TASK-002)")
	addFromFlag(taskCreateCmd)
	taskCmd.AddCommand(taskCreateCmd)
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
