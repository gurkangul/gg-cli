package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/session"
)

var sessionStartAgent string

var sessionStartCmd = &cobra.Command{
	Use:   "session-start",
	Short: "Print session bootstrap briefing (called by agent SessionStart hooks)",
	Long: `Print the session-start briefing for an AI agent entering this project.

This is the canonical entrypoint used by agent SessionStart hooks installed
via ` + "`gg doctor --install-agent-hooks`" + `. The output is a stable,
machine-parseable briefing followed by the current project state.

Enforcement:
  --agent=NAME must be provided, or GG_AGENT must be set in the environment.
  Otherwise the command exits with code 3 (config error) — a silent skip
  would defeat the point of enforcement.

Output layout:
  Line 1:   gg:session-start:v1     (stable marker for tooling)
  Then:     agent + project metadata
  Then:     4-line protocol summary
  Then:     current gg status output

Examples:
  gg session-start --agent=claude-code
  GG_AGENT=cursor gg session-start`,
	Args: cobra.NoArgs,
	RunE: runSessionStart,
}

func init() {
	sessionStartCmd.Flags().StringVar(&sessionStartAgent, "agent", "",
		"agent name (claude-code, cursor, aider, codex, ...) — overrides $GG_AGENT")
	rootCmd.AddCommand(sessionStartCmd)
}

func runSessionStart(cmd *cobra.Command, _ []string) error {
	agent := resolveSessionAgent()
	if agent == "" {
		fmt.Fprintln(os.Stderr, "error: no agent identity — set GG_AGENT or pass --agent=<name>")
		fmt.Fprintln(os.Stderr, "       examples: claude-code, gsd, codex, cursor, aider")
		fmt.Fprintln(os.Stderr, "       agents must self-identify so telemetry can distinguish")
		fmt.Fprintln(os.Stderr, "       agent-initiated calls from human ones.")
		return configErr("agent identity required (set GG_AGENT or pass --agent)")
	}

	br := session.Briefing{Agent: agent}
	// Project metadata is best-effort. A missing .gg/ just means the user ran
	// session-start outside a project — the briefing header is still useful.
	if root, err := config.FindRoot(); err == nil {
		br.ProjectRoot = root
	}
	if cfg, err := config.Load(); err == nil {
		br.ProjectID = cfg.ProjectID
	}

	if err := br.Render(os.Stdout); err != nil {
		return err
	}

	// Inline `gg status` so the briefing carries the full current-state
	// snapshot the agent would otherwise have to fetch separately. runStatus
	// failure (e.g. Qdrant down) is non-fatal here — the briefing header
	// and AGENTS.md pointer remain useful even with the store unreachable.
	fmt.Println("─── CURRENT PROJECT STATE (gg status) ───")
	if err := runStatus(cmd, nil); err != nil {
		fmt.Fprintf(os.Stderr, "warning: gg status failed: %v\n", err)
	}
	return nil
}

func resolveSessionAgent() string {
	if s := strings.TrimSpace(sessionStartAgent); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("GG_AGENT"))
}
