package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

// TestCanonDriftReusesContextDetector proves `gg canon show` surfaces the same
// drift badge as `gg context` (TASK-499): canon text that claims a task is
// "done" while the live task is still pending must flag a conflict via the
// shared detectTextConflicts detector, and the rendered views must show it.
func TestCanonDriftReusesContextDetector(t *testing.T) {
	entries := []store.CanonEntry{
		{Area: "status", Text: "TÜM SERİ TAMAM — TASK-499 done and shipped"},
		{Area: "architecture", Text: "JSONL is source of truth; no task refs here"},
	}
	// Live ledger: TASK-499 is still pending — canon is fresher-but-wronger.
	taskStatus := map[string]string{"TASK-499": "pending"}

	var sources []conflictSource
	for _, e := range entries {
		sources = append(sources, conflictSource{"canon", e.Text})
	}
	conflicts := detectTextConflicts(sources, taskStatus)

	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d: %+v", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c.ID != "TASK-499" {
		t.Errorf("conflict ID = %q, want TASK-499", c.ID)
	}
	if c.SourceType != "canon" {
		t.Errorf("conflict SourceType = %q, want canon", c.SourceType)
	}
	if c.LiveStatus != "pending" {
		t.Errorf("conflict LiveStatus = %q, want pending", c.LiveStatus)
	}

	// Default view: CONFLICTS section + inline ⚠ live: marker on the drifting area.
	var def bytes.Buffer
	writeCanonView(&def, entries, nil, conflicts)
	defOut := def.String()
	if !strings.Contains(defOut, "⚡ [TASK-499] canon says resolved") {
		t.Errorf("default view missing conflict badge:\n%s", defOut)
	}
	if !strings.Contains(defOut, "## status  ⚠ live: TASK-499 pending") {
		t.Errorf("default view missing inline drift marker on the conflicting area:\n%s", defOut)
	}
	if strings.Contains(defOut, "## architecture  ⚠") {
		t.Errorf("non-conflicting area should not carry a drift marker:\n%s", defOut)
	}

	// Compact view: one line per area, badge survives compaction.
	var comp bytes.Buffer
	writeCanonCompact(&comp, entries, nil, conflicts)
	compOut := comp.String()
	if !strings.Contains(compOut, "⚡ CONFLICT [TASK-499] canon says resolved") {
		t.Errorf("compact view missing conflict line:\n%s", compOut)
	}
	if !strings.Contains(compOut, "⚡1X") {
		t.Errorf("compact header missing conflict count:\n%s", compOut)
	}
}
