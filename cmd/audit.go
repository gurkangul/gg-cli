package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

const auditMinWrites = 3

// auditCaptureVerbs are the gg verbs that count as knowledge capture — if
// any of these appear in telemetry since the first write, the session passes.
var auditCaptureVerbs = []string{"record", "decide", "task", "bug"}

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Session mutation audit (called by PostToolUse and Stop hooks)",
	Long: `Track Edit/Write/MultiEdit mutations during a session and emit a
warning at session end when N>=3 mutations happened with no gg
record/decide/task calls — non-blocking, visibility only.

This command is called automatically by hooks installed via:
  gg doctor --install-agent-hooks

Set GG_NO_AUDIT=1 to suppress both the track and report hooks.`,
}

var (
	auditFlagSessionID string
	auditFlagFile      string

	auditGapsSince      string
	auditGapsCompact    bool
	auditGapsIncludeAll bool

	auditDecideGapsSince   string
	auditDecideGapsCompact bool
)

// decideKeywords is a conservative set of phrases that indicate an agent
// produced decision-shaped text. The bar is intentionally high to minimise
// false positives — only unambiguous decision statements are flagged.
var decideKeywords = []string{
	"decided to", "we decided", "i decided",
	"going with ", "we'll use ", "we will use ",
	"we chose ", "we rejected", "rejected because",
	"instead of ", "the decision is", "decision:",
	"we agreed to", "agreed on ", "agreed to use",
	"karar verdik", "reddettik", "seçtik",
}

// gapsDefaultSkipPrefixes are path prefixes excluded from coverage reporting
// by default because they contain auto-generated or test-only files that
// cannot meaningfully be referenced in gg tasks/decisions.
var gapsDefaultSkipPrefixes = []string{
	"docs/cli/",    // auto-generated CLI reference docs
	"testdata/",    // regression test fixtures
	"_bmad",        // BMAD agent planning artifacts
	"seed/",        // demo seed data
	".gsd/",        // GSD planning workspace
	".claude/",     // Claude config artifacts
}

var auditGapsCmd = &cobra.Command{
	Use:   "gaps",
	Short: "List files with recent git commits but no gg record/decision/task coverage",
	Long: `Walk git log for the look-back window (default 7d) and report files
that were committed but never referenced in any gg task, decision, or record.

Use this as a weekly retrospective companion to gg audit report (which fires
live at session end). Helps dogfood reviewers spot knowledge-capture gaps
without reading every commit.`,
	Args: cobra.NoArgs,
	RunE: runAuditGaps,
}

var auditHealthDays int

var auditHealthCmd = &cobra.Command{
	Use:   "health",
	Short: "Reopen rate + surface pressure metrics for quality trend analysis",
	Long: `Compute two stability signals from bug history:

  reopen_rate_7d      = reopens / (reopens + fresh_closes)
  surface_pressure_p95 = p95 of distinct files touched per bug fix

Both are non-blocking observations. Thresholds are configurable in
.gg/config.yaml under audit.thresholds. Defaults:
  reopen_rate_warn:      0.20  (>20%  → "stabilize before adding")
  reopen_rate_freeze:    0.40  (>40%  → "freeze new features")
  surface_pressure_p95:  3     (>3    → "centralize common state")`,
	Args: cobra.NoArgs,
	RunE: runAuditHealth,
}

var auditTrackCmd = &cobra.Command{
	Use:   "track",
	Short: "Record one file mutation in the session audit log (PostToolUse hook)",
	Args:  cobra.NoArgs,
	RunE:  runAuditTrack,
}

var auditReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Emit audit warning at session end if mutations are untracked (Stop hook)",
	Args:  cobra.NoArgs,
	RunE:  runAuditReport,
}

var auditDecideGapsCmd = &cobra.Command{
	Use:   "decide-gaps",
	Short: "Flag gg messages containing decision-language but no corresponding gg record",
	Long: `Scan gg tell messages in the look-back window for decision-shaped text
(e.g. "decided to", "going with", "we chose", "rejected because") and
cross-reference against gg record/decide calls created in the same window.
Messages that look like decisions but have no matching gg record are flagged.

Non-blocking — exits 0 even when gaps are found.`,
	Args: cobra.NoArgs,
	RunE: runAuditDecideGaps,
}

