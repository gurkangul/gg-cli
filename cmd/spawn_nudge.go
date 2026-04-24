package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
)

var spawnNudgeCmd = &cobra.Command{
	Use:   "nudge --surface <id> <text>",
	Short: "Wake an idle worker pane and deliver a prompt",
	Long: `Send a prompt to a worker pane, ensuring a new agent turn starts.

When the target pane has finished its current turn (agent REPL is idle),
a bare Enter is sent first to wake the stdin handler before the real text
is delivered.  When the pane is mid-turn (agent is actively working), the
prompt is sent directly and queued as a Steering message — no observable
difference for the running agent.

This is the canonical dual-write complement to 'gg tell': use 'gg tell'
to record the message in the knowledge base, then 'gg spawn nudge' to
actually trigger the worker.

Example:
  gg spawn nudge --surface surface:46 "Fix the gap in TASK-292: missing error wrap"`,
	Args: cobra.ExactArgs(1),
	RunE: runSpawnNudge,
}

var spawnNudgeSurface string

func init() {
	spawnNudgeCmd.Flags().StringVar(&spawnNudgeSurface, "surface", "", "target pane surface ID (required)")
	_ = spawnNudgeCmd.MarkFlagRequired("surface")
	spawnCmd.AddCommand(spawnNudgeCmd)
}

func runSpawnNudge(cmd *cobra.Command, args []string) error {
	text := strings.TrimSpace(args[0])
	if text == "" {
		return fmt.Errorf("prompt text must not be empty")
	}

	term, err := terminal.NewFromEnv()
	if err != nil {
		return fmt.Errorf("terminal backend: %w", err)
	}

	// Resolve spawn dir for cross-process file flock.  On error we fall back
	// to the in-process-only path so that nudge still works without a config.
	var spawnDir string
	if rt, rtErr := spawnRuntimeDir(); rtErr == nil {
		spawnDir = spawn.Dir(rt)
	}

	id := terminal.SurfaceID(spawnNudgeSurface)
	if err := terminal.WakeAndSendWithFlock(cmd.Context(), term, id, text, spawnDir); err != nil {
		return fmt.Errorf("nudge %s: %w", id, err)
	}

	fmt.Printf("✓ Prompt delivered to %s\n", id)
	return nil
}
