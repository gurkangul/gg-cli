// Package cmd — unit tests for gg metrics dogfood (TASK-303).
// Pure-function coverage of computeDogfoodMetrics, verdict helpers, and
// renderers. No live store needed.
package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/store"
)

// nowMinus returns an RFC3339 timestamp N days before now, used to build
// fixture tasks/decisions within the lookback window.
func nowMinus(days int) string {
	return time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
}

// ── computeDogfoodMetrics ────────────────────────────────────────────────────

// TestComputeDogfood_EmptyStore — no tasks, no decisions → zeroed metrics,
// no division-by-zero panic, velocity=0.
func TestComputeDogfood_EmptyStore(t *testing.T) {
	m := computeDogfoodMetrics(14, nil, nil)
	if m.DoneTasks != 0 {
		t.Errorf("DoneTasks: got %d, want 0", m.DoneTasks)
	}
	if m.Velocity != 0 {
		t.Errorf("Velocity: got %.2f, want 0", m.Velocity)
	}
	if m.ReworkRate != 0 {
		t.Errorf("ReworkRate: got %.2f, want 0", m.ReworkRate)
	}
}

// TestComputeDogfood_VelocityCount — 7 done tasks in a 14d window → 0.5/wk.
func TestComputeDogfood_VelocityCount(t *testing.T) {
	tasks := make([]store.Task, 7)
	for i := range tasks {
		tasks[i] = store.Task{
			ID:        "TASK-" + string(rune('0'+i+1)),
			Status:    "done",
			CreatedAt: nowMinus(3), // within 14d window
		}
	}
	m := computeDogfoodMetrics(14, tasks, nil)
	if m.DoneTasks != 7 {
		t.Errorf("DoneTasks: got %d, want 7", m.DoneTasks)
	}
	// 7 tasks / 2 weeks = 3.5/wk
	want := 7.0 / 2.0
	if m.Velocity != want {
		t.Errorf("Velocity: got %.2f, want %.2f", m.Velocity, want)
	}
}

// TestComputeDogfood_TasksOutsideWindow — tasks older than the window should
// be excluded from the velocity count.
func TestComputeDogfood_TasksOutsideWindow(t *testing.T) {
	tasks := []store.Task{
		{ID: "TASK-1", Status: "done", CreatedAt: nowMinus(3)},  // inside
		{ID: "TASK-2", Status: "done", CreatedAt: nowMinus(20)}, // outside 14d
	}
	m := computeDogfoodMetrics(14, tasks, nil)
	if m.DoneTasks != 1 {
		t.Errorf("DoneTasks: got %d, want 1 (outside-window task filtered)", m.DoneTasks)
	}
}

// TestComputeDogfood_ReworkDetection — decisions with "rework" in text linked
// to tasks should be counted against total task decisions.
func TestComputeDogfood_ReworkDetection(t *testing.T) {
	decisions := []store.Decision{
		{ID: "d1", TaskID: "TASK-1", Text: "TASK-1 rework-2: fixed AC-1 gap", CreatedAt: nowMinus(2)},
		{ID: "d2", TaskID: "TASK-1", Text: "TASK-1 approved", CreatedAt: nowMinus(1)},
		{ID: "d3", TaskID: "TASK-2", Text: "TASK-2 design decision", CreatedAt: nowMinus(5)},
	}
	m := computeDogfoodMetrics(14, nil, decisions)
	if m.TotalDecisions != 3 {
		t.Errorf("TotalDecisions: got %d, want 3", m.TotalDecisions)
	}
	if m.ReworkDecisions != 1 {
		t.Errorf("ReworkDecisions: got %d, want 1", m.ReworkDecisions)
	}
	wantRate := 1.0 / 3.0
	if m.ReworkRate < wantRate-0.001 || m.ReworkRate > wantRate+0.001 {
		t.Errorf("ReworkRate: got %.4f, want %.4f", m.ReworkRate, wantRate)
	}
}

// TestComputeDogfood_GapDetection — decisions with "accept-with-gap" should
// increment GapDecisions; gap rate uses done tasks as denominator.
func TestComputeDogfood_GapDetection(t *testing.T) {
	tasks := []store.Task{
		{ID: "TASK-1", Status: "done", CreatedAt: nowMinus(2)},
		{ID: "TASK-2", Status: "done", CreatedAt: nowMinus(3)},
	}
	decisions := []store.Decision{
		{ID: "d1", TaskID: "TASK-1", Text: "TASK-1 accept-with-gap: shipped with known gap", CreatedAt: nowMinus(2)},
		{ID: "d2", TaskID: "TASK-2", Text: "TASK-2 closed cleanly", CreatedAt: nowMinus(3)},
	}
	m := computeDogfoodMetrics(14, tasks, decisions)
	if m.GapDecisions != 1 {
		t.Errorf("GapDecisions: got %d, want 1", m.GapDecisions)
	}
	if m.DoneTaskIDs != 2 {
		t.Errorf("DoneTaskIDs: got %d, want 2", m.DoneTaskIDs)
	}
	wantGapRate := 0.5
	if m.GapRate < wantGapRate-0.001 || m.GapRate > wantGapRate+0.001 {
		t.Errorf("GapRate: got %.4f, want %.4f", m.GapRate, wantGapRate)
	}
}

