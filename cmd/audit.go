package cmd

import (
	"encoding/json"
	"fmt"
	"os"
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

	auditFileSizeOver       int
	auditFileSizeNoBaseline bool
	auditFileSizeJSON       bool
)

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

func init() {
	auditTrackCmd.Flags().StringVar(&auditFlagSessionID, "session-id", "", "session ID ($CLAUDE_SESSION_ID)")
	auditTrackCmd.Flags().StringVar(&auditFlagFile, "file", "", "mutated file path")

	auditReportCmd.Flags().StringVar(&auditFlagSessionID, "session-id", "", "session ID ($CLAUDE_SESSION_ID)")

	auditGapsCmd.Flags().StringVar(&auditGapsSince, "since", "7d", "look back window (e.g. 7d, 14d, 30d)")
	auditGapsCmd.Flags().BoolVar(&auditGapsCompact, "compact", false, "one line per gap — no coverage details")
	auditGapsCmd.Flags().BoolVar(&auditGapsIncludeAll, "include-all", false, "include generated, test fixture, and binary/coverage noise")
	auditGapsCmd.Flags().BoolVar(&auditGapsIncludeAll, "include-noise", false, "alias for --include-all")

	auditHealthCmd.Flags().IntVar(&auditHealthDays, "days", 7, "look-back window in days")

	auditDecideGapsCmd.Flags().StringVar(&auditDecideGapsSince, "since", "7d", "look back window (e.g. 7d, 14d, 30d)")
	auditDecideGapsCmd.Flags().BoolVar(&auditDecideGapsCompact, "compact", false, "one line per flagged message — no detail")

	auditFileSizeCmd.Flags().IntVar(&auditFileSizeOver, "over", 0, "custom line threshold; overrides per-type defaults")
	auditFileSizeCmd.Flags().BoolVar(&auditFileSizeNoBaseline, "no-baseline", false, "ignore the grandfather baseline")
	auditFileSizeCmd.Flags().BoolVar(&auditFileSizeJSON, "json", false, "emit JSON array of violations")

	auditCmd.AddCommand(auditTrackCmd, auditReportCmd, auditGapsCmd, auditHealthCmd, auditDecideGapsCmd, auditFileSizeCmd)
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
	fmt.Fprintln(w, "  Run `gg record` to capture the rationale.")
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
