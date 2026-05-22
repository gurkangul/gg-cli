package cmd

import (
	"context"
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
	"github.com/gurkangul/gg-cli/internal/projectstate"
	"github.com/gurkangul/gg-cli/internal/session"
)

var sessionStartAgent string
var sessionStartRole string
var sessionStartBench bool

// sessionStartStderr is the writer for session-start stderr output.
// Tests override this to capture output without redirecting os.Stderr.
var sessionStartStderr io.Writer = os.Stderr

var runBrainAutoBackupExport = func(ctx context.Context, self, interval string) (string, string, error) {
	var outBuf, errBuf strings.Builder
	cmd := exec.CommandContext(ctx, self, "brain", "export", "--if-stale="+interval) //nolint:gosec
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	return outBuf.String(), errBuf.String(), cmd.Run()
}

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

  --role=ROLE is optional. When provided (or GG_ROLE is set), the briefing
  prints role-scoped next steps for the current agent instance.

Output layout:
  Line 1:   gg:session-start:v1     (stable marker for tooling)
  Then:     agent + project metadata
  Then:     4-line protocol summary
  Then:     current gg status output

Examples:
  gg session-start --agent=<agent-id> --role=implementer
  GG_AGENT=cursor GG_ROLE=reviewer gg session-start`,
	Args: cobra.NoArgs,
	RunE: runSessionStart,
}

func init() {
	sessionStartCmd.Flags().StringVar(&sessionStartAgent, "agent", "",
		"agent_id for this agent instance (for example omo-slim, codex-1, claude-planner) — overrides $GG_AGENT")
	sessionStartCmd.Flags().StringVar(&sessionStartRole, "role", "",
		"agent role for this session (for example implementer, reviewer, planner) — overrides $GG_ROLE in briefing output")
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
		fmt.Fprintln(os.Stderr, "       examples: codex, cursor, gsd, aider")
		fmt.Fprintln(os.Stderr, "       agents must self-identify so telemetry can distinguish")
		fmt.Fprintln(os.Stderr, "       agent-initiated calls from human ones.")
		return configErr("agent identity required (set GG_AGENT or pass --agent)")
	}

	br := session.Briefing{Agent: agent, Role: resolveSessionRole()}
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

	// Auto-resync managed blocks: repairs contract and agent-specific blocks
	// (codex/bmad/gsd) when their detection signal is present. Best-effort —
	// failures are reported but never fatal.
	if br.ProjectRoot != "" {
		emitResync(br.ProjectRoot, sessionStartBench, sessionStartStderr)
	}

	// Version-delta notice: compare last_seen_cli_version to current version.
	// Best-effort — failures are silently swallowed so a missing state file
	// never disrupts the session.
	emitVersionDelta(loadedCfg)

	// Public update notice: opt-in only because it performs a network-backed
	// Go module lookup. Silent on errors and when the current binary is fresh.
	emitUpdateNotice(os.Stdout)

	// Bypass-audit notice: surface GG_ENFORCEMENT=off events from the last
	// 7 days so the human at the keyboard sees bypass pressure before
	// anything else. Silent when count is zero.
	emitBypassDelta(loadedCfg)

	// Auto-backup: snapshot brain when stale. Non-fatal — failure is logged
	// to stderr with [brain-backup] prefix and never affects exit code.
	emitBrainAutoBackup(os.Stdout, sessionStartStderr)

	// Semantic-memory notice: JSONL remains durable, but agents should know when
	// Qdrant/outbox/placeholder vectors make semantic recall degraded.
	emitSemanticMemoryNotice(cmd.Context(), os.Stdout, loadedCfg)

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
		_ = projectstate.Update(runtimeDir, func(s *projectstate.State) error {
			s.LastSeenCLIVersion = curr
			return nil
		})
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

func resolveSessionRole() string {
	if s := strings.TrimSpace(sessionStartRole); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("GG_ROLE"))
}

// emitBrainAutoBackup runs 'gg brain export --if-stale=INTERVAL' as a bounded,
// non-fatal session-start step. Respects GG_AUTO_BACKUP=off to disable, and
// GG_AUTO_BACKUP_INTERVAL to override configured/default staleness threshold.
// The success one-liner ('✓ brain auto-snapshotted (N records)') is forwarded to
// stdout; all other output (warnings, errors, Qdrant/Memgraph noise) is routed to
// stderr with a [brain-backup] prefix.
func emitBrainAutoBackup(stdout, stderr io.Writer) {
	settings, ok := resolveBrainAutoBackupSettings(stderr)
	if !ok {
		return
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "[brain-backup] could not locate gg binary: %v\n", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), settings.timeout)
	defer cancel()

	out, errOut, runErr := runBrainAutoBackupExport(ctx, self, settings.interval)
	if ctx.Err() != nil {
		fmt.Fprintf(stderr, "[brain-backup] timeout after %s — skipping\n", settings.timeout.Round(time.Second))
		return
	}

	// Route child stderr lines to parent stderr with [brain-backup] prefix.
	if s := strings.TrimSpace(errOut); s != "" {
		for _, line := range strings.Split(s, "\n") {
			if line != "" {
				fmt.Fprintf(stderr, "[brain-backup] %s\n", line)
			}
		}
	}

	if runErr != nil {
		fmt.Fprintf(stderr, "[brain-backup] export failed: %v\n", runErr)
		return
	}

	// Forward only the success one-liner to parent stdout.
	if msg := strings.TrimSpace(out); msg != "" {
		fmt.Fprintln(stdout, msg)
	}
}

type brainAutoBackupSettings struct {
	interval string
	timeout  time.Duration
}

func resolveBrainAutoBackupSettings(stderr io.Writer) (brainAutoBackupSettings, bool) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("GG_AUTO_BACKUP")), "off") {
		return brainAutoBackupSettings{}, false
	}

	interval := "24h"
	timeout := 30 * time.Second
	if cfg, err := config.Load(); err == nil {
		if !cfg.Backup.AutoEnabled() {
			return brainAutoBackupSettings{}, false
		}
		if strings.TrimSpace(cfg.Backup.Interval) != "" {
			interval = strings.TrimSpace(cfg.Backup.Interval)
		}
		if strings.TrimSpace(cfg.Backup.Timeout) != "" {
			parsedTimeout, parseErr := time.ParseDuration(strings.TrimSpace(cfg.Backup.Timeout))
			if parseErr != nil {
				fmt.Fprintf(stderr, "[brain-backup] invalid backup.timeout in .gg/config.yaml: %v\n", parseErr)
				return brainAutoBackupSettings{}, false
			}
			timeout = parsedTimeout
		}
	} else {
		fmt.Fprintf(stderr, "[brain-backup] config load failed: %v\n", err)
		return brainAutoBackupSettings{}, false
	}
	if envInterval := strings.TrimSpace(os.Getenv("GG_AUTO_BACKUP_INTERVAL")); envInterval != "" {
		interval = envInterval
	}
	return brainAutoBackupSettings{interval: interval, timeout: timeout}, true
}
