package cmd

// task_claim.go — gg task claim-files subcommand (TASK-276 AC2).
//
// Advisory file-lock: a task registers its intent to modify specific paths.
// The queue runner checks locks.json before spawning each worker and blocks
// on collision unless --force is passed.

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/locks"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
)

var taskClaimFilesCmd = &cobra.Command{
	Use:   "claim-files <task-id> [path...]",
	Short: "Advisorily claim file paths for a task (prevents parallel spawn collisions)",
	Long: `Register an advisory file-lock claim for a task.

The queue runner reads locks.json before spawning each worker. If a new task
claims a path that is already claimed by an active task, the spawn is blocked
with a 'collision: TASK-X already claims <path>' error. Pass --force on
'gg spawn queue start' to override.

Calling claim-files replaces any prior claim for the same task.
Calling with no paths releases the task's claim (same as 'gg task release-files').

Examples:
  gg task claim-files TASK-042 internal/store/store.go cmd/status.go
  gg task claim-files TASK-042           # release all claims for TASK-042
  gg task release-files TASK-042         # alias`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTaskClaimFiles,
}

var taskClaimForce bool

func init() {
	taskClaimFilesCmd.Flags().BoolVar(&taskClaimForce, "force", false, "override existing claims from other tasks (logs collision, writes anyway)")
	taskCmd.AddCommand(taskClaimFilesCmd)
}

func runTaskClaimFiles(_ *cobra.Command, args []string) error {
	taskID := strings.ToUpper(strings.TrimSpace(args[0]))
	paths := args[1:]

	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	lk := locks.New(spawn.Dir(rt))

	if len(paths) == 0 {
		// Release all claims.
		if err := lk.Release(taskID); err != nil {
			return fmt.Errorf("release claim: %w", err)
		}
		fmt.Printf("✓ Advisory claim released for %s.\n", taskID)
		return nil
	}

	// Check collisions before claiming (unless --force).
	if !taskClaimForce {
		collisions, collErr := lk.CheckConflicts(paths, taskID)
		if collErr != nil {
			return fmt.Errorf("check collisions: %w", collErr)
		}
		if len(collisions) > 0 {
			return fmt.Errorf("%s", collisions.Error())
		}
	}

	if err := lk.Acquire(taskID, paths, taskClaimForce); err != nil {
		// Acquire can also return CollisionList if force=false and we raced.
		return fmt.Errorf("acquire claim: %w", err)
	}

	fmt.Printf("✓ %s claims %d path(s):\n", taskID, len(paths))
	for _, p := range paths {
		fmt.Printf("  • %s\n", p)
	}
	return nil
}

// taskReleaseFilesCmd is a convenience alias for claim-files with no paths.
var taskReleaseFilesCmd = &cobra.Command{
	Use:   "release-files <task-id>",
	Short: "Release advisory file-lock claims for a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		taskID := strings.ToUpper(strings.TrimSpace(args[0]))
		rt, err := spawnRuntimeDir()
		if err != nil {
			return err
		}
		lk := locks.New(spawn.Dir(rt))
		if err := lk.Release(taskID); err != nil {
			return fmt.Errorf("release: %w", err)
		}
		fmt.Printf("✓ Advisory claim released for %s.\n", taskID)
		return nil
	},
}

func init() {
	taskCmd.AddCommand(taskReleaseFilesCmd)
}

// taskListClaimsCmd shows all active advisory claims.
var taskListClaimsCmd = &cobra.Command{
	Use:   "list-claims",
	Short: "Show all active advisory file-lock claims",
	RunE:  runTaskListClaims,
}

func init() {
	taskCmd.AddCommand(taskListClaimsCmd)
}

func runTaskListClaims(_ *cobra.Command, _ []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}
	lk := locks.New(spawn.Dir(rt))
	claims, err := lk.ReadAll()
	if err != nil {
		return fmt.Errorf("read claims: %w", err)
	}
	if len(claims) == 0 {
		fmt.Println("(no active advisory claims)")
		return nil
	}
	for _, c := range claims {
		fmt.Printf("%s  (%d path(s), claimed %s)\n", c.TaskID, len(c.Paths), c.ClaimedAt.Format("2006-01-02 15:04:05"))
		for _, p := range c.Paths {
			fmt.Printf("  • %s\n", p)
		}
	}
	return nil
}