func init() {
	auditTrackCmd.Flags().StringVar(&auditFlagSessionID, "session-id", "", "session ID ($CLAUDE_SESSION_ID)")
	auditTrackCmd.Flags().StringVar(&auditFlagFile, "file", "", "mutated file path")

	auditReportCmd.Flags().StringVar(&auditFlagSessionID, "session-id", "", "session ID ($CLAUDE_SESSION_ID)")

	auditGapsCmd.Flags().StringVar(&auditGapsSince, "since", "7d", "look back window (e.g. 7d, 14d, 30d)")
	auditGapsCmd.Flags().BoolVar(&auditGapsCompact, "compact", false, "one line per gap — no coverage details")
	auditGapsCmd.Flags().BoolVar(&auditGapsIncludeAll, "include-all", false, "include auto-gen and test dirs (docs/cli, testdata, _bmad, seed)")

	auditHealthCmd.Flags().IntVar(&auditHealthDays, "days", 7, "look-back window in days")

	auditDecideGapsCmd.Flags().StringVar(&auditDecideGapsSince, "since", "7d", "look back window (e.g. 7d, 14d, 30d)")
	auditDecideGapsCmd.Flags().BoolVar(&auditDecideGapsCompact, "compact", false, "one line per flagged message — no detail")

	auditCmd.AddCommand(auditTrackCmd, auditReportCmd, auditGapsCmd, auditHealthCmd, auditDecideGapsCmd)
	rootCmd.AddCommand(auditCmd)
}

type auditEntry struct {
	Ts   string `json:"ts"`
	File string `json:"file"`
}

func auditSessionFile(sessionID string) string {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		id = "default"
	}
	return filepath.Join(os.TempDir(), "gg-audit-"+id+".jsonl")
}

func runAuditTrack(_ *cobra.Command, _ []string) error {
	entry := auditEntry{
		Ts:   time.Now().UTC().Format(time.RFC3339),
		File: auditFlagFile,
	}
	line, _ := json.Marshal(entry)

	path := auditSessionFile(auditFlagSessionID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil // best-effort; never block the write tool
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(line, '\n'))
	return nil
}

func runAuditReport(cmd *cobra.Command, _ []string) error {
	path := auditSessionFile(auditFlagSessionID)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // no writes this session
	}
	if err != nil {
		return nil // best-effort; Stop hook must not fail
	}
	defer func() { _ = os.Remove(path) }()

	var entries []auditEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e auditEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			entries = append(entries, e)
		}
	}

	if len(entries) < auditMinWrites {
		return nil
	}

	// Check gg telemetry for knowledge-capture calls since the first write.
	captureCount := countCaptureSince(firstWriteTime(entries))
	if captureCount > 0 {
		return nil
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "\naudit: %d file mutations this session with no gg record/decide/task calls\n", len(entries))
	unique := dedupAuditFiles(entries)
	fmt.Fprintln(w, "  Mutated files:")
	for i, f := range unique {
		if i >= 10 {
			fmt.Fprintf(w, "  ... and %d more\n", len(unique)-10)
			break
		}
		if f != "" {
			fmt.Fprintf(w, "  • %s\n", f)
		}
	}
	fmt.Fprintln(w, "  Run `gg record` or `gg decide` to capture the rationale.")
	return nil
}

func countCaptureSince(since time.Time) int {
	cfg, err := config.Load()
	if err != nil {
		return 0
	}
	rdir, err := cfg.RuntimeDir()
	if err != nil {
		return 0
	}
	sum, err := telemetry.SummarizeFrom(rdir, since)
	if err != nil {
		return 0
	}
	total := 0
	for _, verb := range auditCaptureVerbs {
		total += sum.VerbCounts[verb]
	}
	return total
}

func firstWriteTime(entries []auditEntry) time.Time {
	if len(entries) == 0 {
		return time.Now().UTC().Add(-24 * time.Hour)
	}
	t, err := time.Parse(time.RFC3339, entries[0].Ts)
	if err != nil {
		return time.Now().UTC().Add(-24 * time.Hour)
	}
	return t
}

