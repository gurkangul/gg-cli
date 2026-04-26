package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
)

var spawnHeartbeatFromWorker bool
var spawnHeartbeatWatch bool
var spawnHeartbeatPollSecs int
var spawnHeartbeatKeepaliveSecs int

// defaultKeepaliveSecs returns the effective keepalive interval in seconds.
// Priority: --keepalive flag > GG_PANE_KEEPALIVE_SEC env > 240.
func defaultKeepaliveSecs(flagVal int) int {
	if flagVal > 0 {
		return flagVal
	}
	if v := os.Getenv("GG_PANE_KEEPALIVE_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 240
}

var spawnHeartbeatCmd = &cobra.Command{
	Use:   "heartbeat",
	Short: "Record master session liveness",
	Long: `Write a heartbeat timestamp to the runtime dir so worker sessions can verify
the master is still alive before closing tasks.

Call this once to register liveness and check registered worker panes. For a
persistent master session, run with --watch from the master terminal; this keeps
refreshing liveness and re-checking worker panes until interrupted. Hook-driven
worker pings should keep the default one-shot mode.

The worker-liveness-check hook installed by 'gg doctor --install-task-hooks'
reads this file and blocks 'gg task done' when the master heartbeat is stale
(> 5 min old). Set GG_NO_MASTER_GUARD=1 in worker sessions to bypass the
liveness check.

The 46-worker-heartbeat.sh hook calls this with --worker to ping the master
from a worker pane at task-completion boundaries (best-effort).`,
	RunE: runSpawnHeartbeat,
}

func init() {
	spawnHeartbeatCmd.Flags().BoolVar(&spawnHeartbeatFromWorker, "worker", false, "ping originates from a worker pane (informational)")
	spawnHeartbeatCmd.Flags().BoolVar(&spawnHeartbeatWatch, "watch", false, "keep refreshing heartbeat and checking registered worker panes until interrupted")
	spawnHeartbeatCmd.Flags().IntVar(&spawnHeartbeatPollSecs, "poll", 60, "seconds between checks when --watch is set")
	spawnHeartbeatCmd.Flags().IntVar(&spawnHeartbeatKeepaliveSecs, "keepalive", 0, "seconds between noop keepalive sends to worker panes (default 240, or GG_PANE_KEEPALIVE_SEC)")
	spawnCmd.AddCommand(spawnHeartbeatCmd)
}

func runSpawnHeartbeat(cmd *cobra.Command, _ []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	agent := os.Getenv("GG_AGENT")
	if agent == "" {
		agent = os.Getenv("GG_ROLE")
	}

	source := "master"
	if spawnHeartbeatFromWorker {
		source = "worker"
	}

	if spawnHeartbeatWatch && spawnHeartbeatFromWorker {
		return fmt.Errorf("--watch cannot be combined with --worker")
	}

	if !spawnHeartbeatWatch {
		hb, summary, err := recordHeartbeatAndCheckWorkers(cmd.Context(), rt, agent, !spawnHeartbeatFromWorker)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{
			"status":     "ok",
			"agent":      agent,
			"source":     source,
			"updated_at": hb.UpdatedAt,
			"workers":    summary,
		}, func() {
			fmt.Printf("✓ Heartbeat recorded (agent: %s, source: %s)\n", agent, source)
			if !spawnHeartbeatFromWorker {
				printWorkerCheckSummary(summary)
			}
		})
	}

	poll := time.Duration(spawnHeartbeatPollSecs) * time.Second
	if poll <= 0 {
		return fmt.Errorf("--poll must be greater than 0")
	}

	keepaliveSecs := defaultKeepaliveSecs(spawnHeartbeatKeepaliveSecs)
	keepalive := time.Duration(keepaliveSecs) * time.Second

	term, termErr := terminal.NewFromEnv()
	if termErr != nil {
		// Non-fatal: watch still runs but keepalive/prune are disabled.
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ terminal backend unavailable — keepalive/prune disabled: %v\n", termErr)
		term = nil
	}

	fmt.Printf("→ Heartbeat watch started (agent: %s, poll: %s, keepalive: %s)\n", agent, poll, keepalive)

	lastKeepalive := time.Now()

	for {
		hb, summary, err := recordHeartbeatAndCheckWorkers(cmd.Context(), rt, agent, true)
		if err != nil {
			return err
		}
		fmt.Printf("✓ %s heartbeat refreshed", hb.UpdatedAt.Format(time.RFC3339))
		printWorkerCheckSummary(summary)

		if term != nil {
			pollAdvanceSentinels(cmd.Context(), rt, term)

			if time.Since(lastKeepalive) >= keepalive {
				sendPaneKeepalives(cmd.Context(), rt, term, cmd.ErrOrStderr())
				lastKeepalive = time.Now()
			}
		}

		select {
		case <-cmd.Context().Done():
			fmt.Println("Heartbeat watch stopped.")
			return nil
		case <-time.After(poll):
		}
	}
}

type heartbeatWorkerSummary struct {
	Total   int `json:"total"`
	Working int `json:"working"`
	Idle    int `json:"idle"`
	Missing int `json:"missing"`
}

func recordHeartbeatAndCheckWorkers(ctx context.Context, rt, agent string, checkWorkers bool) (*spawn.Heartbeat, heartbeatWorkerSummary, error) {
	if err := spawn.WriteHeartbeat(rt, agent); err != nil {
		return nil, heartbeatWorkerSummary{}, fmt.Errorf("write heartbeat: %w", err)
	}
	hb, _ := spawn.ReadHeartbeat(rt)
	if !checkWorkers {
		return hb, heartbeatWorkerSummary{}, nil
	}
	summary, err := checkWorkerPanes(ctx, rt)
	if err != nil {
		return nil, summary, err
	}
	return hb, summary, nil
}

