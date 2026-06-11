package cmd

import (
	"fmt"
	"os"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/outbox"
)

// doctorJSON is the stable machine-readable payload emitted by
// `gg doctor --json`. Agents parse this instead of regexing the human report.
// The shape is intentionally flat and additive: new keys may be appended, but
// existing keys and their types are part of the stability contract.
type doctorJSON struct {
	OK        bool          `json:"ok"`       // true when problems == 0
	Problems  int           `json:"problems"` // count of failing checks
	Checks    []doctorCheck `json:"checks"`   // every check: label/status/detail
	Summary   doctorSummary `json:"summary"`  // pass/warn/fail tallies
	Outbox    int           `json:"outbox_pending"`
	Drift     int           `json:"artifact_drift"`
	TraceOn   bool          `json:"trace_enabled"`
	IndexHook bool          `json:"index_hooks_installed"`
	Linked    int           `json:"linked_projects"`
}

type doctorSummary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

// buildDoctorJSON converts an accumulated doctorReport into the JSON payload.
// It re-derives the same advisory signals the human tips section uses (outbox
// backlog, trace state, linked-project count) so the JSON and text views agree.
func buildDoctorJSON(report *doctorReport, driftCount int) doctorJSON {
	var sum doctorSummary
	for _, c := range report.checks {
		switch c.Status {
		case "pass":
			sum.Pass++
		case "warn":
			sum.Warn++
		case "fail":
			sum.Fail++
		}
	}

	out := doctorJSON{
		OK:        report.problems == 0,
		Problems:  report.problems,
		Checks:    report.checks,
		Summary:   sum,
		Outbox:    doctorOutboxPending(),
		Drift:     driftCount,
		TraceOn:   traceEnabled(),
		IndexHook: doctorIndexHooksInstalled(),
		Linked:    doctorLinkedProjectCount(),
	}
	if out.Checks == nil {
		out.Checks = []doctorCheck{}
	}
	return out
}

// renderDoctorTips prints the advisory discoverability one-liners (TASK-503).
// The outbox backlog is already surfaced as a first-class doctor check
// (doctorCheckOutbox), so the only tip added here is the GG_TRACE hint, shown
// only when tracing is OFF so opted-in users are never nagged. Never called
// under --json (tips are human-only; JSON exposes the same signals as fields).
func renderDoctorTips(_ *doctorReport) {
	if !traceEnabled() {
		fmt.Println("\nTip: set GG_TRACE=1 to record perf spans (.gg/traces/).")
	}
}

// doctorOutboxPending counts pending derived-store writes in the outbox.
// Best-effort: any error yields 0 so the tip/JSON simply omit the signal.
func doctorOutboxPending() int {
	ggDir, err := config.GGDir()
	if err != nil {
		return 0
	}
	entries, err := outbox.List(ggDir)
	if err != nil {
		return 0
	}
	return len(entries)
}

// traceEnabled reports whether GG_TRACE=1 perf-span recording is active.
func traceEnabled() bool {
	return os.Getenv("GG_TRACE") == "1"
}

// doctorIndexHooksInstalled reports index-hook install state for the JSON
// payload. Best-effort: not-a-repo / no-root yields false.
func doctorIndexHooksInstalled() bool {
	root, err := config.FindRoot()
	if err != nil {
		return false
	}
	return indexHooksInstalled(root)
}

// doctorLinkedProjectCount returns the number of configured linked projects.
// Best-effort: a config load failure yields 0.
func doctorLinkedProjectCount() int {
	cfg, err := config.Load()
	if err != nil {
		return 0
	}
	return len(cfg.LinkedProjects)
}
