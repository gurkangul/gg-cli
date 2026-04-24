package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
)

var spawnHeartbeatCmd = &cobra.Command{
	Use:   "heartbeat",
	Short: "Record master session liveness",
	Long: `Write a heartbeat timestamp to the runtime dir so worker sessions can verify
the master is still alive before closing tasks.

Call this once to register liveness. For a persistent master session, wire it
into a loop or hook (e.g. a cron every 60s, or a pre-task-done hook).

The master-guard hook installed by 'gg doctor --install-task-hooks' reads this
file and blocks 'gg task done' when the master heartbeat is stale (> 5 min old).
Set GG_NO_MASTER_GUARD=1 in worker sessions to bypass the liveness check.`,
	RunE: runSpawnHeartbeat,
}

func init() {
	spawnCmd.AddCommand(spawnHeartbeatCmd)
}

func runSpawnHeartbeat(_ *cobra.Command, _ []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	agent := os.Getenv("GG_AGENT")
	if agent == "" {
		agent = os.Getenv("GG_ROLE")
	}

	if err := spawn.WriteHeartbeat(rt, agent); err != nil {
		return fmt.Errorf("write heartbeat: %w", err)
	}

	hb, _ := spawn.ReadHeartbeat(rt)
	return printJSON(map[string]any{
		"status":     "ok",
		"agent":      agent,
		"updated_at": hb.UpdatedAt,
	}, func() {
		fmt.Printf("✓ Heartbeat recorded (agent: %s)\n", agent)
	})
}
