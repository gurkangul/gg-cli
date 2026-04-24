package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

	// 7. panes.json raw content — read independently so the master always sees
	// the exact file bytes (pane → task mapping), matching the CLAUDE.md protocol
	// which specifies `cat ~/.gg/projects/<id>/spawn/panes.json`.
	panesPath := filepath.Join(spawn.Dir(rt), spawn.PanesFile)
	panesRaw, panesRawErr := os.ReadFile(panesPath)

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
			qdrantNote = "(Qdrant slow — tasks/inbox/decisions unavailable)"
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

			// Inbox: include agent messages, peek (no mark-as-read). Cap at 10.
			allMessages, _ := d.store.GetInbox(ctx, "", false)
			if len(allMessages) > 10 {
				messages = allMessages[:10]
			} else {
				messages = allMessages
			}

			// Recent decisions — newest 10.
			decisions, _ = d.store.ListDecisions(ctx, 10)
		}
	} else {
		qdrantNote = fmt.Sprintf("(store unavailable: %v)", depsErr)
	}

	return printJSON(buildMasterResumeJSON(
		gitLines, hb, alive, sess, workers,
		pendingTasks, readyTasks, messages, decisions,
		panesRaw,
	), func() {
		w := cmd.OutOrStdout()
		printMasterResume(w,
			gitLines, hb, hbErr, alive, aliveReason,
			sess, sessErr, workers, workersErr,
			pendingTasks, readyTasks, messages, decisions,
			qdrantNote,
			panesPath, panesRaw, panesRawErr,
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
	w io.Writer,
	gitLines []string,
	hb *spawn.Heartbeat, hbErr error, alive bool, aliveReason string,
	sess *spawn.QueueSession, sessErr error,
	workers []spawn.WorkerPane, workersErr error,
	pendingTasks, readyTasks []store.Task,
	messages []store.Message,
	decisions []store.Decision,
	qdrantNote string,
	panesPath string, panesRaw []byte, panesRawErr error,
) {
	sep := func(title string) { fmt.Fprintf(w, "\n══ %s ══\n", title) }

	// 1. Git log
	sep("Recent Commits (git log --oneline -10)")
	if len(gitLines) == 0 {
		fmt.Fprintln(w, "  (unavailable)")
	} else {
		for _, l := range gitLines {
			fmt.Fprintf(w, "  %s\n", l)
		}
	}

	// 2. Master liveness
	sep("Master Liveness")
	if hbErr != nil {
		if errors.Is(hbErr, spawn.ErrNoHeartbeat) {
			fmt.Fprintln(w, "  No heartbeat — master has not run `gg spawn heartbeat`.")
		} else {
			fmt.Fprintf(w, "  heartbeat error: %v\n", hbErr)
		}
	} else if hb != nil {
		icon := "✓"
		if !alive {
			icon = "✗"
		}
		age := time.Since(hb.UpdatedAt).Round(time.Second)
		fmt.Fprintf(w, "  %s last seen %s ago (agent: %s)\n", icon, age, hb.Agent)
		if !alive {
			fmt.Fprintf(w, "    reason: %s\n", aliveReason)
		}
	}

	// Queue session
	sep("Queue Session")
	if sessErr != nil {
		if errors.Is(sessErr, spawn.ErrNoQueue) {
			fmt.Fprintln(w, "  No active queue session.")
		} else {
			fmt.Fprintf(w, "  error: %v\n", sessErr)
		}
	} else if sess != nil {
		dur := time.Since(sess.StartedAt).Round(time.Second)
		paused := ""
		if sess.Paused {
			paused = " [PAUSED]"
		}
		fmt.Fprintf(w, "  Agent: %s  Running: %s%s\n", sess.Agent, dur, paused)
		if sess.CurrentTask != "" {
			fmt.Fprintf(w, "  Current task: %s\n", sess.CurrentTask)
		}
		fmt.Fprintf(w, "  Completed: %d  Skipped: %d\n", len(sess.Completed), len(sess.Skipped))
		if len(sess.Completed) > 0 {
			fmt.Fprintf(w, "  Completed IDs: %s\n", strings.Join(sess.Completed, ", "))
		}
	}

	// Active worker panes
	sep("Active Worker Panes")
	if workersErr != nil {
		fmt.Fprintf(w, "  error: %v\n", workersErr)
	} else if len(workers) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		for _, wp := range workers {
			age := time.Since(wp.SpawnedAt).Round(time.Second)
			state := string(wp.State)
			if state == "" {
				state = "working"
			}
			fmt.Fprintf(w, "  ● %s  task: %s  pane: %s  age: %s  state: %s\n",
				wp.Agent, wp.TaskID, wp.SurfaceID, age, state)
		}
	}

	if qdrantNote != "" {
		sep("Qdrant State")
		fmt.Fprintf(w, "  %s\n", qdrantNote)
		return
	}

	// 3. Pending tasks
	sep(fmt.Sprintf("Pending Tasks (up to %d)", masterResumeMaxPendingTasks))
	if len(pendingTasks) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		for _, t := range pendingTasks {
			fmt.Fprintf(w, "  %s %s [%s] %s\n", statusIcon(t.Status), t.ID, t.Priority, t.Title)
		}
	}

	// 4. Ready-for-live tasks
	sep("Ready-for-Live Tasks (awaiting master review)")
	if len(readyTasks) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		for _, t := range readyTasks {
			plan := ""
			if t.ReadyForLivePlan != "" {
				plan = " — " + t.ReadyForLivePlan
			}
			fmt.Fprintf(w, "  ◉ %s [%s] %s%s\n", t.ID, t.Priority, t.Title, plan)
		}
	}

	// 5. Inbox (including agent messages, peek, top 10)
	sep("Unread Inbox (all audiences, peek, top 10)")
	if len(messages) == 0 {
		fmt.Fprintln(w, "  (empty)")
	} else {
		for _, m := range messages {
			audience := ""
			if m.Audience == "agents" {
				audience = " [agent]"
			}
			fmt.Fprintf(w, "  [%s → %s]%s %s\n", m.FromRole, m.ToRole, audience, compactTrim(m.Content, 100))
		}
	}

	// 6. Recent decisions
	sep("Recent Decisions (latest 10)")
	if len(decisions) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		for _, dec := range decisions {
			tags := ""
			if len(dec.Tags) > 0 {
				tags = " [" + strings.Join(dec.Tags, ",") + "]"
			}
			fmt.Fprintf(w, "  D  %s  %s%s\n", shortDate(dec.CreatedAt), compactTrim(dec.Text, 80), tags)
		}
	}

	// 7. panes.json raw — exact file content for pane→task mapping (mirrors the
	// `cat ~/.gg/projects/<id>/spawn/panes.json` step in the CLAUDE.md protocol).
	sep(fmt.Sprintf("panes.json raw (%s)", panesPath))
	switch {
	case panesRawErr != nil && os.IsNotExist(panesRawErr):
		fmt.Fprintln(w, "  (file absent — no active worker panes registered)")
	case panesRawErr != nil:
		fmt.Fprintf(w, "  error reading panes.json: %v\n", panesRawErr)
	default:
		fmt.Fprintf(w, "%s\n", panesRaw)
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
	panesRaw []byte,
) map[string]any {
	// Normalise nil slices to empty arrays so the JSON output is stable
	// (null vs [] matters to callers parsing --json output).
	if gitLines == nil {
		gitLines = []string{}
	}
	if workers == nil {
		workers = []spawn.WorkerPane{}
	}
	if pendingTasks == nil {
		pendingTasks = []store.Task{}
	}
	if readyTasks == nil {
		readyTasks = []store.Task{}
	}
	if messages == nil {
		messages = []store.Message{}
	}
	if decisions == nil {
		decisions = []store.Decision{}
	}
	// panes_json_raw: include raw bytes as a JSON string so callers get exactly
	// what `cat panes.json` would produce. Nil (file absent) becomes "".
	panesRawStr := ""
	if panesRaw != nil {
		panesRawStr = string(panesRaw)
	}
	out := map[string]any{
		"git_log":          gitLines,
		"master_alive":     alive,
		"workers":          workers,
		"pending_tasks":    pendingTasks,
		"ready_for_live":   readyTasks,
		"inbox_messages":   messages,
		"recent_decisions": decisions,
		"panes_json_raw":   panesRawStr,
	}
	if hb != nil {
		out["heartbeat"] = hb
	}
	if sess != nil {
		out["queue_session"] = sess
	}
	return out
}
