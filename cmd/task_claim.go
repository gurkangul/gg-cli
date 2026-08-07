package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/identity"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/telemetry"
	"github.com/spf13/cobra"
)

var (
	taskStartOwner     string
	taskStartLease     time.Duration
	taskStartNoContext bool
	taskRenewOwner     string
	taskRenewLease     time.Duration
	taskReleaseOwner   string
	taskUnblockOwner   string
	taskUnblockLease   time.Duration
)

var taskStartCmd = &cobra.Command{
	Use:   "start TASK-ID",
	Short: "Claim a task and move it to in_progress",
	Long: `Claim a task for one agent and attach a time-bounded lease.

WHEN TO USE: an agent is actively taking ownership of a pending task. The
claim is stored on the task and visible in task list/get so other agents avoid
colliding with the same work.

Existing active claims are refused unless the lease has expired.

A successful claim also prints an === Related Context === block: the top-3
decisions, rejected approaches, and notes semantically related to this task.
Claiming is the moment the topic is known, so prior decisions are pushed here
rather than left to a flag the agent has to remember. The block is capped at
~800 tokens and never fails the claim — if the vector store or embedder is
unavailable it degrades to a one-line notice. Use --no-context to suppress it.`,
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

var taskUnblockCmd = &cobra.Command{
	Use:   "unblock TASK-ID",
	Short: "Clear a task's blocked state and return it to in_progress",
	Long: `Return a blocked task to active work — the non-destructive inverse of 'gg task block'.

WHEN TO USE: the dependency a task was blocked on has cleared and you want to
resume it. Moves the task from blocked back to in_progress under the caller with
a fresh lease and clears the stored block reason.

Refused if the task is not blocked, or if another agent holds an active lease.`,
	Args: cobra.ExactArgs(1),
	RunE: runTaskUnblock,
}

func init() {
	taskStartCmd.Flags().StringVar(&taskStartOwner, "owner", "", "agent taking the claim (defaults to $GG_AGENT / $GG_ROLE)")
	taskStartCmd.Flags().DurationVar(&taskStartLease, "lease", 30*time.Minute, "claim lease duration (for example 30m, 2h)")
	taskStartCmd.Flags().BoolVar(&taskStartNoContext, "no-context", false, "suppress the === Related Context === block (for scripted/CI callers)")
	taskRenewCmd.Flags().StringVar(&taskRenewOwner, "owner", "", "agent renewing the claim (defaults to $GG_AGENT / $GG_ROLE)")
	taskRenewCmd.Flags().DurationVar(&taskRenewLease, "lease", 30*time.Minute, "new lease duration from now")
	taskReleaseCmd.Flags().StringVar(&taskReleaseOwner, "owner", "", "agent releasing the claim (defaults to $GG_AGENT / $GG_ROLE)")
	taskUnblockCmd.Flags().StringVar(&taskUnblockOwner, "owner", "", "agent resuming the task (defaults to $GG_AGENT / $GG_ROLE)")
	taskUnblockCmd.Flags().DurationVar(&taskUnblockLease, "lease", 30*time.Minute, "claim lease duration (for example 30m, 2h)")
	taskCmd.AddCommand(taskStartCmd)
	taskCmd.AddCommand(taskRenewCmd)
	taskCmd.AddCommand(taskReleaseCmd)
	taskCmd.AddCommand(taskUnblockCmd)
}

func resolveTaskOwner(flagValue string) string {
	if owner := strings.TrimSpace(flagValue); owner != "" {
		return owner
	}
	// BUG-084: derive a per-session identity so two Claude tabs sharing the
	// generic GG_AGENT=claude-code do not collapse into one task owner.
	if owner := strings.TrimSpace(identity.Agent()); owner != "" {
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
	// The embedder is only constructed when the memory packet will actually be
	// rendered — building it is cheap and offline, and a dead embedding backend
	// degrades inside fetchRelatedContext rather than failing the claim.
	d, err := loadDeps(!taskStartNoContext)
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

	// TASK-538: push the task-scoped memory packet at claim time. Faz 1 shipped
	// this same block behind `gg task get --with-context`, where it scored 0 of
	// 482 get calls in 7 days — an opt-in read path is not read. Claiming is the
	// one moment the topic is already known, so the packet is pushed here.
	// Never fatal: a nil relatedContext renders as an "(unavailable)" notice and
	// the claim still succeeds.
	ctxBlock := taskStartContextBlock(d, t)

	payload := map[string]any{
		"id":          taskID,
		"status":      t.Status,
		"owner":       t.Owner,
		"lease_until": t.LeaseUntil,
	}
	if ctxBlock.Len() > 0 {
		payload["related_context"] = ctxBlock.String()
	}

	return printJSON(payload, func() {
		fmt.Printf("→ %s started by %s (lease until %s)\n", taskID, t.Owner, t.LeaseUntil)
		_, _ = os.Stdout.Write(ctxBlock.Bytes())
	})
}

// taskStartContextBlock renders the === Related Context === block for a freshly
// claimed task and records the injection in telemetry so its usage is countable
// the same way `--with-context` is. Returns an empty buffer when the caller
// passed --no-context.
func taskStartContextBlock(d *deps, t *store.Task) *bytes.Buffer {
	var buf bytes.Buffer
	if taskStartNoContext {
		return &buf
	}

	var rc *relatedContext
	if d.embedder != nil {
		rc = fetchRelatedContext(d, t)
	}
	renderRelatedContext(&buf, rc)

	if cfg, cfgErr := config.Load(); cfgErr == nil {
		if rtDir, rtErr := cfg.RuntimeDir(); rtErr == nil {
			telemetry.RecordWithContext(rtDir, telemetry.VerbTaskStartContext, "", buf.Len(), rc.items())
		}
	}
	return &buf
}

func runTaskUnblock(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	owner := resolveTaskOwner(taskUnblockOwner)
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

	t, err := d.store.UnblockTask(ctx, taskID, owner, taskUnblockLease)
	if err != nil {
		return err
	}
	notifyTaskLifecycle(ctx, d.store, taskID, "unblocked", owner)

	return printJSON(map[string]any{
		"id":          taskID,
		"status":      t.Status,
		"owner":       t.Owner,
		"lease_until": t.LeaseUntil,
	}, func() {
		fmt.Printf("▲ %s unblocked by %s — back to in_progress (lease until %s)\n", taskID, t.Owner, t.LeaseUntil)
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
