package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	gg "github.com/gurkangul/gg-cli"
	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/changelog"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/projectstate"
	"github.com/gurkangul/gg-cli/internal/session"
	"github.com/gurkangul/gg-cli/internal/store"
)

var (
	sessionStartAgent string
	sessionStartRole  string
	sessionStartBench bool
)

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

This is the canonical orientation entrypoint used by agent SessionStart hooks installed
via ` + "`gg doctor --install-agent-hooks`" + `. The output is a stable,
machine-parseable briefing followed by the current project state.

Identity:
  --agent=NAME must be provided, or GG_AGENT must be set in the environment.
  Otherwise the command exits with code 3 (config error). Agent identity keeps
  durable memory and telemetry attributable to the runtime that wrote it.

  --role=ROLE is optional. When provided (or GG_ROLE is set), the briefing
  prints role-scoped next steps for the current agent instance.

Output layout:
  Line 1:   gg:session-start:v1     (stable marker for tooling)
  Then:     agent + project metadata
  Then:     4-line protocol summary
  Then:     current gg status output

CodeGraph notices use the shared freshness contract. session-start never runs
background graph refresh; repair is explicit via gg doctor --fix-index, and
foreground active mode is gg index --watch / gg watch --index.

Examples:
  gg session-start --agent=gsd-myproject-1 --role=planner
  gg session-start --agent=codex-1 --role=implementer
  GG_AGENT=cursor-1 GG_ROLE=reviewer gg session-start`,
	Args: cobra.NoArgs,
	RunE: runSessionStart,
}

func init() {
	sessionStartCmd.Flags().StringVar(&sessionStartAgent, "agent", "",
		"agent_id for this runtime instance (for example gsd-myproject-1, codex-1, claude-planner) — overrides $GG_AGENT")
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
		fmt.Fprintln(os.Stderr, "       examples: gsd-myproject-1, codex-1, cursor-1, aider-1")
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
	// Resolve the .gg dir NOW, before any resync step can change the working
	// directory, so the canon read targets this project (TASK-468).
	canonGGDir, _ := config.GGDir()

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

	// TASK-480: warn if commits would be attributed to an agent, not the human.
	if warn := gitCommitIdentityWarning(); warn != "" {
		fmt.Printf("⚠ %s\n", warn)
	}

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

	// Code graph notice: warn agents when impact data is missing/stale, but do
	// not run an implicit refresh. The explicit safe path is gg doctor --fix-index.
	emitCodeGraphNotice(cmd.Context(), os.Stdout, loadedCfg)

	// TASK-489: auto-heal brain relationship-graph drift (decision nodes present
	// but no edges) instead of asking the human to run gg doctor --fix-index —
	// the brain self-maintains. Silent when healthy; non-fatal.
	healBrainGraphIfDrifted(cmd, os.Stdout, loadedCfg)

	// TASK-505: auto-drain the crash-recovery outbox. When a vector write failed
	// (Qdrant down) the intent was queued in .gg/outbox/; replay it now so the
	// human never has to run `gg doctor --reconcile`. Bounded, non-fatal, and
	// silent when the outbox is empty (the common path). Opt out with
	// GG_NO_AUTO_RECONCILE=1.
	reconcileOutboxIfNeeded(cmd, os.Stdout, sessionStartStderr)

	// BUG-091: routine-drain the agent firehose. Auto-archive audience=agents
	// broadcasts older than the default 30d cutoff so month-old "TASK-N done"
	// pings retire on their own (recent ones stay; JSONL is preserved). Bounded
	// and failure-tolerant — never blocks or fails session-start. Opt out with
	// GG_NO_AUTO_DRAIN=1.
	autoDrainInboxIfNeeded(cmd, os.Stdout, sessionStartStderr)

	// TASK-468: inject the distilled project canon so a fresh agent starts with
	// the senior-dev knowledge, not just a searchable ledger. Best-effort.
	emitProjectCanon(cmd.Context(), canonGGDir)

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

// emitProjectCanon prints the distilled institutional-memory canon (TASK-468) so
// a fresh agent inherits the senior-dev knowledge at session start. Best-effort
// and JSONL-backed (works even when Qdrant is down); silent when no canon exists.
func emitProjectCanon(ctx context.Context, ggDir string) {
	if ggDir == "" {
		return
	}
	// Curated canon is JSONL-backed (works even when Qdrant is down). The
	// auto-derived canon is computed live from the ledger so it requires no
	// manual upkeep — a fresh agent gets it automatically with zero curation.
	manual, _ := store.ReadCanon(ggDir)
	var auto []store.CanonEntry
	if d, err := loadDepsReadOnly(false); err == nil {
		defer d.Close()
		if !d.qdrantDown {
			cctx, cancel := withTimeout(ctx)
			defer cancel()
			auto = autoCanonEntries(cctx, d, true) // compact view for the per-session briefing
		}
	}
	if len(manual) == 0 && len(auto) == 0 {
		return
	}
	fmt.Println("─── PROJECT CANON (distilled institutional memory) ───")
	for _, e := range manual {
		fmt.Printf("## %s\n%s\n", e.Area, e.Text)
	}
	for _, e := range auto {
		fmt.Printf("## %s\n%s\n", e.Area, e.Text)
	}
	if len(auto) > 0 {
		fmt.Println("(compact view — full canon: gg canon show)")
	}
	fmt.Println()
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
	// BUG-086 QA-followup: keep the briefing lean for a fresh agent — a per-gate
	// count answers "which gate is under pressure?"; the per-entry task/actor
	// detail is operator-grade and lives behind `gg doctor --bypass-audit`.
	fmt.Printf("─── BYPASS AUDIT (last 7d): %d %s — ", len(entries), pluralize("bypass", len(entries)))
	gates := make([]string, 0, len(perGate))
	for g := range perGate {
		gates = append(gates, g)
	}
	sort.Slice(gates, func(i, j int) bool {
		if perGate[gates[i]] != perGate[gates[j]] {
			return perGate[gates[i]] > perGate[gates[j]]
		}
		return gates[i] < gates[j]
	})
	parts := make([]string, 0, len(gates))
	for _, g := range gates {
		parts = append(parts, fmt.Sprintf("%s×%d", g, perGate[g]))
	}
	fmt.Printf("%s ───\n", strings.Join(parts, ", "))
	fmt.Println("  detail: gg doctor --bypass-audit")
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
