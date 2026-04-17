package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
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
	taskListStatus string
	taskListReady  bool
	taskGetCompact bool
)

func init() {
	taskListCmd.Flags().StringVar(&taskListStatus, "status", "", "filter by status: pending, in_progress, done, blocked")
	taskListCmd.Flags().BoolVar(&taskListReady, "ready", false, "show only pending tasks whose dependencies are all done")
	taskGetCmd.Flags().BoolVar(&taskGetCompact, "compact", false, "one line summary — drops detail/tags/author to preserve agent context window")
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskGetCmd)
}

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
		if taskGetCompact {
			emitCompact(cmd, "task",
				func(w io.Writer) { renderTaskGetDefault(w, t) },
				func(w io.Writer) { renderTaskGetCompact(w, t) },
			)
			return
		}
		renderTaskGetDefault(os.Stdout, t)
	})
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
