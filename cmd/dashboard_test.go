package cmd

import (
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/orchestrator/locks"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
)

// TestEffectivePaneState covers the state-promotion logic.
func TestEffectivePaneState(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name string
		pane spawn.WorkerPane
		want spawn.WorkerState
	}{
		{
			name: "waiting-on-master is never auto-demoted",
			pane: spawn.WorkerPane{
				State:         spawn.WorkerStateWaiting,
				LastHeartbeat: now.Add(-10 * time.Minute), // stale heartbeat
			},
			want: spawn.WorkerStateWaiting,
		},
		{
			name: "fresh heartbeat stays working",
			pane: spawn.WorkerPane{
				State:         spawn.WorkerStateWorking,
				LastHeartbeat: now.Add(-30 * time.Second),
			},
			want: spawn.WorkerStateWorking,
		},
		{
			name: "stale heartbeat promotes working→idle",
			pane: spawn.WorkerPane{
				State:         spawn.WorkerStateWorking,
				LastHeartbeat: now.Add(-10 * time.Minute),
			},
			want: spawn.WorkerStateIdle,
		},
		{
			name: "zero heartbeat promotes working→idle",
			pane: spawn.WorkerPane{
				State: spawn.WorkerStateWorking,
				// LastHeartbeat zero value
			},
			want: spawn.WorkerStateIdle,
		},
		{
			name: "empty state with fresh heartbeat → working",
			pane: spawn.WorkerPane{
				LastHeartbeat: now.Add(-1 * time.Second),
			},
			want: spawn.WorkerStateWorking,
		},
		{
			name: "empty state with stale heartbeat → idle",
			pane: spawn.WorkerPane{
				LastHeartbeat: now.Add(-10 * time.Minute),
			},
			want: spawn.WorkerStateIdle,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := effectivePaneState(&tc.pane)
			if got != tc.want {
				t.Errorf("effectivePaneState = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHasCollisionRisk verifies path-overlap detection across claims.
func TestHasCollisionRisk(t *testing.T) {
	claims := []locks.Claim{
		{TaskID: "TASK-001", Paths: []string{"internal/store/store.go", "cmd/status.go"}},
		{TaskID: "TASK-002", Paths: []string{"internal/store/store.go", "cmd/record.go"}},
		{TaskID: "TASK-003", Paths: []string{"cmd/dashboard.go"}},
	}

	// TASK-001 and TASK-002 share internal/store/store.go → both have risk.
	if !hasCollisionRisk("TASK-001", claims) {
		t.Error("TASK-001 should have collision risk with TASK-002")
	}
	if !hasCollisionRisk("TASK-002", claims) {
		t.Error("TASK-002 should have collision risk with TASK-001")
	}
	// TASK-003 has no overlap with others.
	if hasCollisionRisk("TASK-003", claims) {
		t.Error("TASK-003 should NOT have collision risk")
	}
	// Unknown task has no claims.
	if hasCollisionRisk("TASK-999", claims) {
		t.Error("TASK-999 (no claim) should NOT have collision risk")
	}
}

// TestHasCollisionRiskNoClaims verifies an empty claims slice is safe.
func TestHasCollisionRiskNoClaims(t *testing.T) {
	if hasCollisionRisk("TASK-001", nil) {
		t.Error("no claims should mean no collision risk")
	}
}

// TestStateColor verifies each WorkerState maps to a distinct non-empty color.
func TestStateColor(t *testing.T) {
	colors := map[spawn.WorkerState]string{
		spawn.WorkerStateWorking: stateColor(spawn.WorkerStateWorking),
		spawn.WorkerStateIdle:    stateColor(spawn.WorkerStateIdle),
		spawn.WorkerStateWaiting: stateColor(spawn.WorkerStateWaiting),
	}
	seen := map[string]spawn.WorkerState{}
	for state, color := range colors {
		if color == "" {
			t.Errorf("stateColor(%q) returned empty string", state)
		}
		if prior, dup := seen[color]; dup {
			t.Errorf("stateColor(%q) == stateColor(%q) — colors must be distinct", state, prior)
		}
		seen[color] = state
	}
}

// TestDashTruncate verifies truncation behavior.
func TestDashTruncate(t *testing.T) {
	if got := dashTruncate("hello", 10); got != "hello" {
		t.Errorf("dashTruncate no-op: got %q", got)
	}
	if got := dashTruncate("hello world", 5); got != "hell…" {
		t.Errorf("dashTruncate truncated: got %q", got)
	}
	if got := dashTruncate("x", 1); got != "x" {
		t.Errorf("dashTruncate max=1 single rune: got %q", got)
	}
}
