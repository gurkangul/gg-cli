package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	taskStartOwner   string
	taskStartLease   time.Duration
	taskRenewOwner   string
	taskRenewLease   time.Duration
	taskReleaseOwner string
)

var taskStartCmd = &cobra.Command{
	Use:   "start TASK-ID",
	Short: "Claim a task and move it to in_progress",
	Long: `Claim a task for one agent and attach a time-bounded lease.

WHEN TO USE: an agent is actively taking ownership of a pending task. The
claim is stored on the task and visible in task list/get so other agents avoid
colliding with the same work.

Existing active claims are refused unless the lease has expired.`,
	Args: cobra.ExactArgs(1),
	RunE: runTaskStart,
}

var taskRenewCmd = &cobra.Command{
	Use:   "renew TASK-ID",
	Short: "Renew the current owner lease for an in-progress task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskRenew,
}

var taskReleaseCmd = &cobra.Command{
	Use:   "release TASK-ID",
	Short: "Release the current task claim and return it to pending",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskRelease,
}

func init() {
	taskStartCmd.Flags().StringVar(&taskStartOwner, "owner", "", "agent taking the claim (defaults to $GG_AGENT / $GG_ROLE)")
	taskStartCmd.Flags().DurationVar(&taskStartLease, "lease", 30*time.Minute, "claim lease duration (for example 30m, 2h)")
	taskRenewCmd.Flags().StringVar(&taskRenewOwner, "owner", "", "agent renewing the claim (defaults to $GG_AGENT / $GG_ROLE)")
	taskRenewCmd.Flags().DurationVar(&taskRenewLease, "lease", 30*time.Minute, "new lease duration from now")
	taskReleaseCmd.Flags().StringVar(&taskReleaseOwner, "owner", "", "agent releasing the claim (defaults to $GG_AGENT / $GG_ROLE)")
	taskCmd.AddCommand(taskStartCmd)
	taskCmd.AddCommand(taskRenewCmd)
	taskCmd.AddCommand(taskReleaseCmd)
}

func resolveTaskOwner(flagValue string) string {
	if owner := strings.TrimSpace(flagValue); owner != "" {
		return owner
	}
	if owner := strings.TrimSpace(os.Getenv("GG_AGENT")); owner != "" {
		return owner
	}
	return strings.TrimSpace(os.Getenv("GG_ROLE"))
}

func runTaskStart(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	owner := resolveTaskOwner(taskStartOwner)
	if owner == "" {
		return fmt.Errorf("--owner is required (or set GG_AGENT / GG_ROLE)")
	}
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	t, err := d.store.StartTask(ctx, taskID, owner, taskStartLease)
	if err != nil {
		return err
	}
	notifyTaskLifecycle(ctx, d.store, taskID, "started", owner)

	return printJSON(map[string]any{
		"id":          taskID,
		"status":      t.Status,
		"owner":       t.Owner,
		"lease_until": t.LeaseUntil,
	}, func() {
		fmt.Printf("→ %s started by %s (lease until %s)\n", taskID, t.Owner, t.LeaseUntil)
	})
}

func runTaskRenew(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	owner := resolveTaskOwner(taskRenewOwner)
	if owner == "" {
		return fmt.Errorf("--owner is required (or set GG_AGENT / GG_ROLE)")
	}
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	t, err := d.store.RenewTask(ctx, taskID, owner, taskRenewLease)
	if err != nil {
		return err
	}
	notifyTaskLifecycle(ctx, d.store, taskID, "renewed", owner)

	return printJSON(map[string]any{
		"id":          taskID,
		"status":      t.Status,
		"owner":       t.Owner,
		"lease_until": t.LeaseUntil,
	}, func() {
		fmt.Printf("↻ %s lease renewed by %s until %s\n", taskID, t.Owner, t.LeaseUntil)
	})
}

func runTaskRelease(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	owner := resolveTaskOwner(taskReleaseOwner)
	if owner == "" {
		return fmt.Errorf("--owner is required (or set GG_AGENT / GG_ROLE)")
	}
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	t, err := d.store.ReleaseTask(ctx, taskID, owner)
	if err != nil {
		return err
	}
	notifyTaskLifecycle(ctx, d.store, taskID, "released", owner)

	return printJSON(map[string]any{
		"id":     taskID,
		"status": t.Status,
		"owner":  t.Owner,
	}, func() {
		fmt.Printf("○ %s released by %s\n", taskID, owner)
	})
}
