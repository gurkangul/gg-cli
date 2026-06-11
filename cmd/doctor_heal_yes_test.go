// Package cmd — TASK-499 tests: the --yes / GG_YES non-interactive bypass for
// `gg doctor --heal`. These assert the CLAUDE.md contract that a state-changing
// command runs from CI/scripts without reading stdin, while a non-TTY run
// without the bypass takes the safe default (skip) instead of hanging.
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gurkangul/gg-cli/internal/templates"
)

// seedDriftedRules writes a RULES.md that differs from the embedded template so
// the heal flow's template-drift branch fires.
func seedDriftedRules(t *testing.T, ggDir string) string {
	t.Helper()
	rulesPath := filepath.Join(ggDir, "RULES.md")
	if err := os.WriteFile(rulesPath, []byte("# stale rules — drifted\n"), 0o644); err != nil {
		t.Fatalf("write drifted RULES.md: %v", err)
	}
	return rulesPath
}

// TestDoctorHeal_YesFlag_RerendersNonInteractive verifies that with the doctor
// --yes flag set, runDoctorHeal re-renders RULES.md to match the template
// without prompting (stdin is /dev/null / closed in `go test`, so any stdin
// read would either hang or skip — re-render proves the auto-accept path ran).
func TestDoctorHeal_YesFlag_RerendersNonInteractive(t *testing.T) {
	ggDir := setupGGDir(t)
	rulesPath := seedDriftedRules(t, ggDir)

	prev := doctorWipeBrainYes
	doctorWipeBrainYes = true
	t.Cleanup(func() { doctorWipeBrainYes = prev })

	if err := runDoctorHeal(); err != nil {
		t.Fatalf("runDoctorHeal with --yes: %v", err)
	}

	got, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("read RULES.md after heal: %v", err)
	}
	if string(got) != templates.RulesMD {
		t.Fatalf("RULES.md was not re-rendered from template under --yes")
	}
	if _, err := os.Stat(rulesPath + ".bak"); err != nil {
		t.Fatalf("expected backup %s.bak to exist: %v", rulesPath, err)
	}
}

// TestDoctorHeal_GGYesEnv_RerendersNonInteractive verifies the GG_YES=1 env
// bypass with the --yes flag unset.
func TestDoctorHeal_GGYesEnv_RerendersNonInteractive(t *testing.T) {
	ggDir := setupGGDir(t)
	rulesPath := seedDriftedRules(t, ggDir)

	prev := doctorWipeBrainYes
	doctorWipeBrainYes = false
	t.Cleanup(func() { doctorWipeBrainYes = prev })
	t.Setenv("GG_YES", "1")

	if err := runDoctorHeal(); err != nil {
		t.Fatalf("runDoctorHeal with GG_YES=1: %v", err)
	}

	got, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("read RULES.md after heal: %v", err)
	}
	if string(got) != templates.RulesMD {
		t.Fatalf("RULES.md was not re-rendered from template under GG_YES=1")
	}
}

// TestDoctorHeal_NonTTY_NoBypass_SkipsAndDoesNotHang verifies the safe default:
// a non-TTY run (the `go test` harness) without --yes/GG_YES does NOT read
// stdin, does NOT re-render, and returns promptly. If the code read stdin here
// the test would hang until timeout — its completion is the assertion.
func TestDoctorHeal_NonTTY_NoBypass_SkipsAndDoesNotHang(t *testing.T) {
	ggDir := setupGGDir(t)
	rulesPath := seedDriftedRules(t, ggDir)
	original := "# stale rules — drifted\n"

	prev := doctorWipeBrainYes
	doctorWipeBrainYes = false
	t.Cleanup(func() { doctorWipeBrainYes = prev })
	t.Setenv("GG_YES", "")

	if err := runDoctorHeal(); err != nil {
		t.Fatalf("runDoctorHeal non-TTY default: %v", err)
	}

	got, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("read RULES.md after heal: %v", err)
	}
	if string(got) != original {
		t.Fatalf("RULES.md was modified without --yes/GG_YES on a non-TTY (should be untouched)")
	}
	if _, err := os.Stat(rulesPath + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("expected NO backup on safe-default skip; stat err=%v", err)
	}
}

// TestAutoConfirmYes covers the flag/env truth table that drives every
// state-changing doctor prompt (heal re-render, Makefile test-tier).
func TestAutoConfirmYes(t *testing.T) {
	cases := []struct {
		name string
		flag bool
		env  string
		want bool
	}{
		{"flag set", true, "", true},
		{"env 1", false, "1", true},
		{"env true", false, "true", true},
		{"env yes", false, "yes", true},
		{"env on", false, "on", true},
		{"env empty", false, "", false},
		{"env zero", false, "0", false},
		{"env garbage", false, "nope", false},
	}
	prev := doctorWipeBrainYes
	t.Cleanup(func() { doctorWipeBrainYes = prev })
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doctorWipeBrainYes = tc.flag
			t.Setenv("GG_YES", tc.env)
			if got := autoConfirmYes(); got != tc.want {
				t.Fatalf("autoConfirmYes(flag=%v, GG_YES=%q) = %v, want %v", tc.flag, tc.env, got, tc.want)
			}
		})
	}
}
