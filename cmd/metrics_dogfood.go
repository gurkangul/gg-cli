package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

// Rework-rate thresholds: fraction of task decisions that reference rework.
// >20% means the team is iterating heavily; >40% means design-before-commit.
const (
	reworkRateWarnFrac   = 0.20
	reworkRateFreezeFrac = 0.40
)

var metricsDogfoodSince string
var metricsDogfoodCompact bool

var metricsDogfoodCmd = &cobra.Command{
	Use:   "dogfood",
	Short: "Per-project velocity and rework rate for dogfood sessions",
	Long: `Compute two dogfood health signals for the project:

  velocity      = done tasks / week over the lookback window
  rework_rate   = decisions referencing rework iterations / total task decisions
  gap_rate      = decisions referencing accept-with-gap / total task closures

velocity measures throughput. rework_rate measures how often shipped work
needed a revision cycle. gap_rate measures how often work shipped with known
gaps (accepted but imperfect). Together they answer: are we moving fast, and
is the quality holding?

Signals are advisory — exits 0 regardless of values.`,
	Args: cobra.NoArgs,
	RunE: runMetricsDogfood,
}

func init() {
	metricsDogfoodCmd.Flags().StringVar(&metricsDogfoodSince, "since", "14d",
		"lookback window (e.g. 7d, 14d, 30d)")
	metricsDogfoodCmd.Flags().BoolVar(&metricsDogfoodCompact, "compact", false,
		"one-line summary — three metrics, no chart")
	metricsCmd.AddCommand(metricsDogfoodCmd)
}

// dogfoodMetrics is the computed report payload.
type dogfoodMetrics struct {
	WindowDays    int
	DoneTasks     int     // done tasks created in window
	WeeksInWindow float64 // fractional weeks
	Velocity      float64 // tasks per week
	// rework: decisions for task IDs whose text contains "rework"
	ReworkDecisions int
	TotalDecisions  int
	ReworkRate      float64
	// gap: decisions whose text contains "accept-with-gap"
	GapDecisions int
	DoneTaskIDs  int // denominator for gap rate
	GapRate      float64
}

func runMetricsDogfood(cmd *cobra.Command, _ []string) error {
	days, err := parseSinceDays(metricsDogfoodSince)
	if err != nil {
		return err
	}

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if d.qdrantDown {
		fmt.Fprintln(cmd.ErrOrStderr(), "⚠ Qdrant unreachable — dogfood metrics unavailable.")
		return nil
	}

	tasks, err := d.store.ListTasks(ctx, "done")
	if err != nil {
		return fmt.Errorf("list done tasks: %w", err)
	}
	decisions, err := d.store.ListDecisions(ctx, 0)
	if err != nil {
		return fmt.Errorf("list decisions: %w", err)
	}

	m := computeDogfoodMetrics(days, tasks, decisions)

	w := cmd.OutOrStdout()
	if isCompactActive(cmd) {
		renderDogfoodCompact(w, m)
		return nil
	}
	renderDogfoodDefault(w, m)
	return nil
}

func computeDogfoodMetrics(days int, tasks []store.Task, decisions []store.Decision) dogfoodMetrics {
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	weeks := float64(days) / 7.0

	// Velocity: count done tasks whose created_at falls in the window.
	// We use created_at as a proxy (done_at is not stored).
	var doneTasks []store.Task
	doneIDs := map[string]bool{}
	for _, t := range tasks {
		ts, err := time.Parse(time.RFC3339, t.CreatedAt)
		if err != nil || ts.Before(cutoff) {
			continue
		}
		doneTasks = append(doneTasks, t)
		doneIDs[t.ID] = true
	}

	velocity := 0.0
	if weeks > 0 {
		velocity = float64(len(doneTasks)) / weeks
	}

	// Rework rate: among decisions linked to any task in the window,
	// how many reference a rework iteration ("rework-N" or "rework " prefix).
	totalDec := 0
	reworkDec := 0
	gapDec := 0
	for _, dec := range decisions {
		ts, err := time.Parse(time.RFC3339, dec.CreatedAt)
		if err != nil || ts.Before(cutoff) {
			continue
		}
		if dec.TaskID == "" {
			continue
		}
		totalDec++
		lower := strings.ToLower(dec.Text)
		if strings.Contains(lower, "rework") {
			reworkDec++
		}
		if strings.Contains(lower, "accept-with-gap") {
			gapDec++
		}
	}

	reworkRate := 0.0
	if totalDec > 0 {
		reworkRate = float64(reworkDec) / float64(totalDec)
	}
	gapRate := 0.0
	if len(doneTasks) > 0 {
		gapRate = float64(gapDec) / float64(len(doneTasks))
	}

	return dogfoodMetrics{
		WindowDays:      days,
		DoneTasks:       len(doneTasks),
		WeeksInWindow:   weeks,
		Velocity:        velocity,
		ReworkDecisions: reworkDec,
		TotalDecisions:  totalDec,
		ReworkRate:      reworkRate,
		GapDecisions:    gapDec,
		DoneTaskIDs:     len(doneTasks),
		GapRate:         gapRate,
	}
}

