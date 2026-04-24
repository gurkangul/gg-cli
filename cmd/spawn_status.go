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
	sess, sessErr := spawn.ReadSession(rt)

	// Workers.
	workers, workersErr := spawn.ListWorkers(rt)

	return printJSON(buildSpawnStatusJSON(hb, alive, sess, workers), func() {
		// --- Heartbeat ---
		fmt.Println("── Master Liveness ──")
		if errors.Is(hbErr, spawn.ErrNoHeartbeat) {
			fmt.Println("  No heartbeat recorded. Run 'gg spawn heartbeat' from the master session.")
		} else if hbErr != nil {
			fmt.Printf("  heartbeat error: %v\n", hbErr)
		} else {
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
		if errors.Is(sessErr, spawn.ErrNoSession) {
			fmt.Println("  No active queue session. Run 'gg spawn queue' to start one.")
		} else if sessErr != nil {
			fmt.Printf("  session error: %v\n", sessErr)
		} else {
			dur := time.Since(sess.StartedAt).Round(time.Second)
			fmt.Printf("  Agent: %s  Running: %s\n", sess.Agent, dur)
			if sess.Current != "" {
				fmt.Printf("  Current task: %s\n", sess.Current)
			}
			fmt.Printf("  Completed: %d  Skipped: %d\n", len(sess.Completed), len(sess.Skipped))
		}

		// --- Workers ---
		fmt.Println("── Active Workers ──")
		if workersErr != nil {
			fmt.Printf("  error: %v\n", workersErr)
		} else if len(workers) == 0 {
			fmt.Println("  (none)")
		} else {
			for _, w := range workers {
				age := time.Since(w.SpawnedAt).Round(time.Second)
				fmt.Printf("  ● %s  task: %s  pane: %s  age: %s\n",
					w.Agent, w.TaskID, w.SurfaceID, age)
			}
		}
	})
}

func buildSpawnStatusJSON(hb *spawn.Heartbeat, alive bool, sess *spawn.QueueSession, workers []spawn.WorkerEntry) map[string]any {
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