// TestComputeDogfood_DecisionsOutsideWindow — decisions older than the window
// should not count toward rework or gap signals.
func TestComputeDogfood_DecisionsOutsideWindow(t *testing.T) {
	decisions := []store.Decision{
		{ID: "d1", TaskID: "TASK-1", Text: "rework-3 old cycle", CreatedAt: nowMinus(30)}, // outside 14d
		{ID: "d2", TaskID: "TASK-1", Text: "TASK-1 approved", CreatedAt: nowMinus(2)},     // inside
	}
	m := computeDogfoodMetrics(14, nil, decisions)
	if m.TotalDecisions != 1 {
		t.Errorf("TotalDecisions: got %d, want 1 (old decision excluded)", m.TotalDecisions)
	}
	if m.ReworkDecisions != 0 {
		t.Errorf("ReworkDecisions: got %d, want 0 (rework decision outside window)", m.ReworkDecisions)
	}
}

// TestComputeDogfood_DecisionsWithoutTaskID — decisions not linked to a task
// should not count in rework/gap totals.
func TestComputeDogfood_DecisionsWithoutTaskID(t *testing.T) {
	decisions := []store.Decision{
		{ID: "d1", TaskID: "", Text: "rework pattern noted in arch discussion", CreatedAt: nowMinus(2)},
		{ID: "d2", TaskID: "TASK-1", Text: "TASK-1 accepted", CreatedAt: nowMinus(1)},
	}
	m := computeDogfoodMetrics(14, nil, decisions)
	if m.TotalDecisions != 1 {
		t.Errorf("TotalDecisions: got %d, want 1 (unlinked decision excluded)", m.TotalDecisions)
	}
}

// ── Verdict helpers ──────────────────────────────────────────────────────────

// TestReworkRateVerdict_Empty — zero decisions yields the no-signal result.
func TestReworkRateVerdict_Empty(t *testing.T) {
	icon, verdict := reworkRateVerdict(0, 0)
	if icon != "·" {
		t.Errorf("empty rework icon: got %q, want %q", icon, "·")
	}
	if !strings.Contains(verdict, "no signal") {
		t.Errorf("empty rework verdict should say 'no signal', got %q", verdict)
	}
}

// TestReworkRateVerdict_Healthy — below warn threshold returns green ✓.
func TestReworkRateVerdict_Healthy(t *testing.T) {
	icon, _ := reworkRateVerdict(0.10, 10)
	if icon != "✓" {
		t.Errorf("10%% rework: expected ✓, got %q", icon)
	}
}

// TestReworkRateVerdict_Warn — between warn and freeze shows ⚠.
func TestReworkRateVerdict_Warn(t *testing.T) {
	icon, _ := reworkRateVerdict(0.25, 10)
	if icon != "⚠" {
		t.Errorf("25%% rework: expected ⚠, got %q", icon)
	}
}

// TestReworkRateVerdict_Freeze — above freeze threshold shows ✗.
func TestReworkRateVerdict_Freeze(t *testing.T) {
	icon, verdict := reworkRateVerdict(0.50, 10)
	if icon != "✗" {
		t.Errorf("50%% rework: expected ✗, got %q", icon)
	}
	if !strings.Contains(verdict, "design before") {
		t.Errorf("50%% verdict should mention design, got %q", verdict)
	}
}

// TestVelocityIcon — sanity-check icon thresholds.
func TestVelocityIcon(t *testing.T) {
	cases := []struct {
		tpw  float64
		want string
	}{
		{6.0, "✓"},
		{5.0, "✓"},
		{2.0, "·"},
		{4.9, "·"},
		{0.0, "⚠"},
		{1.9, "⚠"},
	}
	for _, c := range cases {
		got := velocityIcon(c.tpw)
		if got != c.want {
			t.Errorf("velocityIcon(%.1f): got %q, want %q", c.tpw, got, c.want)
		}
	}
}

// ── Renderers ────────────────────────────────────────────────────────────────

// TestRenderDogfoodCompact_Format — compact line must contain the three metric
// labels so an agent can parse it with a simple grep.
func TestRenderDogfoodCompact_Format(t *testing.T) {
	var sb strings.Builder
	m := dogfoodMetrics{
		WindowDays:    14,
		WeeksInWindow: 2.0,
		Velocity:      3.5,
		ReworkRate:    0.25,
		TotalDecisions: 8,
		GapRate:       0.33,
		DoneTaskIDs:   3,
	}
	renderDogfoodCompact(&sb, m)
	out := sb.String()
	for _, want := range []string{"14d", "velocity:", "rework:", "gaps:"} {
		if !strings.Contains(out, want) {
			t.Errorf("compact output missing %q: %q", want, out)
		}
	}
}

// TestRenderDogfoodDefault_ContainsLabels — default output must include all
// three metric labels so a human can read the report.
func TestRenderDogfoodDefault_ContainsLabels(t *testing.T) {
	var sb strings.Builder
	m := dogfoodMetrics{
		WindowDays:    14,
		WeeksInWindow: 2.0,
		DoneTasks:     7,
		Velocity:      3.5,
	}
	renderDogfoodDefault(&sb, m)
	out := sb.String()
	for _, want := range []string{"Velocity", "Rework rate", "Gap rate"} {
		if !strings.Contains(out, want) {
			t.Errorf("default output missing %q: %q", want, out)
		}
	}
}