func renderDogfoodDefault(w io.Writer, m dogfoodMetrics) {
	fmt.Fprintf(w, "Dogfood metrics — last %dd (%.1f weeks)\n", m.WindowDays, m.WeeksInWindow)
	fmt.Fprintln(w, strings.Repeat("─", 48))

	// Velocity
	velIcon := velocityIcon(m.Velocity)
	fmt.Fprintf(w, "  Velocity:       %s %.1f tasks/week  (%d done)\n",
		velIcon, m.Velocity, m.DoneTasks)

	// Rework rate
	reworkIcon, reworkVerdict := reworkRateVerdict(m.ReworkRate, m.TotalDecisions)
	if m.TotalDecisions == 0 {
		fmt.Fprintf(w, "  Rework rate:    %s %s\n", reworkIcon, reworkVerdict)
	} else {
		fmt.Fprintf(w, "  Rework rate:    %s %.0f%%  — %s  (%d/%d task decisions)\n",
			reworkIcon, m.ReworkRate*100, reworkVerdict, m.ReworkDecisions, m.TotalDecisions)
	}

	// Gap rate
	gapIcon, gapVerdict := gapRateVerdict(m.GapRate, m.DoneTaskIDs)
	if m.DoneTaskIDs == 0 {
		fmt.Fprintf(w, "  Gap rate:       %s %s\n", gapIcon, gapVerdict)
	} else {
		fmt.Fprintf(w, "  Gap rate:       %s %.0f%%  — %s  (%d gaps in %d done tasks)\n",
			gapIcon, m.GapRate*100, gapVerdict, m.GapDecisions, m.DoneTaskIDs)
	}
}

func renderDogfoodCompact(w io.Writer, m dogfoodMetrics) {
	velIcon := velocityIcon(m.Velocity)
	reworkIcon, _ := reworkRateVerdict(m.ReworkRate, m.TotalDecisions)
	gapIcon, _ := gapRateVerdict(m.GapRate, m.DoneTaskIDs)
	fmt.Fprintf(w, "dogfood %dd — velocity: %s %.1f/wk  rework: %s %.0f%%  gaps: %s %.0f%%\n",
		m.WindowDays, velIcon, m.Velocity,
		reworkIcon, m.ReworkRate*100,
		gapIcon, m.GapRate*100)
}

func velocityIcon(tasksPerWeek float64) string {
	switch {
	case tasksPerWeek >= 5:
		return "✓"
	case tasksPerWeek >= 2:
		return "·"
	default:
		return "⚠"
	}
}

func reworkRateVerdict(rate float64, total int) (icon, verdict string) {
	switch {
	case total == 0:
		return "·", "no task decisions in window — no signal"
	case rate >= reworkRateFreezeFrac:
		return "✗", "high iteration — design before starting next task"
	case rate >= reworkRateWarnFrac:
		return "⚠", "elevated iteration — consider better up-front spec"
	default:
		return "✓", "healthy"
	}
}

func gapRateVerdict(rate float64, total int) (icon, verdict string) {
	switch {
	case total == 0:
		return "·", "no closed tasks in window — no signal"
	case rate >= 0.50:
		return "⚠", "half of closures had known gaps — track follow-ups"
	default:
		return "✓", "acceptable gap rate"
	}
}
