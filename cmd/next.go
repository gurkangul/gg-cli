package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Recommend the next safe agent command",
	Long: `Recommend the next safe command for an agent without changing workflow state.

This command is read-only: it does not advance inbox cursors, claim tasks,
review work, or mark tasks done. It only inspects current task/inbox state and
prints explicit commands the agent may choose to run next.

If CodeGraph is missing, stale, or unavailable, next prints the shared freshness
notice. It does not refresh in the background; use gg doctor --fix-index for
explicit repair or gg index --watch / gg watch --index for foreground active
mode.`,
	Args: cobra.NoArgs,
	RunE: runNext,
}

var (
	nextAgent string
	nextRole  string
)

func init() {
	nextCmd.Flags().StringVar(&nextAgent, "agent", "", "agent id to plan for (defaults to $GG_AGENT)")
	nextCmd.Flags().StringVar(&nextRole, "role", "", "role to plan for (defaults to $GG_ROLE)")
	rootCmd.AddCommand(nextCmd)
}

type nextSnapshot struct {
	Agent          string
	Role           string
	InboxCount     int
	ActiveTasks    []store.Task
	ReadyForLive   []store.Task
	ReadyTasks     []store.Task
	StateWarnings  []string
	ReviewerRole   bool
	StoreDegraded  bool
	StoreDegradeIs string
}

func runNext(cmd *cobra.Command, _ []string) error {
	agent := resolveNextValue(nextAgent, "GG_AGENT", "$GG_AGENT")
	role := resolveNextValue(nextRole, "GG_ROLE", "$GG_ROLE")
	snap := nextSnapshot{Agent: agent, Role: role, ReviewerRole: strings.Contains(strings.ToLower(role), "review")}

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	if d.qdrantDown || d.qdrantSlow {
		snap.StoreDegraded = true
		if d.qdrantSlow {
			snap.StoreDegradeIs = "Qdrant health check timed out"
		} else {
			snap.StoreDegradeIs = "Qdrant unreachable"
		}
		renderNext(cmd.OutOrStdout(), snap)
		return nil
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()
	if warning := semanticMemoryActionWarning(ctx, nil); warning != "" {
		snap.StateWarnings = append(snap.StateWarnings, warning)
	}
	if warning := codeGraphActionWarning(ctx); warning != "" {
		snap.StateWarnings = append(snap.StateWarnings, warning)
	}

	if msgs, msgErr := d.store.GetInbox(ctx, roleForQuery(role), true, ""); msgErr == nil {
		snap.InboxCount = len(msgs)
	} else {
		snap.StateWarnings = append(snap.StateWarnings, fmt.Sprintf("inbox unavailable: %v", msgErr))
	}

	if agentForQuery(agent) != "" {
		for _, status := range []string{"in_progress", "ready_for_live"} {
			tasks, listErr := d.store.ListTasks(ctx, status)
			if listErr != nil {
				snap.StateWarnings = append(snap.StateWarnings, fmt.Sprintf("%s tasks unavailable: %v", status, listErr))
				continue
			}
			for _, t := range tasks {
				if t.Owner != agentForQuery(agent) {
					continue
				}
				if t.Status == "ready_for_live" {
					snap.ReadyForLive = append(snap.ReadyForLive, t)
				} else {
					snap.ActiveTasks = append(snap.ActiveTasks, t)
				}
			}
		}
	}

	if snap.ReviewerRole {
		if tasks, listErr := d.store.ListTasks(ctx, "ready_for_live"); listErr == nil {
			snap.ReadyForLive = mergeTaskSets(snap.ReadyForLive, tasks)
		} else {
			snap.StateWarnings = append(snap.StateWarnings, fmt.Sprintf("ready_for_live tasks unavailable: %v", listErr))
		}
	}

	pending, pendingErr := d.store.ListTasks(ctx, "pending")
	done, doneErr := d.store.ListTasks(ctx, "done")
	if pendingErr == nil && doneErr == nil {
		snap.ReadyTasks = readyTaskCandidates(pending, done)
	} else {
		if pendingErr != nil {
			snap.StateWarnings = append(snap.StateWarnings, fmt.Sprintf("pending tasks unavailable: %v", pendingErr))
		}
		if doneErr != nil {
			snap.StateWarnings = append(snap.StateWarnings, fmt.Sprintf("done tasks unavailable: %v", doneErr))
		}
	}

	renderNext(cmd.OutOrStdout(), snap)
	return nil
}

func resolveNextValue(flagValue, envName, fallback string) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		return v
	}
	return fallback
}

func roleForQuery(role string) string {
	if strings.HasPrefix(role, "$") {
		return ""
	}
	return role
}

func agentForQuery(agent string) string {
	if strings.HasPrefix(agent, "$") {
		return ""
	}
	return agent
}

