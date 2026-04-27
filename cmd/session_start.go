package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	gg "github.com/gurkangul/gg-cli"
	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/changelog"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/projectstate"
	"github.com/gurkangul/gg-cli/internal/session"
)

var sessionStartAgent string
var sessionStartBench bool

// sessionStartStderr is the writer for session-start stderr output.
// Tests override this to capture output without redirecting os.Stderr.
var sessionStartStderr io.Writer = os.Stderr

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
		"agent name (claude-code, cursor, codex, ...) — overrides $GG_AGENT")
	sessionStartCmd.Flags().BoolVar(&sessionStartBench, "bench", false,
		"print timing for the managed-block resync step to stderr")
	rootCmd.AddCommand(sessionStartCmd)
	// Inject CHANGELOG.md content so the parser has it at runtime.
	changelog.SetContent(gg.ChangelogRaw)
}

func runSessionStart(cmd *cobra.Command, _ []string) error {
	agent := resolveSessionAgent()
	if agent == "" {
		fmt.Fprintln(os.Stderr, "error: no agent identity — set GG_AGENT or pass --agent=<name>")
		fmt.Fprintln(os.Stderr, "       examples: claude-code, gsd, codex, cursor")
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

	// Auto-resync managed blocks: repairs contract, master-role, dev-routing,
	// and agent-specific blocks (codex/bmad/gsd) when their detection signal is
	// present. Best-effort — failures are reported but never fatal.
	if br.ProjectRoot != "" {
		emitResync(br.ProjectRoot, sessionStartBench, sessionStartStderr)
	}

	// Master heartbeat notice: do not auto-start a background watcher from
	// session-start (that can leak duplicate jobs), but make stale supervision
	// visible whenever a master session opens with registered worker panes.
	emitMasterHeartbeatNotice(loadedCfg, agent, sessionStartStderr)

	// Version-delta notice: compare last_seen_cli_version to current version.
	// Best-effort — failures are silently swallowed so a missing state file
	// never disrupts the session.
	emitVersionDelta(loadedCfg)

	// Bypass-audit notice: surface GG_ENFORCEMENT=off events from the last
	// 7 days so the human at the keyboard sees bypass pressure before
	// anything else. Silent when count is zero.
	emitBypassDelta(loadedCfg)

	// Auto-backup: snapshot brain when stale. Non-fatal — failure is logged
	// to stderr with [brain-backup] prefix and never affects exit code.
	emitBrainAutoBackup(sessionStartStderr)

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

// emitResync runs SyncManagedBlocks against projectRoot and writes any repair
// notices or errors to w. When bench is true, appends a timing line. Extracted
// so tests can inject an arbitrary root + writer without going through the full
// cobra command stack.
func emitResync(projectRoot string, bench bool, w io.Writer) {
	syncStart := time.Now()
	sr := agenthooks.SyncManagedBlocks(projectRoot)
	syncElapsed := time.Since(syncStart)
	if sr.Repaired || len(sr.Errors) > 0 {
		fmt.Fprintln(w, "─── MANAGED BLOCK RESYNC ───")
		for _, l := range sr.Lines {
			fmt.Fprintln(w, l)
		}
		for _, e := range sr.Errors {
			fmt.Fprintf(w, "  ✗ resync error: %v\n", e)
		}
		fmt.Fprintln(w)
	}
	if bench {
		fmt.Fprintf(w, "bench: managed-block resync %v\n", syncElapsed.Round(time.Millisecond))
	}
}

func emitMasterHeartbeatNotice(cfg *config.Config, agent string, w io.Writer) {
	if cfg == nil || !isMasterSessionAgent(agent) {
		return
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return
	}
	writeMasterHeartbeatNotice(w, runtimeDir)
}

func isMasterSessionAgent(agent string) bool {
	a := strings.ToLower(strings.TrimSpace(agent))
	return a == "claude-code" || a == "master"
}

func writeMasterHeartbeatNotice(w io.Writer, runtimeDir string) {
	panes, err := spawn.ListPanes(runtimeDir)
	if err != nil || len(panes) == 0 {
		return
	}
	alive, reason := spawn.IsMasterAlive(runtimeDir)
	if alive {
		return
	}

	fmt.Fprintln(w, "─── MASTER HEARTBEAT STALE ───")
	fmt.Fprintf(w, "  ✗ active worker panes: %d; master heartbeat: %s\n", len(panes), reason)
	fmt.Fprintln(w, "  run: GG_AGENT=claude-code gg spawn heartbeat --watch --poll 90 &")
	fmt.Fprintln(w, "  then: gg spawn status")
	fmt.Fprintln(w)
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
	writeVersionDelta(os.Stdout, runtimeDir, version)
}

// writeVersionDelta contains the testable body of emitVersionDelta: given a
// runtime dir and a "current" version string, load the state, print the
// delta block when applicable, and write LSCV back. Separated so tests can
// exercise the full flow without mocking the process-global cfg/version.
func writeVersionDelta(w io.Writer, runtimeDir, curr string) {
	state, err := projectstate.Read(runtimeDir)
	if err != nil {
		return
	}
	prev := state.LastSeenCLIVersion

	// Always update so next session sees the current version.
	// Preserve other state fields (BypassLog, etc.) by mutating-then-writing
	// the existing struct — the previous version wrote a fresh State{} and
	// silently dropped BypassLog every session-start.
	defer func() {
		state.LastSeenCLIVersion = curr
		_ = projectstate.Write(runtimeDir, state)
	}()

	if prev == "" || prev == curr {
		return
	}

	excerpt := changelog.Since(prev, curr)
	if excerpt == "" {
		fmt.Fprintf(w, "─── VERSION UPDATE: %s → %s ───\n\n", prev, curr)
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
	fmt.Fprintf(w, "─── VERSION UPDATE: %s → %s ───\n", prev, curr)
	fmt.Fprintln(w)
	fmt.Fprintln(w, strings.Join(lines, "\n"))
	if truncated {
		fmt.Fprintln(w, "… (see CHANGELOG.md for full details)")
	}
	fmt.Fprintln(w)
}

// emitBypassDelta surfaces a warning when GG_ENFORCEMENT=off bypasses have
// occurred in the last 7 days. Silent when count is zero or project context
// is missing. Format mirrors emitVersionDelta (box header, short body) so
// multiple delta blocks stack predictably in the session briefing.
func emitBypassDelta(cfg *config.Config) {
	if cfg == nil {
		return
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	entries, err := projectstate.ListBypassesSince(runtimeDir, cutoff)
	if err != nil || len(entries) == 0 {
		return
	}
	// Count per-gate so the header answers "which gate is being bypassed?".
	perGate := map[string]int{}
	for _, e := range entries {
		perGate[e.Gate]++
	}
	fmt.Printf("─── BYPASS AUDIT (last 7d): %d %s ───\n", len(entries), pluralize("bypass", len(entries)))
	for g, n := range perGate {
		fmt.Printf("  %-40s %d\n", g, n)
	}
	// Show the most recent 3 entries inline so the operator has concrete
	// task/actor context without running `gg doctor --bypass-audit` first.
	fmt.Println()
	last := entries
	if len(last) > 3 {
		last = last[len(last)-3:]
	}
	for _, e := range last {
		taskInfo := e.TaskID
		if taskInfo == "" {
			taskInfo = "(no task)"
		}
		actor := e.Actor
		if actor == "" {
			actor = "(unknown)"
		}
		fmt.Printf("  %s  %-28s  %-18s  %s\n", shortDate(e.TS), e.Gate, taskInfo, actor)
	}
	fmt.Println("  (full log: gg doctor --bypass-audit)")
	fmt.Println()
}

func pluralize(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "es"
}

func resolveSessionAgent() string {
	if s := strings.TrimSpace(sessionStartAgent); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("GG_AGENT"))
}

// emitBrainAutoBackup runs 'gg brain export --if-stale=INTERVAL' in the background.
// Respects GG_AUTO_BACKUP=off to disable, and GG_AUTO_BACKUP_INTERVAL to override
// the default 24h staleness threshold. Errors are written to w with a [brain-backup]
// prefix and never propagate — session-start exit code is never affected.
func emitBrainAutoBackup(w io.Writer) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("GG_AUTO_BACKUP")), "off") {
		return
	}
	interval := strings.TrimSpace(os.Getenv("GG_AUTO_BACKUP_INTERVAL"))
	if interval == "" {
		interval = "24h"
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(w, "[brain-backup] could not locate gg binary: %v\n", err)
		return
	}

	out, err := exec.Command(self, "brain", "export", "--if-stale="+interval).CombinedOutput() //nolint:gosec
	if err != nil {
		fmt.Fprintf(w, "[brain-backup] export failed: %v\n", err)
		if len(out) > 0 {
			fmt.Fprintf(w, "[brain-backup] %s\n", strings.TrimSpace(string(out)))
		}
		return
	}
	// On success, forward the one-liner note (if any) directly to stdout.
	if msg := strings.TrimSpace(string(out)); msg != "" {
		fmt.Println(msg)
	}
}
