package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	gg "github.com/gurkangul/gg-cli"
	"github.com/gurkangul/gg-cli/internal/changelog"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/projectstate"
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
	// Inject CHANGELOG.md content so the parser has it at runtime.
	changelog.SetContent(gg.ChangelogRaw)
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
	var loadedCfg *config.Config
	if cfg, err := config.Load(); err == nil {
		br.ProjectID = cfg.ProjectID
		loadedCfg = cfg
	}

	if err := br.Render(os.Stdout); err != nil {
		return err
	}

	// Version-delta notice: compare last_seen_cli_version to current version.
	// Best-effort — failures are silently swallowed so a missing state file
	// never disrupts the session.
	emitVersionDelta(loadedCfg)

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

// emitVersionDelta surfaces a version upgrade notice when the CLI has been
// updated since the last session. Updates last_seen_cli_version on success.
// cfg may be nil (outside a project) — silently skipped.
func emitVersionDelta(cfg *config.Config) {
	if cfg == nil {
		return
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return
	}
	state, err := projectstate.Read(runtimeDir)
	if err != nil {
		return
	}
	curr := version // package-level var set by ldflags
	prev := state.LastSeenCLIVersion

	// Always update so next session sees the current version.
	defer func() {
		_ = projectstate.Write(runtimeDir, projectstate.State{LastSeenCLIVersion: curr})
	}()

	if prev == "" || prev == curr {
		return
	}

	excerpt := changelog.Since(prev, curr)
	if excerpt == "" {
		fmt.Printf("─── VERSION UPDATE: %s → %s ───\n\n", prev, curr)
		return
	}

	// Limit excerpt to first 50 lines to keep session context concise.
	lines := strings.Split(excerpt, "\n")
	const maxLines = 50
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	fmt.Printf("─── VERSION UPDATE: %s → %s ───\n", prev, curr)
	fmt.Println()
	fmt.Println(strings.Join(lines, "\n"))
	if truncated {
		fmt.Println("… (see CHANGELOG.md for full details)")
	}
	fmt.Println()
}

func resolveSessionAgent() string {
	if s := strings.TrimSpace(sessionStartAgent); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("GG_AGENT"))
}
