// Package cmd — unit tests for the reopen-rate verdict + since-parser in
// `gg audit trends` (TASK-223). Both are pure functions designed for
// table-driven coverage without a live store.
package cmd

import (
	"strings"
	"testing"
)

// emptyWindow — zero closes must not print a divide-by-zero and must clearly
// state "no signal" so a human reviewing the output can tell the metric is
// vacuous rather than green.
func TestReopenRateVerdict_EmptyWindow_NoSignal(t *testing.T) {
	icon, verdict := reopenRateVerdict(0, 0)
	if icon != "·" {
		t.Errorf("empty window icon: got %q, want %q", icon, "·")
	}
	if !strings.Contains(verdict, "no signal") {
		t.Errorf("empty window verdict should say 'no signal', got %q", verdict)
	}
}

// healthy range — under the 20% warn threshold should display the green ✓.
func TestReopenRateVerdict_HealthyRange(t *testing.T) {
	cases := []float64{0.0, 5.0, 10.0, 19.9}
	for _, rate := range cases {
		icon, verdict := reopenRateVerdict(rate, 10)
		if icon != "✓" {
			t.Errorf("rate=%.1f%% should be healthy ✓, got %q / %q", rate, icon, verdict)
		}
	}
}

// warn range — 20% to 39.9% should tell the caller to stabilise before
// shipping new scope. This is the band onelift is in.
func TestReopenRateVerdict_WarnRange_Stabilise(t *testing.T) {
	cases := []float64{20.0, 30.0, 39.9}
	for _, rate := range cases {
		icon, verdict := reopenRateVerdict(rate, 10)
		if icon != "⚠" {
			t.Errorf("rate=%.1f%% should warn ⚠, got %q", rate, icon)
		}
		if !strings.Contains(verdict, "stabilise") {
			t.Errorf("rate=%.1f%% verdict should mention 'stabilise', got %q", rate, verdict)
		}
	}
}

// freeze range — 40%+ is the hard stop signal: defer new features entirely.
// This is the band that catches runaway premature-closure projects.
func TestReopenRateVerdict_FreezeRange_HardStop(t *testing.T) {
	cases := []float64{40.0, 55.0, 100.0}
	for _, rate := range cases {
		icon, verdict := reopenRateVerdict(rate, 10)
		if icon != "✗" {
			t.Errorf("rate=%.1f%% should fail ✗, got %q", rate, icon)
		}
		if !strings.Contains(verdict, "feature-freeze") {
			t.Errorf("rate=%.1f%% verdict should demand feature-freeze, got %q", rate, verdict)
		}
	}
}

// parseSinceDays — human input "7d" / "14d" / "30" must all resolve to the
// correct integer day count. Empty string is the default (7).
func TestParseSinceDays_ValidInputs(t *testing.T) {
	cases := map[string]int{
		"":     7,
		"7d":   7,
		"14d":  14,
		"30":   30,
		" 7d ": 7,
	}
	for in, want := range cases {
		got, err := parseSinceDays(in)
		if err != nil {
			t.Errorf("parseSinceDays(%q): unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseSinceDays(%q): got %d, want %d", in, got, want)
		}
	}
}

// parseSinceDays — malformed or non-positive input must reject so the
// command does not silently run against 0 or a garbage window.
func TestParseSinceDays_RejectsBadInputs(t *testing.T) {
	cases := []string{"abc", "-3d", "0", "0d"}
	for _, in := range cases {
		if _, err := parseSinceDays(in); err == nil {
			t.Errorf("parseSinceDays(%q) should have errored", in)
		}
	}
}
