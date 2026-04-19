package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// auditTrendsSince is the lookback window for the metrics. Humans write "7d"
// / "14d" / "30d"; 0 or unparseable falls back to 7.
var auditTrendsSince string

// Reopen-rate thresholds (percent). Tuned from dogfood audit 2026-04-19:
// onelift's TASK-200→207 + TASK-194→204→209 chain sits around 30-40%,
// which is the class of stability incident the gate is meant to catch.
const (
	reopenRateWarnPercent   = 20
	reopenRateFreezePercent = 40
)

var auditTrendsCmd = &cobra.Command{
	Use:   "trends",
	Short: "Quality-signal metrics: bug reopen rate over a lookback window",
	Long: `Summarise the project's recent stability signals without blocking any
workflow — this is a diagnostic, not a gate.

Metrics:
  reopen_rate = reopens / (reopens + fresh_closes), last --since window
    < 20% healthy; 20–40% stabilise before adding features; > 40% feature-freeze.

Surface pressure (distinct files touched per task close) is a planned metric
tracked by a follow-up task once the git-integration path is in.

Use: run weekly during dogfood reviews, or before deciding to add new CLI
surface to a project already in a bug treadmill (the signal that motivated
this command per TASK-223).`,
	Args: cobra.NoArgs,
	RunE: runAuditTrends,
}

func init() {
	auditTrendsCmd.Flags().StringVar(&auditTrendsSince, "since", "7d",
		"lookback window (e.g. 7d, 14d, 30d)")
	auditCmd.AddCommand(auditTrendsCmd)
}

func runAuditTrends(cmd *cobra.Command, _ []string) error {
	days, err := parseSinceDays(auditTrendsSince)
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	stats, err := d.store.BugHealthStatsSince(ctx, days)
	if err != nil {
		return fmt.Errorf("bug health stats: %w", err)
	}

	total := stats.Reopens + stats.FreshCloses
	rate := 0.0
	if total > 0 {
		rate = 100.0 * float64(stats.Reopens) / float64(total)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Quality trends — last %dd\n", days)
	fmt.Fprintln(w, strings.Repeat("─", 40))
	fmt.Fprintf(w, "  Reopens (window):      %d\n", stats.Reopens)
	fmt.Fprintf(w, "  Fresh closes (window): %d\n", stats.FreshCloses)

	icon, verdict := reopenRateVerdict(rate, total)
	if total == 0 {
		fmt.Fprintf(w, "  Reopen rate:           %s %s\n", icon, verdict)
	} else {
		fmt.Fprintf(w, "  Reopen rate:           %s %.1f%%  — %s\n", icon, rate, verdict)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Surface pressure (files touched per close): not yet implemented — tracked as a follow-up.")
	return nil
}

// reopenRateVerdict returns an icon + one-line verdict appropriate to the
// reopen-rate percentage. Returns the low-signal verdict when total == 0
// (window had no closes at all) so callers can reuse the same print path.
func reopenRateVerdict(rate float64, total int) (icon string, verdict string) {
	switch {
	case total == 0:
		return "·", "no bug closes in window — no signal"
	case rate >= reopenRateFreezePercent:
		return "✗", "feature-freeze: defer new scope until rate drops below 20%"
	case rate >= reopenRateWarnPercent:
		return "⚠", "stabilise before adding new scope"
	default:
		return "✓", "healthy"
	}
}

// parseSinceDays converts a "7d" / "14d" / "30d" argument into an integer
// day count. Empty input → 7 (the default). Accepts bare integers too so
// "14" works the same as "14d".
func parseSinceDays(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 7, nil
	}
	raw = strings.TrimSuffix(raw, "d")
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("--since %q: want an integer day count like 7d or 14d", auditTrendsSince)
	}
	if n <= 0 {
		return 0, fmt.Errorf("--since must be positive, got %d", n)
	}
	return n, nil
}
