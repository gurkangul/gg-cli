package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
)

var gsdGuardCmd = &cobra.Command{
	Use:    "gsd-guard",
	Hidden: true, // internal — called only by the Claude Code PreToolUse hook
	Short:  "Block gsd_plan_* calls when tracker.canonical=gg",
	Long: `Claude Code PreToolUse hook guard. Reads the tool-call JSON from stdin,
checks whether tracker.canonical is set to "gg" in .gg/config.yaml, and
exits non-zero to block the call when both conditions are true.

Exit codes:
  0  allow (canonical != gg, or tool not in the forbidden list)
  1  block (canonical == gg and tool is a forbidden gsd_plan_* call)`,
	RunE: runGSDGuard,
}

func init() {
	rootCmd.AddCommand(gsdGuardCmd)
}

// forbiddenGSDTools is the set of MCP tool names that create GSD milestone
// state. Substring-matched against the incoming tool_name so both
// "mcp__gsd-workflow__gsd_plan_milestone" and bare "gsd_plan_milestone" hit.
var forbiddenGSDTools = []string{
	"gsd_plan_milestone",
	"gsd_plan_slice",
	"gsd_plan_task",
}

func runGSDGuard(_ *cobra.Command, _ []string) error {
	// Load config — if .gg/ is not found or tracker.canonical != "gg", allow.
	if _, err := config.FindRoot(); err != nil {
		return nil // not a gg project, passthrough
	}
	cfg, err := config.Load()
	if err != nil || strings.ToLower(cfg.Tracker.Canonical) != "gg" {
		return nil // not canonical, passthrough
	}

	// Read the PreToolUse JSON payload from stdin.
	raw, _ := io.ReadAll(os.Stdin)
	var payload struct {
		ToolName string `json:"tool_name"`
	}
	_ = json.Unmarshal(raw, &payload)

	toolName := strings.ToLower(payload.ToolName)
	for _, forbidden := range forbiddenGSDTools {
		if strings.Contains(toolName, forbidden) {
			fmt.Fprintf(os.Stdout,
				"BLOCKED by gg tracker guard: this project uses gg as its canonical tracker.\n"+
					"Do not call %s — use gg instead:\n\n"+
					"  gg task create \"<title>\" --priority <high|medium|low> --detail \"<details>\"\n\n"+
					"Set tracker.canonical in .gg/config.yaml to change this behaviour.\n",
				payload.ToolName)
			os.Exit(1)
		}
	}
	return nil
}
