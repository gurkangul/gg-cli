package projectstate

import (
	"testing"
	"time"
)

func TestAppendBypass_RoundTripsEntry(t *testing.T) {
	dir := t.TempDir()
	if err := AppendBypass(dir, "pre-task-done", "TASK-500", "claude-code", "TASK-500: emergency ship", "TASK-500"); err != nil {
		t.Fatalf("append: %v", err)
	}
	s, err := Read(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(s.BypassLog) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(s.BypassLog))
	}
	e := s.BypassLog[0]
	if e.Gate != "pre-task-done" || e.TaskID != "TASK-500" || e.Actor != "claude-code" {
		t.Fatalf("entry fields wrong: %+v", e)
	}
	if e.TS == "" {
		t.Fatal("TS must be set by AppendBypass")
	}
	if e.Rationale != "TASK-500: emergency ship" {
		t.Fatalf("Rationale not stored: %q", e.Rationale)
	}
	if e.RationaleTaskID != "TASK-500" {
		t.Fatalf("RationaleTaskID not stored: %q", e.RationaleTaskID)
	}
}

func TestAppendBypass_PreservesLastSeenCLIVersion(t *testing.T) {
	// Regression for the session-start bug where Write(State{LSCV:...}) would
	// drop BypassLog. Here we write LSCV first, then append — the append must
	// not blow away LSCV.
	dir := t.TempDir()
	if err := Write(dir, State{LastSeenCLIVersion: "v0.2.3"}); err != nil {
		t.Fatalf("write initial: %v", err)
	}
	if err := AppendBypass(dir, "gsd-guard", "", "", "", ""); err != nil {
		t.Fatalf("append: %v", err)
	}
	s, err := Read(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if s.LastSeenCLIVersion != "v0.2.3" {
		t.Fatalf("LastSeenCLIVersion clobbered: %q", s.LastSeenCLIVersion)
	}
	if len(s.BypassLog) != 1 {
		t.Fatalf("expected 1 bypass entry, got %d", len(s.BypassLog))
	}
}

func TestListBypassesSince_FiltersByWindow(t *testing.T) {
	dir := t.TempDir()
	// Write two entries — one old (8 days ago), one fresh.
	oldTS := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	freshTS := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	s := State{BypassLog: []BypassEntry{
		{TS: oldTS, Gate: "gsd-guard"},
		{TS: freshTS, Gate: "pre-task-done", TaskID: "TASK-600"},
	}}
	if err := Write(dir, s); err != nil {
		t.Fatalf("write: %v", err)
	}

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	got, err := ListBypassesSince(dir, cutoff)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry within 7d, got %d", len(got))
	}
	if got[0].TaskID != "TASK-600" {
		t.Fatalf("wrong entry survived filter: %+v", got[0])
	}
}

func TestListBypassesSince_EmptyStateReturnsNil(t *testing.T) {
	dir := t.TempDir()
	got, err := ListBypassesSince(dir, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("list on missing state should succeed, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil slice on empty state, got %v", got)
	}
}