func dedupAuditFiles(entries []auditEntry) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if !seen[e.File] {
			seen[e.File] = true
			out = append(out, e.File)
		}
	}
	return out
}

// auditHealthThresholds returns the effective thresholds, applying defaults for
// any zero values that the config leaves unset.
func auditHealthThresholds() (warnRate, freezeRate float64, p95Files int) {
	warnRate, freezeRate, p95Files = 0.20, 0.40, 3
	cfg, err := config.Load()
	if err != nil {
		return
	}
	t := cfg.Audit.Thresholds
	if t.ReopenRateWarn > 0 {
		warnRate = t.ReopenRateWarn
	}
	if t.ReopenRateFreeze > 0 {
		freezeRate = t.ReopenRateFreeze
	}
	if t.SurfacePressureP95 > 0 {
		p95Files = t.SurfacePressureP95
	}
	return
}

func runAuditHealth(cmd *cobra.Command, _ []string) error {
	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	w := cmd.OutOrStdout()

	if d.qdrantDown {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: Qdrant unreachable — health metrics unavailable")
		return nil
	}

	warnRate, freezeRate, p95Threshold := auditHealthThresholds()

	// ── reopen rate ──────────────────────────────────────────────────────────
	health, err := d.store.BugHealthStatsSince(ctx, auditHealthDays)
	if err != nil {
		return fmt.Errorf("reopen stats: %w", err)
	}
	total := health.Reopens + health.FreshCloses
	var reopenRate float64
	if total > 0 {
		reopenRate = float64(health.Reopens) / float64(total)
	}

	reopenLabel, reopenIcon := formatReopenRate(reopenRate, warnRate, freezeRate, total)
	fmt.Fprintf(w, "Reopen rate (%dd):     %s %s\n", auditHealthDays, reopenLabel, reopenIcon)

	// ── surface pressure ─────────────────────────────────────────────────────
	sp, err := d.store.SurfacePressureSince(ctx, auditHealthDays)
	if err != nil {
		return fmt.Errorf("surface pressure: %w", err)
	}

	spLabel, spIcon := formatSurfacePressure(sp.P95FilesPerFix, p95Threshold, sp.SampleSize)
	fmt.Fprintf(w, "Surface pressure p95: %s %s\n", spLabel, spIcon)

	// ── recommendation ───────────────────────────────────────────────────────
	if rec := auditHealthRecommendation(reopenRate, sp.P95FilesPerFix, warnRate, freezeRate, p95Threshold); rec != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Recommendation: %s\n", rec)
	}

	return nil
}

func formatReopenRate(rate, warnRate, freezeRate float64, sampleSize int) (label, icon string) {
	if sampleSize == 0 {
		return "n/a (no data)", ""
	}
	pct := fmt.Sprintf("%d%%", int(rate*100+0.5))
	switch {
	case rate > freezeRate:
		return pct, "⚠⚠"
	case rate > warnRate:
		return pct, "⚠"
	default:
		return pct, "✓"
	}
}

func formatSurfacePressure(p95, threshold, sampleSize int) (label, icon string) {
	if sampleSize == 0 {
		return "n/a (no data)", ""
	}
	s := fmt.Sprintf("%d files/fix  (n=%d)", p95, sampleSize)
	if p95 > threshold {
		return s, "⚠"
	}
	return s, "✓"
}

func auditHealthRecommendation(reopenRate float64, p95Files int, warnRate, freezeRate float64, p95Threshold int) string {
	highReopen := reopenRate > freezeRate
	warnReopen := reopenRate > warnRate && !highReopen
	highSurface := p95Files > p95Threshold

	switch {
	case highReopen && highSurface:
		return "freeze new features until reopen_rate < " + pctStr(warnRate) + " and centralize common state to reduce surface pressure"
	case highReopen:
		return "freeze new features until reopen_rate < " + pctStr(warnRate)
	case warnReopen && highSurface:
		return "stabilize before adding features; centralize common state to reduce surface pressure"
	case warnReopen:
		return "stabilize before adding new features — reopen rate is elevated"
	case highSurface:
		return "centralize common state to reduce surface pressure"
	default:
		return ""
	}
}

