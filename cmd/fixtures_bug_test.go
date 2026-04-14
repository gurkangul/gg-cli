// Package cmd — store-down tests for bug commands + bug helper functions.
package cmd

import (
	"testing"
)

func TestBugReport_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "bug", "report", "nil pointer in search")
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

func TestBugList_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "bug", "list")
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

func TestBugGet_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "bug", "get", "BUG-001")
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

func TestBugFix_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "bug", "fix", "BUG-001", "nil deref in search handler", "--root-cause", "missing nil check")
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

func TestBugStart_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "bug", "start", "BUG-001")
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

func TestBugWontFix_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "bug", "wontfix", "BUG-001", "not reproducible")
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

func TestBugTriage_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "bug", "triage", "BUG-001")
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

func TestBugStatusIcon(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"fixed", "✓"},
		{"wontfix", "–"},
		{"fixing", "→"},
		{"open", "!"},
		{"unknown", "!"},
		{"", "!"},
	}
	for _, tc := range cases {
		got := bugStatusIcon(tc.status)
		if got != tc.want {
			t.Errorf("bugStatusIcon(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestBugReport_InvalidSeverity(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "bug", "report", "nil pointer in search", "--severity", "mega-critical")
	// severity validation happens before loadDeps
	if err == nil {
		t.Fatal("expected error for invalid severity")
	}
}
