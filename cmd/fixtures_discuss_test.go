// Package cmd — store-down and validation tests for discuss commands.
package cmd

import (
	"testing"
)

func TestDiscussOpen_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "discuss", "open", "should we migrate to Postgres?")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

func TestDiscussList_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "discuss", "list")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

func TestDiscussGet_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "discuss", "get", "DISC-001")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

func TestDiscussResolve_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "discuss", "resolve", "DISC-001", "--via", "decision", "--summary", "decided JWT")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

func TestDiscussDismiss_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "discuss", "dismiss", "DISC-001", "--reason", "superseded by DISC-005")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

func TestDiscussNote_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "discuss", "note", "DISC-001", "consider Postgres as alternative", "--by", "architect")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

func TestDiscussShow_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "discuss", "show", "DISC-001")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}

// ── validation-error paths (before loadDeps) ──────────────────────────────────

func TestDiscussResolve_InvalidVia(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "discuss", "resolve", "DISC-001", "--via", "invalid", "--summary", "decided JWT")
	if err == nil {
		t.Fatal("expected error for invalid --via")
	}
	// Should be a plain error, not ExitStoreDown.
	if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
		t.Error("unexpected ExitStoreDown — should fail at --via validation before loadDeps")
	}
}

func TestDiscussNote_InvalidRole(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "discuss", "note", "DISC-001", "some text", "--by", "architect", "--role", "superuser")
	if err == nil {
		t.Fatal("expected error for invalid --role")
	}
	if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
		t.Error("unexpected ExitStoreDown — should fail at --role validation")
	}
}

func TestDiscussNote_EmptyText(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "discuss", "note", "DISC-001", "  ", "--by", "architect")
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}
