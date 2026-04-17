package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/hooks"
)

var taskDoneCmd = &cobra.Command{
	Use:   `done TASK-ID "summary"`,
	Short: "Mark a task done — include a one-sentence summary of what was accomplished",
	Long: `Close a task and record a completion summary in the shared brain.

WHEN TO USE: you have finished the work described in the task. The summary is stored
and surfaced in 'gg status' and 'gg search' — write it for the next agent that reads it.

See also: gg task review (request peer review), gg record (capture design decisions made during the work)`,
	Args: cobra.ExactArgs(2),
	RunE: runTaskDone,
}

var taskBlockCmd = &cobra.Command{
	Use:   `block TASK-ID "reason"`,
	Short: "Mark a task blocked — state what specific dependency is missing",
	Long: `Signal that work is stalled because of an external dependency or unresolved question.

WHEN TO USE: you cannot make progress without input from another agent or an external
resource. The reason is stored and shown in 'gg status --blocked'.

WHEN NOT TO USE: for long-term deprioritization, update priority instead.`,
	Args: cobra.ExactArgs(2),
	RunE: runTaskBlock,
}

var taskDepsCmd = &cobra.Command{
	Use:   "deps TASK-ID",
	Short: "Show dependency status for a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskDeps,
}

func init() {
	taskCmd.AddCommand(taskDoneCmd)
	taskCmd.AddCommand(taskBlockCmd)
	taskCmd.AddCommand(taskDepsCmd)
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

	// Run post-done hooks from .gg/hooks/task-done.d/*.sh (warn-only unless hooks.strict=true).
	if hookErr := runTaskDoneHooks(cmd, taskID, summary); hookErr != nil {
		return hookErr // only non-nil when strict mode is enabled and a hook failed
	}

	warnBinaryStale()

	return printJSON(map[string]any{"id": taskID, "status": "done", "summary": summary}, func() {
		fmt.Printf("✓ %s marked as done\n", taskID)
	})
}

// runTaskDoneHooks runs .gg/hooks/task-done.d/*.sh scripts after a task is
// marked done. Returns a non-nil error only in strict mode when a hook fails.
func runTaskDoneHooks(cmd *cobra.Command, taskID, summary string) error {
	ggDir, err := config.GGDir()
	if err != nil {
		return nil // can't find .gg — skip silently
	}
	cfg, err := config.Load()
	if err != nil {
		return nil
	}

	env := map[string]string{
		"GG_TASK_ID":      taskID,
		"GG_TASK_SUMMARY": summary,
		"GG_PROJECT_ID":   cfg.ProjectID,
	}

	_, hookErr := hooks.RunHooks(ggDir, "task-done", env, cfg.Hooks.Strict, cmd.ErrOrStderr())
	return hookErr
}

// warnBinaryStale prints an advisory when the most recently committed Go
// source file in the project is newer than the installed gg binary. This
// catches the "gg task done while binary is stale" workflow problem.
//
// The check is entirely advisory: it never fails the command. Silence it
// with GG_SKIP_SHIP_CHECK=1.
func warnBinaryStale() {
	if os.Getenv("GG_SKIP_SHIP_CHECK") == "1" {
		return
	}

	projectRoot, err := config.FindRoot()
	if err != nil {
		return // not in a gg project — skip
	}

	// Only warn in Go repos (go.mod present) to avoid false positives in
	// Python/JS projects where .go sources don't map to the gg binary.
	if _, statErr := os.Stat(filepath.Join(projectRoot, "go.mod")); statErr != nil {
		return
	}

	// Last commit timestamp touching any *.go file (Unix seconds, empty = no Go files).
	out, runErr := exec.Command("git", "-C", projectRoot, "log", "-1", "--format=%ct", "--", "*.go").Output()
	if runErr != nil || len(strings.TrimSpace(string(out))) == 0 {
		return
	}
	srcTS, convErr := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if convErr != nil {
		return
	}
	srcTime := time.Unix(srcTS, 0)

	// Find installed binary.
	binPath, lookErr := exec.LookPath("gg")
	if lookErr != nil {
		return
	}
	info, statErr := os.Stat(binPath)
	if statErr != nil {
		return
	}

	if srcTime.After(info.ModTime()) {
		fmt.Fprintf(os.Stderr, "\n⚠  Source files modified after installed binary mtime.\n")
		fmt.Fprintf(os.Stderr, "   Binary: %s (built %s)\n", binPath, info.ModTime().Format("2006-01-02 15:04"))
		fmt.Fprintf(os.Stderr, "   Source: last commit %s\n", srcTime.Format("2006-01-02 15:04"))
		fmt.Fprintf(os.Stderr, "   Run: go install ./... (or your install path) then re-test.\n")
		fmt.Fprintf(os.Stderr, "   This task is marked done but may not be live in your shell.\n\n")
	}
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