func readyTaskCandidates(pending, done []store.Task) []store.Task {
	doneSet := make(map[string]bool, len(done))
	for _, t := range done {
		doneSet[t.ID] = true
	}
	ready := make([]store.Task, 0, len(pending))
	for _, t := range pending {
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
	sortTasksForNext(ready)
	return ready
}

func mergeTaskSets(first, second []store.Task) []store.Task {
	seen := make(map[string]bool, len(first)+len(second))
	out := make([]store.Task, 0, len(first)+len(second))
	for _, list := range [][]store.Task{first, second} {
		for _, t := range list {
			if seen[t.ID] {
				continue
			}
			seen[t.ID] = true
			out = append(out, t)
		}
	}
	sortTasksForNext(out)
	return out
}

func sortTasksForNext(tasks []store.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		pi, pj := priorityRank(tasks[i].Priority), priorityRank(tasks[j].Priority)
		if pi != pj {
			return pi < pj
		}
		ni, _ := store.ParseTaskID(tasks[i].ID)
		nj, _ := store.ParseTaskID(tasks[j].ID)
		return ni < nj
	})
}

func priorityRank(p string) int {
	switch strings.ToLower(p) {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	default:
		return 3
	}
}

func renderNext(w io.Writer, snap nextSnapshot) {
	fmt.Fprintf(w, "You are %s: %s\n\n", snap.Role, snap.Agent)
	if snap.StoreDegraded {
		fmt.Fprintf(w, "State lookup degraded: %s.\n", snap.StoreDegradeIs)
		fmt.Fprintln(w, "Recommended safe checks:")
		fmt.Fprintln(w, "1. gg doctor")
		fmt.Fprintln(w, "2. Restore Qdrant if doctor reports store connectivity issues")
		fmt.Fprintln(w, "3. gg reconcile")
		fmt.Fprintf(w, "4. gg inbox --role %s --peek\n", snap.Role)
		return
	}

	for _, warning := range snap.StateWarnings {
		fmt.Fprintf(w, "warning: %s\n", warning)
	}
	if len(snap.StateWarnings) > 0 {
		fmt.Fprintln(w)
	}
	if snap.InboxCount > 0 {
		fmt.Fprintf(w, "Inbox has %d unread message(s). Read it before choosing work:\n", snap.InboxCount)
		fmt.Fprintf(w, "  gg inbox --role %s --peek\n", snap.Role)
		return
	}

	if len(snap.ActiveTasks) > 0 {
		t := snap.ActiveTasks[0]
		fmt.Fprintf(w, "Active task: %s — %s\n", t.ID, t.Title)
		if t.LeaseUntil != "" {
			fmt.Fprintf(w, "Lease until: %s\n", t.LeaseUntil)
		}
		fmt.Fprintln(w, "Next:")
		fmt.Fprintf(w, "  gg task get %s\n", t.ID)
		fmt.Fprintf(w, "  gg context --for-task %s\n", t.ID)
		return
	}

	if len(snap.ReadyForLive) > 0 {
		t := snap.ReadyForLive[0]
		fmt.Fprintf(w, "Task is ready_for_live: %s — %s\n", t.ID, t.Title)
		if t.ReadyForLivePlan != "" {
			fmt.Fprintf(w, "Verify plan: %s\n", t.ReadyForLivePlan)
		}
		if !snap.ReviewerRole {
			fmt.Fprintln(w, "Next:")
			fmt.Fprintf(w, "  gg task get %s\n", t.ID)
			fmt.Fprintf(w, "  gg task packet %s\n", t.ID)
			fmt.Fprintf(w, "  gg tell reviewer \"%s ready for review: include evidence packet (commands, live smoke, impact, gaps, artifacts)\" --from %s --task %s\n", t.ID, snap.Role, t.ID)
			return
		}
		fmt.Fprintln(w, "Reviewer should run:")
		fmt.Fprintf(w, "  gg task get %s\n", t.ID)
		fmt.Fprintf(w, "  gg task packet %s\n", t.ID)
		fmt.Fprintf(w, "  gg task review %s --approve --by %s\n", t.ID, reviewerName(snap))
		fmt.Fprintf(w, "  gg task done %s \"verified: tests + live smoke\" --verifier %s\n", t.ID, reviewerName(snap))
		return
	}

	fmt.Fprintln(w, "Recommended next step:")
	fmt.Fprintln(w, "1. Read inbox:")
	fmt.Fprintf(w, "   gg inbox --role %s --peek\n", snap.Role)
	if snap.InboxCount > 0 {
		fmt.Fprintf(w, "   (%d unread message(s))\n", snap.InboxCount)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "2. Ready tasks:")
	fmt.Fprintln(w, "   gg task list --ready --compact")
	for i, t := range snap.ReadyTasks {
		if i >= 5 {
			fmt.Fprintf(w, "   … %d more\n", len(snap.ReadyTasks)-i)
			break
		}
		fmt.Fprintf(w, "   %s [%s] %s\n", t.ID, t.Priority, t.Title)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "3. To claim:")
	fmt.Fprintf(w, "   gg task start TASK-ID --owner %s --lease 30m\n", snap.Agent)
}

func reviewerName(snap nextSnapshot) string {
	if !strings.HasPrefix(snap.Agent, "$") {
		return snap.Agent
	}
	if !strings.HasPrefix(snap.Role, "$") {
		return snap.Role
	}
	return "reviewer-1"
}
