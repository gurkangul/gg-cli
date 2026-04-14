// Package cmd — store-down tests for note commands.
package cmd

import (
	"testing"
)

func TestNote_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "note", "high latency observed")
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

func TestNoteList_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "note", "list")
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

func TestNoteSearch_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "note", "search", "high latency")
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
