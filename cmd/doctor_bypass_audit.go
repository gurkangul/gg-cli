package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/projectstate"
)

// runDoctorBypassAudit prints every GG_ENFORCEMENT=off event observed in the
// configured window. The source of truth is ~/.gg/projects/<id>/state.json,
// written by emitGuardSkipEvent every time a gate skipped.
func runDoctorBypassAudit(since string) error {
	cfg, err := config.Load()
	if err != nil {
		return configErr("run `gg init` first: " + err.Error())
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return err
	}
	cutoff, err := parseBypassWindow(since)
	if err != nil {
		return err
	}

	entries, err := projectstate.ListBypassesSince(runtimeDir, cutoff)
	if err != nil {
		return fmt.Errorf("read bypass log: %w", err)
	}

	fmt.Println("Bypass Audit")
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("  window: since %s (cutoff %s)\n", since, cutoff.UTC().Format(time.RFC3339))
	fmt.Printf("  source: %s/state.json\n", runtimeDir)
	fmt.Println(strings.Repeat("─", 70))

	if len(entries) == 0 {
		fmt.Println("No bypass events recorded in this window. ✓")
		return nil
	}

	// Per-gate histogram so the summary answers "which gate is leaking?"
	// without the reader doing arithmetic on the full list.
	perGate := map[string]int{}
	for _, e := range entries {
		perGate[e.Gate]++
	}
	fmt.Println("Summary:")
	for g, n := range perGate {
		fmt.Printf("  %-40s %d\n", g, n)
	}
	fmt.Println()

	fmt.Printf("%-20s  %-32s  %-18s  %-16s  %s\n", "TS", "GATE", "TASK", "ACTOR", "RATIONALE")
	fmt.Println(strings.Repeat("─", 110))
	for _, e := range entries {
		task := e.TaskID
		if task == "" {
			task = "—"
		}
		actor := e.Actor
		if actor == "" {
			actor = "—"
		}
		rationale := e.Rationale
		if rationale == "" {
			rationale = "—"
		} else if len(rationale) > 40 {
			rationale = rationale[:37] + "…"
		}
		fmt.Printf("%-20s  %-32s  %-18s  %-16s  %s\n", shortDate(e.TS), e.Gate, task, actor, rationale)
	}
	fmt.Println(strings.Repeat("─", 110))
	fmt.Printf("Total: %d bypass event(s).\n", len(entries))
	return nil
}

// parseBypassWindow accepts a human-friendly window ("7d", "24h", "30d") or
// an RFC3339 timestamp and returns the absolute cutoff for filtering.
// Rejects unknown formats rather than silently treating them as zero, so the
// operator gets a clear error when they mistype.
func parseBypassWindow(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now().Add(-7 * 24 * time.Hour), nil
	}
	// RFC3339 passthrough: if the string parses as an absolute time, use it.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Relative suffix: 7d, 24h, 30d, 60m.
	if len(s) >= 2 {
		unit := s[len(s)-1]
		nStr := s[:len(s)-1]
		n, err := strconv.Atoi(nStr)
		if err == nil {
			var d time.Duration
			switch unit {
			case 'd':
				d = time.Duration(n) * 24 * time.Hour
			case 'h':
				d = time.Duration(n) * time.Hour
			case 'm':
				d = time.Duration(n) * time.Minute
			default:
				return time.Time{}, fmt.Errorf("unknown time unit %q — use d, h, m or RFC3339", string(unit))
			}
			return time.Now().Add(-d), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable --bypass-since %q — use 7d, 24h, 30d, or RFC3339", s)
}
