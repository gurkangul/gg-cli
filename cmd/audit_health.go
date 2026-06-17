package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
)

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
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: vector store unavailable — health metrics unavailable")
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
