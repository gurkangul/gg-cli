// Package cmd — fresh-project (un-reembedded) behavior for tell, status, inbox,
// export, check commands. With the embedded SQLite store there is no "store down"
// state; a fresh project simply has un-materialized collections. Read paths that
// have a JSONL/snapshot fallback degrade gracefully (exit 0); the rest surface a
// not-found until `gg reembed` materializes the collections.
package cmd

import (
	"testing"
)

func TestTell_FreshProject_Succeeds(t *testing.T) {
	setupGGDir(t)
	// tell is JSONL-first: it persists the message even on a fresh project where
	// the messages collection has not been materialized yet — no hard failure.
	if _, _, err := execCmd(t, "tell", "developer", "ship it"); err != nil {
		t.Fatalf("tell should persist on a fresh project (JSONL-first), got %v", err)
	}
}

func TestStatus_FreshProject_Succeeds(t *testing.T) {
	setupGGDir(t)
	// status degrades to the JSONL/snapshot view on a fresh project — it must not
	// hard-fail just because the vector collections are not materialized.
	if _, _, err := execCmd(t, "status"); err != nil {
		t.Fatalf("status should degrade gracefully on a fresh project, got %v", err)
	}
}

func TestInbox_FreshProject_NotMaterialized(t *testing.T) {
	setupGGDir(t)
	// inbox reads the messages collection directly; on a fresh project that
	// collection is not materialized, so the read surfaces a not-found error
	// rather than the (removed) Qdrant-down sentinel.
	if _, _, err := execCmd(t, "inbox"); err == nil {
		t.Fatal("expected a not-found error reading an un-materialized inbox collection")
	}
}

func TestExport_FreshProject_Succeeds(t *testing.T) {
	setupGGDir(t)
	// export writes a snapshot from the JSONL source of truth; on a fresh project
	// it produces an empty snapshot rather than failing.
	if _, _, err := execCmd(t, "export"); err != nil {
		t.Fatalf("export should succeed (empty snapshot) on a fresh project, got %v", err)
	}
}

func TestCheck_FreshProject_Succeeds(t *testing.T) {
	setupGGDir(t)
	// check reports project health; on a fresh project it completes cleanly rather
	// than reporting the (removed) store-down condition.
	if _, _, err := execCmd(t, "check"); err != nil {
		t.Fatalf("check should complete on a fresh project, got %v", err)
	}
}
