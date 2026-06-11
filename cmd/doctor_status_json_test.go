// Package cmd — TASK-503 tests: --json on gg status/doctor plus the
// discoverability surfacing (linked projects, outbox, GG_TRACE tip).
package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/outbox"
)

// TestBuildDoctorJSON_ValidAndKeyed verifies buildDoctorJSON produces a payload
// that marshals to valid JSON and carries every contract key, and that the
// pass/warn/fail summary tallies match the recorded checks.
func TestBuildDoctorJSON_ValidAndKeyed(t *testing.T) {
	setupGGDir(t) // isolates HOME + cwd so config/outbox lookups are sandboxed

	report := &doctorReport{}
	report.ok("config.yaml", "valid")
	report.ok("project_id", "abc")
	report.warn("memgraph", "not configured")
	report.fail("qdrant", "unreachable")

	payload := buildDoctorJSON(report, 2)

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal doctor json: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("doctor json not parseable: %v\n%s", err, raw)
	}

	wantKeys := []string{
		"ok", "problems", "checks", "summary",
		"outbox_pending", "artifact_drift", "trace_enabled",
		"index_hooks_installed", "linked_projects",
	}
	for _, k := range wantKeys {
		if _, ok := decoded[k]; !ok {
			t.Errorf("doctor json missing key %q; got keys %v", k, keysOf(decoded))
		}
	}

	if payload.OK {
		t.Error("OK should be false when a check failed")
	}
	if payload.Problems != 1 {
		t.Errorf("Problems = %d, want 1", payload.Problems)
	}
	if payload.Summary.Pass != 2 || payload.Summary.Warn != 1 || payload.Summary.Fail != 1 {
		t.Errorf("summary tallies wrong: %+v", payload.Summary)
	}
	if payload.Drift != 2 {
		t.Errorf("Drift = %d, want 2", payload.Drift)
	}
	if len(payload.Checks) != 4 {
		t.Errorf("checks len = %d, want 4", len(payload.Checks))
	}
}

// TestBuildDoctorJSON_EmptyChecksNotNull verifies the checks array serialises
// as [] (not null) so JSON consumers can always iterate it.
func TestBuildDoctorJSON_EmptyChecksNotNull(t *testing.T) {
	setupGGDir(t)
	raw, err := json.Marshal(buildDoctorJSON(&doctorReport{}, 0))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"checks":[]`) {
		t.Errorf("empty checks should serialise as [], got %s", raw)
	}
}

// TestDoctorReport_JSONModeSuppressesText verifies the report's ok/warn/fail
// methods record structured checks but print no human text when --json is on.
func TestDoctorReport_JSONModeSuppressesText(t *testing.T) {
	old := jsonOutput
	jsonOutput = true
	defer func() { jsonOutput = old }()

	report := &doctorReport{}
	out := captureStdout(t, func() {
		report.ok("a", "x")
		report.warn("b", "y")
		report.fail("c", "z")
	})
	if out != "" {
		t.Errorf("expected no stdout under --json, got %q", out)
	}
	if len(report.checks) != 3 {
		t.Errorf("checks not recorded under --json: %d", len(report.checks))
	}
	if report.problems != 1 {
		t.Errorf("problems = %d, want 1", report.problems)
	}
}

// TestRenderDoctorTips_TraceGating verifies the GG_TRACE tip fires only when
// tracing is OFF, so opted-in users are never nagged.
func TestRenderDoctorTips_TraceGating(t *testing.T) {
	setupGGDir(t)

	t.Run("off shows tip", func(t *testing.T) {
		t.Setenv("GG_TRACE", "")
		out := captureStdout(t, func() { renderDoctorTips(&doctorReport{}) })
		if !strings.Contains(out, "GG_TRACE=1") {
			t.Errorf("expected GG_TRACE tip when tracing off, got %q", out)
		}
	})

	t.Run("on suppresses tip", func(t *testing.T) {
		t.Setenv("GG_TRACE", "1")
		out := captureStdout(t, func() { renderDoctorTips(&doctorReport{}) })
		if strings.Contains(out, "GG_TRACE") {
			t.Errorf("expected no GG_TRACE tip when tracing on, got %q", out)
		}
	})
}

// TestTraceEnabled verifies the trace-state helper used by both the JSON
// payload and the human tip.
func TestTraceEnabled(t *testing.T) {
	t.Setenv("GG_TRACE", "1")
	if !traceEnabled() {
		t.Error("traceEnabled() = false with GG_TRACE=1")
	}
	t.Setenv("GG_TRACE", "0")
	if traceEnabled() {
		t.Error("traceEnabled() = true with GG_TRACE=0")
	}
	t.Setenv("GG_TRACE", "")
	if traceEnabled() {
		t.Error("traceEnabled() = true with GG_TRACE unset")
	}
}

// TestDoctorOutboxPending_OnlyWhenEntriesExist verifies the outbox-pending
// signal is 0 with no fixtures and N after writing fake entries.
func TestDoctorOutboxPending_OnlyWhenEntriesExist(t *testing.T) {
	ggDir := setupGGDir(t)

	if n := doctorOutboxPending(); n != 0 {
		t.Fatalf("expected 0 pending with empty outbox, got %d", n)
	}

	for i := 0; i < 2; i++ {
		if _, err := outbox.Write(ggDir, "full-index", map[string]string{"k": "v"}); err != nil {
			t.Fatalf("seed outbox: %v", err)
		}
	}
	if n := doctorOutboxPending(); n != 2 {
		t.Errorf("expected 2 pending after seeding, got %d", n)
	}
}

// TestDoctorLinkedProjectCount_OnlyWhenConfigured verifies the linked-project
// count is 0 for the bare fixture and N once linked_projects is configured.
func TestDoctorLinkedProjectCount_OnlyWhenConfigured(t *testing.T) {
	ggDir := setupGGDir(t)

	if n := doctorLinkedProjectCount(); n != 0 {
		t.Fatalf("expected 0 linked projects in bare fixture, got %d", n)
	}

	cfgWithLinks := ggConfig + `
linked_projects:
  - other-project-a
  - other-project-b
  - other-project-c
`
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfgWithLinks), 0o644); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if n := doctorLinkedProjectCount(); n != 3 {
		t.Errorf("expected 3 linked projects, got %d", n)
	}
}

// keysOf returns the map keys for error messages.
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
