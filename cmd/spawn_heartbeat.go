package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
)

var spawnHeartbeatFromWorker bool
var spawnHeartbeatWatch bool
var spawnHeartbeatPollSecs int

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

	fmt.Printf("→ Heartbeat watch started (agent: %s, poll: %s)\n", agent, poll)
	for {
		hb, summary, err := recordHeartbeatAndCheckWorkers(cmd.Context(), rt, agent, true)
		if err != nil {
			return err
		}
		fmt.Printf("✓ %s heartbeat refreshed", hb.UpdatedAt.Format(time.RFC3339))
		printWorkerCheckSummary(summary)

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
			summary.Missing++
			continue
		}
		_ = spawn.UpdateWorkerHeartbeat(rt, pane.TaskID)
		_ = spawn.UpdateWorkerState(rt, pane.TaskID, spawn.WorkerStateWorking)
		summary.Working++
	}
	return summary, nil
}

func printWorkerCheckSummary(summary heartbeatWorkerSummary) {
	if summary.Total == 0 {
		fmt.Print("  workers: none\n")
		return
	}
	fmt.Printf("  workers checked: %d working, %d idle, %d missing (total %d)\n",
		summary.Working, summary.Idle, summary.Missing, summary.Total)
}
