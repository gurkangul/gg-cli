package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
)

var spawnStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show spawn session status: heartbeat age, active workers, queue progress",
	RunE:  runSpawnStatus,
}

func init() {
	spawnCmd.AddCommand(spawnStatusCmd)
}

func runSpawnStatus(_ *cobra.Command, _ []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	// Heartbeat status.
	hb, hbErr := spawn.ReadHeartbeat(rt)
	alive, aliveReason := spawn.IsMasterAlive(rt)

	// Queue session.
	sess, sessErr := spawn.ReadQueue(rt)

	// Worker panes.
	workers, workersErr := spawn.ListPanes(rt)

	return printJSON(buildSpawnStatusJSON(hb, alive, sess, workers), func() {
		// --- Heartbeat ---
		fmt.Println("── Master Liveness ──")
		switch {
		case errors.Is(hbErr, spawn.ErrNoHeartbeat):
			fmt.Println("  No heartbeat recorded. Run 'gg spawn heartbeat' from the master session.")
		case hbErr != nil:
			fmt.Printf("  heartbeat error: %v\n", hbErr)
		default:
			age := time.Since(hb.UpdatedAt).Round(time.Second)
			icon := "✓"
			if !alive {
				icon = "✗"
			}
			fmt.Printf("  %s last seen %s ago (agent: %s)\n", icon, age, hb.Agent)
			if !alive {
				fmt.Printf("    reason: %s\n", aliveReason)
			}
		}

		// --- Queue session ---
		fmt.Println("── Queue Session ──")
		switch {
		case errors.Is(sessErr, spawn.ErrNoQueue):
			fmt.Println("  No active queue session. Run 'gg spawn queue start' to start one.")
		case sessErr != nil:
			fmt.Printf("  session error: %v\n", sessErr)
		default:
			dur := time.Since(sess.StartedAt).Round(time.Second)
			state := "Running"
			stateSuffix := ""
			if sess.Done {
				state = "Complete"
				if !sess.CompletedAt.IsZero() {
					dur = sess.CompletedAt.Sub(sess.StartedAt).Round(time.Second)
				}
			} else if sess.Paused {
				stateSuffix = " [PAUSED]"
			}
			fmt.Printf("  Agent: %s  %s: %s%s\n", sess.Agent, state, dur, stateSuffix)
			if sess.CurrentTask != "" {
				fmt.Printf("  Current task: %s\n", sess.CurrentTask)
			}
			fmt.Printf("  Completed: %d  Skipped: %d\n", len(sess.Completed), len(sess.Skipped))
		}

		// --- Workers ---
		fmt.Println("── Active Workers ──")
		switch {
		case workersErr != nil:
			fmt.Printf("  error: %v\n", workersErr)
		case len(workers) == 0:
			fmt.Println("  (none)")
		default:
			for _, w := range workers {
				age := time.Since(w.SpawnedAt).Round(time.Second)
				fmt.Printf("  ● %s  task: %s  pane: %s  age: %s\n",
					w.Agent, w.TaskID, w.SurfaceID, age)
				if w.PromptDeliveryStatus == "skipped" || w.PromptDeliveryStatus == "failed" {
					fmt.Printf("    prompt delivery: %s — %s\n", w.PromptDeliveryStatus, w.PromptDeliveryError)
				}
			}
		}
	})
}

func buildSpawnStatusJSON(hb *spawn.Heartbeat, alive bool, sess *spawn.QueueSession, workers []spawn.WorkerPane) map[string]any {
	out := map[string]any{
		"master_alive": alive,
	}
	if hb != nil {
		out["heartbeat"] = hb
	}
	if sess != nil {
		out["session"] = sess
	}
	out["workers"] = workers
	return out
}