func pctStr(f float64) string {
	return fmt.Sprintf("%d%%", int(f*100+0.5))
}

// parseSinceFlag converts a duration string like "7d" or "14d" to a --since
// value accepted by git log (e.g. "7 days ago").
func parseSinceFlag(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		days = strings.TrimSpace(days)
		if days == "" || strings.ContainsAny(days, " \t") {
			return "", fmt.Errorf("invalid --since value %q: expected NNd (e.g. 7d)", s)
		}
		return days + " days ago", nil
	}
	return "", fmt.Errorf("invalid --since value %q: only NNd format supported (e.g. 7d, 14d)", s)
}

// gitChangedFiles returns the set of files changed in commits since the given
// git --since value (e.g. "7 days ago").
func gitChangedFiles(projectRoot, since string) ([]string, error) {
	// --diff-filter=d excludes deleted files — they can't be in the current codebase.
	out, err := exec.Command(
		"git", "-C", projectRoot,
		"log", "--since="+since, "--name-only", "--diff-filter=d", "--pretty=format:",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	seen := map[string]bool{}
	var files []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		files = append(files, line)
	}
	return files, scanner.Err()
}

// buildGGCorpus returns a slice of all text blobs from gg tasks, decisions,
// and rejections. Each blob is the concatenated searchable text for one entry.
func buildGGCorpus(d *deps) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var corpus []string

	tasks, err := d.store.ListTasks(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	for _, t := range tasks {
		corpus = append(corpus, strings.ToLower(t.Title+" "+t.Detail))
	}

	decisions, err := d.store.ListDecisions(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("list decisions: %w", err)
	}
	for _, dec := range decisions {
		corpus = append(corpus, strings.ToLower(dec.Text+" "+dec.Reason))
	}

	rejections, err := d.store.ListRejections(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("list rejections: %w", err)
	}
	for _, r := range rejections {
		corpus = append(corpus, strings.ToLower(r.Approach+" "+r.Reason))
	}

	return corpus, nil
}

// fileIsCovered returns true if any corpus blob mentions the file path or its
// base name.
func fileIsCovered(file string, corpus []string) bool {
	lowerFile := strings.ToLower(file)
	lowerBase := strings.ToLower(filepath.Base(file))
	for _, blob := range corpus {
		if strings.Contains(blob, lowerFile) || strings.Contains(blob, lowerBase) {
			return true
		}
	}
	return false
}

// filterGapsFiles removes files whose path starts with any of the given prefixes.
func filterGapsFiles(files, skipPrefixes []string) []string {
	out := files[:0:len(files)]
	for _, f := range files {
		skip := false
		for _, p := range skipPrefixes {
			if strings.HasPrefix(f, p) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, f)
		}
	}
	return out
}

func runAuditGaps(cmd *cobra.Command, _ []string) error {
	since, err := parseSinceFlag(auditGapsSince)
	if err != nil {
		return err
	}

	projRoot, err := config.FindRoot()
	if err != nil {
		return fmt.Errorf("find project root: %w", err)
	}

	files, err := gitChangedFiles(projRoot, since)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No commits found in the last %s.\n", auditGapsSince)
		return nil
	}

	if !auditGapsIncludeAll {
		files = filterGapsFiles(files, gapsDefaultSkipPrefixes)
	}

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	if d.qdrantDown {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: Qdrant unreachable — coverage check skipped, listing all changed files as gaps")
		for _, f := range files {
			fmt.Fprintln(cmd.OutOrStdout(), f)
		}
		return nil
	}

	corpus, err := buildGGCorpus(d)
	if err != nil {
		return err
	}

	var gaps []string
	for _, f := range files {
		if !fileIsCovered(f, corpus) {
			gaps = append(gaps, f)
		}
	}

	w := cmd.OutOrStdout()
	if len(gaps) == 0 {
		fmt.Fprintf(w, "No gaps — all %d changed files have gg coverage.\n", len(files))
		return nil
	}

	if auditGapsCompact {
		for _, f := range gaps {
			fmt.Fprintln(w, f)
		}
		return nil
	}

	fmt.Fprintf(w, "gaps: %d of %d changed files have no gg record/decision/task coverage (last %s)\n\n",
		len(gaps), len(files), auditGapsSince)
	for _, f := range gaps {
		fmt.Fprintf(w, "  • %s\n", f)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run `gg record` or `gg decide` after editing files to capture rationale.")
	return nil
}

