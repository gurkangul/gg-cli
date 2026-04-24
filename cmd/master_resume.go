package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/store"
)

const masterResumeMaxPendingTasks = 20

var masterResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Print a structured session-handoff dump for a fresh master session",
	Long: `Produce a one-shot snapshot of all gg state a fresh Opus master session needs
to resume without asking the user to re-explain. Runs the 7-source pipeline from
the master-resume protocol documented in CLAUDE.md:

  1. git log --oneline -10                     (recent commits)
  2. spawn state: heartbeat + queue + panes    (local, no Qdrant)
  3. pending tasks (up to 20)                  (Qdrant)
  4. ready_for_live tasks                      (Qdrant)
  5. unread inbox (--include-agents)           (Qdrant, peek — no mark-as-read)
  6. recent decisions (compact)                (Qdrant)
  7. panes.json raw                            (local)

Outputs plain text sections. Qdrant down → local sections still print.
Combine with --json for machine-readable output.`,
	RunE: runMasterResume,
}

func init() {
	masterCmd.AddCommand(masterResumeCmd)
}

func runMasterResume(cmd *cobra.Command, _ []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	// 1. Recent git log — best-effort, non-fatal.
	gitLines := recentGitLog()

	// 2. Spawn state — local files, no Qdrant.
	hb, hbErr := spawn.ReadHeartbeat(rt)
	alive, aliveReason := spawn.IsMasterAlive(rt)
	sess, sessErr := spawn.ReadQueue(rt)
	workers, workersErr := spawn.ListPanes(rt)

	// 3-6. Qdrant-backed state — tolerate Qdrant down.
	d, depsErr := loadDepsReadOnly(false)
	var pendingTasks, readyTasks []store.Task
	var messages []store.Message
	var decisions []store.Decision
	var qdrantNote string
	if depsErr == nil {
		defer d.Close()
		if d.qdrantDown {
			qdrantNote = "(Qdrant unreachable — tasks/inbox/decisions unavailable)"
		} else if d.qdrantSlow {
			qdrantNote = "(Qdrant slow — results may be incomplete)"
		} else {
			ctx, cancel := withTimeout(cmd.Context())
			defer cancel()

			allPending, tErr := d.store.ListTasks(ctx, "pending")
			if tErr == nil {
				if len(allPending) > masterResumeMaxPendingTasks {
					pendingTasks = allPending[:masterResumeMaxPendingTasks]
				} else {
					pendingTasks = allPending
				}
			}

			readyTasks, _ = d.store.ListTasks(ctx, "ready_for_live")

			// Inbox: include agent messages, peek (no mark-as-read).
			messages, _ = d.store.GetInbox(ctx, "", false)

			// Recent decisions — newest 10.
			decisions, _ = d.store.ListDecisions(ctx, 10)
		}
	} else {
		qdrantNote = fmt.Sprintf("(store unavailable: %v)", depsErr)
	}

	return printJSON(buildMasterResumeJSON(
		gitLines, hb, alive, sess, workers,
		pendingTasks, readyTasks, messages, decisions,
	), func() {
		printMasterResume(
			gitLines, hb, hbErr, alive, aliveReason,
			sess, sessErr, workers, workersErr,
			pendingTasks, readyTasks, messages, decisions,
			qdrantNote,
		)
	})
}