func checkWorkerPanes(ctx context.Context, rt string) (heartbeatWorkerSummary, error) {
	panes, err := spawn.ListPanes(rt)
	if err != nil {
		return heartbeatWorkerSummary{}, fmt.Errorf("read worker panes: %w", err)
	}
	if len(panes) == 0 {
		return heartbeatWorkerSummary{}, nil
	}

	term, err := terminal.NewFromEnv()
	if err != nil {
		return heartbeatWorkerSummary{}, fmt.Errorf("terminal backend: %w", err)
	}
	return checkWorkerPanesWithTerminal(ctx, rt, term, panes)
}

func checkWorkerPanesWithTerminal(ctx context.Context, rt string, term terminal.Terminal, panes []spawn.WorkerPane) (heartbeatWorkerSummary, error) {
	summary := heartbeatWorkerSummary{Total: len(panes)}
	for _, pane := range panes {
		id := terminal.SurfaceID(pane.SurfaceID)
		if term.Capabilities().CanReadScreen {
			content, readErr := term.ReadScreen(ctx, id)
			if readErr != nil {
				pruneStalePane(rt, pane)
				summary.Missing++
				continue
			}
			_ = spawn.UpdateWorkerHeartbeat(rt, pane.TaskID)
			if terminal.IsAgentIdle(content) {
				_ = spawn.UpdateWorkerState(rt, pane.TaskID, spawn.WorkerStateIdle)
				summary.Idle++
				continue
			}
			_ = spawn.UpdateWorkerState(rt, pane.TaskID, spawn.WorkerStateWorking)
			summary.Working++
			continue
		}
		if focusErr := term.Focus(ctx, id); focusErr != nil {
			pruneStalePane(rt, pane)
			summary.Missing++
			continue
		}
		_ = spawn.UpdateWorkerHeartbeat(rt, pane.TaskID)
		_ = spawn.UpdateWorkerState(rt, pane.TaskID, spawn.WorkerStateWorking)
		summary.Working++
	}
	return summary, nil
}

// pruneStalePane removes a dead pane entry from panes.json and its lock file,
// logging a message so the master knows to re-spawn if needed.
func pruneStalePane(rt string, pane spawn.WorkerPane) {
	fmt.Fprintf(os.Stderr, "⚠ pruned stale pane %s for %s — was this an unsupervised death? consider increasing keepalive\n",
		pane.SurfaceID, pane.TaskID)
	_ = spawn.RemovePane(rt, pane.TaskID)
	// Best-effort lock file removal — ignore errors.
	lockPath := spawn.LockFilePath(rt, pane.TaskID)
	if lockPath != "" {
		_ = os.Remove(lockPath)
	}
}

// pollAdvanceSentinels checks whether any registered worker pane has written an
// advance sentinel. On detection: prints a ready signal, updates pane state to
// ready, and renames the sentinel to .consumed (atomic, prevents double-fire).
func pollAdvanceSentinels(ctx context.Context, rt string, term terminal.Terminal) {
	panes, err := spawn.ListPanes(rt)
	if err != nil || len(panes) == 0 {
		return
	}
	for _, pane := range panes {
		// Skip panes already marked ready — sentinel already consumed.
		if pane.State == spawn.WorkerStateReady {
			continue
		}
		s, err := spawn.ConsumeAdvanceSentinel(rt, pane.TaskID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠ advance sentinel consume for %s: %v\n", pane.TaskID, err)
			continue
		}
		if s == nil {
			continue
		}
		// Sentinel consumed — signal master.
		commitRef := s.CommitSHA
		if commitRef == "" {
			commitRef = "(no sha)"
		}
		surfRef := pane.SurfaceID
		if s.SurfaceID != "" {
			surfRef = s.SurfaceID
		}
		fmt.Fprintf(os.Stderr, "⚡ worker ready: %s at %s on %s\n", pane.TaskID, commitRef, surfRef)
		_ = spawn.UpdateWorkerState(rt, pane.TaskID, spawn.WorkerStateReady)
	}
}

// sendPaneKeepalives sends a noop newline to every registered active worker pane
// to prevent cmux idle-timeout from culling panes that are waiting for master review.
func sendPaneKeepalives(ctx context.Context, rt string, term terminal.Terminal, errOut interface{ Write([]byte) (int, error) }) {
	panes, err := spawn.ListPanes(rt)
	if err != nil || len(panes) == 0 {
		return
	}
	for _, pane := range panes {
		if pane.State == spawn.WorkerStateReady {
			// Pane is awaiting review — still alive, still needs keepalive.
		}
		id := terminal.SurfaceID(pane.SurfaceID)
		// Send an empty line — no observable side effect beyond resetting idle timer.
		if err := term.SendKey(ctx, id, ""); err != nil {
			fmt.Fprintf(os.Stderr, "⚠ keepalive send to pane %s (%s): %v\n", pane.SurfaceID, pane.TaskID, err)
		}
	}
}

func printWorkerCheckSummary(summary heartbeatWorkerSummary) {
	if summary.Total == 0 {
		fmt.Print("  workers: none\n")
		return
	}
	fmt.Printf("  workers checked: %d working, %d idle, %d missing (total %d)\n",
		summary.Working, summary.Idle, summary.Missing, summary.Total)
}