// messageHasDecideLanguage returns true if the message content contains any of
// the conservative decide-language phrases. Case-insensitive.
func messageHasDecideLanguage(content string) bool {
	lower := strings.ToLower(content)
	for _, kw := range decideKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// decideGapCoverageWindow is the tolerance window for correlating a decision
// message with a gg record call. A record created within this duration of the
// message is considered coverage.
const decideGapCoverageWindow = 2 * time.Hour

// decisionsInWindow returns the subset of decisions whose CreatedAt falls
// within ±decideGapCoverageWindow of the given timestamp.
func decisionsInWindow(decisions []string, msgTime time.Time) []string {
	// decisions is a slice of RFC3339 timestamps from the gg store.
	var near []string
	for _, ts := range decisions {
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		diff := t.Sub(msgTime)
		if diff < 0 {
			diff = -diff
		}
		if diff <= decideGapCoverageWindow {
			near = append(near, ts)
		}
	}
	return near
}

func runAuditDecideGaps(cmd *cobra.Command, _ []string) error {
	since, err := parseSinceDuration(auditDecideGapsSince)
	if err != nil {
		return err
	}

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	w := cmd.OutOrStdout()

	if d.qdrantDown {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: Qdrant unreachable — decide-gaps check skipped")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	messages, err := d.store.ListMessagesSince(ctx, since)
	if err != nil {
		return fmt.Errorf("list messages: %w", err)
	}

	decisions, err := d.store.ListDecisions(ctx, 0)
	if err != nil {
		return fmt.Errorf("list decisions: %w", err)
	}

	// Build a slice of decision timestamps for window correlation.
	decisionTimes := make([]string, 0, len(decisions))
	for _, dec := range decisions {
		decisionTimes = append(decisionTimes, dec.CreatedAt)
	}

	// System roles that produce non-human messages — exclude from heuristic scan.
	systemRoles := map[string]bool{
		"verify-gate": true,
		"system":      true,
	}

	type flaggedMsg struct {
		fromRole  string
		toRole    string
		content   string
		msgTime   time.Time
	}
	var flagged []flaggedMsg
	for _, m := range messages {
		if systemRoles[m.FromRole] {
			continue
		}
		if !messageHasDecideLanguage(m.Content) {
			continue
		}
		msgTime, err := time.Parse(time.RFC3339, m.CreatedAt)
		if err != nil {
			continue
		}
		// Skip if there's a decision record within the coverage window.
		if len(decisionsInWindow(decisionTimes, msgTime)) > 0 {
			continue
		}
		flagged = append(flagged, flaggedMsg{
			fromRole: m.FromRole,
			toRole:   m.ToRole,
			content:  m.Content,
			msgTime:  msgTime,
		})
	}

	if len(flagged) == 0 {
		fmt.Fprintf(w, "No decide-gaps — all decision-shaped messages in the last %s have gg record coverage.\n", auditDecideGapsSince)
		return nil
	}

	if auditDecideGapsCompact {
		for _, f := range flagged {
			fmt.Fprintf(w, "[%s] %s → %s: %s\n",
				f.msgTime.Format("2006-01-02T15:04"),
				f.fromRole, f.toRole,
				truncate(f.content, 80))
		}
		return nil
	}

	fmt.Fprintf(w, "decide-gaps: %d message(s) with decision-language but no gg record (last %s)\n\n",
		len(flagged), auditDecideGapsSince)
	for _, f := range flagged {
		fmt.Fprintf(w, "  [%s] %s → %s\n", f.msgTime.Format("2006-01-02 15:04"), f.fromRole, f.toRole)
		fmt.Fprintf(w, "    %s\n\n", truncate(f.content, 120))
	}
	fmt.Fprintln(w, "Run `gg record \"<decision>\" --reason \"<why>\"` to capture the rationale.")
	return nil
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