// recentGitLog returns the last 10 git log --oneline lines. Non-fatal on error.
func recentGitLog() []string {
	out, err := exec.Command("git", "log", "--oneline", "-10").Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	var result []string
	for _, l := range lines {
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

func printMasterResume(
	gitLines []string,
	hb *spawn.Heartbeat, hbErr error, alive bool, aliveReason string,
	sess *spawn.QueueSession, sessErr error,
	workers []spawn.WorkerPane, workersErr error,
	pendingTasks, readyTasks []store.Task,
	messages []store.Message,
	decisions []store.Decision,
	qdrantNote string,
) {
	sep := func(title string) { fmt.Printf("\n══ %s ══\n", title) }

	// 1. Git log
	sep("Recent Commits (git log --oneline -10)")
	if len(gitLines) == 0 {
		fmt.Println("  (unavailable)")
	} else {
		for _, l := range gitLines {
			fmt.Printf("  %s\n", l)
		}
	}

	// 2. Master liveness
	sep("Master Liveness")
	if errors.Is(hbErr, spawn.ErrNoHeartbeat) {
		fmt.Println("  No heartbeat — master has not run `gg spawn heartbeat`.")
	} else if hbErr != nil {
		fmt.Printf("  heartbeat error: %v\n", hbErr)
	} else {
		icon := "✓"
		if !alive {
			icon = "✗"
		}
		age := time.Since(hb.UpdatedAt).Round(time.Second)
		fmt.Printf("  %s last seen %s ago (agent: %s)\n", icon, age, hb.Agent)
		if !alive {
			fmt.Printf("    reason: %s\n", aliveReason)
		}
	}

	// Queue session
	sep("Queue Session")
	if errors.Is(sessErr, spawn.ErrNoQueue) {
		fmt.Println("  No active queue session.")
	} else if sessErr != nil {
		fmt.Printf("  error: %v\n", sessErr)
	} else {
		dur := time.Since(sess.StartedAt).Round(time.Second)
		paused := ""
		if sess.Paused {
			paused = " [PAUSED]"
		}
		fmt.Printf("  Agent: %s  Running: %s%s\n", sess.Agent, dur, paused)
		if sess.CurrentTask != "" {
			fmt.Printf("  Current task: %s\n", sess.CurrentTask)
		}
		fmt.Printf("  Completed: %d  Skipped: %d\n", len(sess.Completed), len(sess.Skipped))
		if len(sess.Completed) > 0 {
			fmt.Printf("  Completed IDs: %s\n", strings.Join(sess.Completed, ", "))
		}
	}

	// Active worker panes
	sep("Active Worker Panes")
	if workersErr != nil {
		fmt.Printf("  error: %v\n", workersErr)
	} else if len(workers) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, w := range workers {
			age := time.Since(w.SpawnedAt).Round(time.Second)
			state := string(w.State)
			if state == "" {
				state = "working"
			}
			fmt.Printf("  ● %s  task: %s  pane: %s  age: %s  state: %s\n",
				w.Agent, w.TaskID, w.SurfaceID, age, state)
		}
	}

	if qdrantNote != "" {
		sep("Qdrant State")
		fmt.Printf("  %s\n", qdrantNote)
		return
	}

	// 3. Pending tasks
	sep(fmt.Sprintf("Pending Tasks (up to %d)", masterResumeMaxPendingTasks))
	if len(pendingTasks) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, t := range pendingTasks {
			fmt.Printf("  %s %s [%s] %s\n", statusIcon(t.Status), t.ID, t.Priority, t.Title)
		}
	}

	// 4. Ready-for-live tasks
	sep("Ready-for-Live Tasks (awaiting master review)")
	if len(readyTasks) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, t := range readyTasks {
			plan := ""
			if t.ReadyForLivePlan != "" {
				plan = " — " + t.ReadyForLivePlan
			}
			fmt.Printf("  ◉ %s [%s] %s%s\n", t.ID, t.Priority, t.Title, plan)
		}
	}

	// 5. Inbox (including agent messages, peek)
	sep("Unread Inbox (all audiences, peek)")
	if len(messages) == 0 {
		fmt.Println("  (empty)")
	} else {
		for _, m := range messages {
			audience := ""
			if m.Audience == "agents" {
				audience = " [agent]"
			}
			fmt.Printf("  [%s → %s]%s %s\n", m.FromRole, m.ToRole, audience, compactTrim(m.Content, 100))
		}
	}

	// 6. Recent decisions
	sep("Recent Decisions (latest 10)")
	if len(decisions) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, dec := range decisions {
			tags := ""
			if len(dec.Tags) > 0 {
				tags = " [" + strings.Join(dec.Tags, ",") + "]"
			}
			fmt.Printf("  D  %s  %s%s\n", dec.CreatedAt[:10], compactTrim(dec.Text, 80), tags)
		}
	}
}

// buildMasterResumeJSON constructs the JSON payload for --json mode.
func buildMasterResumeJSON(
	gitLines []string,
	hb *spawn.Heartbeat, alive bool,
	sess *spawn.QueueSession,
	workers []spawn.WorkerPane,
	pendingTasks, readyTasks []store.Task,
	messages []store.Message,
	decisions []store.Decision,
) map[string]any {
	out := map[string]any{
		"git_log":         gitLines,
		"master_alive":    alive,
		"workers":         workers,
		"pending_tasks":   pendingTasks,
		"ready_for_live":  readyTasks,
		"inbox_messages":  messages,
		"recent_decisions": decisions,
	}
	if hb != nil {
		out["heartbeat"] = hb
	}
	if sess != nil {
		out["queue_session"] = sess
	}
	return out
}
