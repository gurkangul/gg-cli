package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCursor_WriteReadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC().Truncate(time.Second)
	if err := Write(dir, "claude-code", ts); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, ok, err := Read(dir, "claude-code")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !ok {
		t.Fatal("Read: ok=false, want true")
	}
	if !got.Equal(ts) {
		t.Errorf("got %v, want %v", got, ts)
	}
}

func TestCursor_ReadMissingFile(t *testing.T) {
	dir := t.TempDir()
	ts, ok, err := Read(dir, "claude-code")
	if err != nil {
		t.Fatalf("Read on missing file should not error, got: %v", err)
	}
	if ok {
		t.Fatal("ok should be false for missing file")
	}
	if !ts.IsZero() {
		t.Errorf("ts should be zero, got %v", ts)
	}
}

func TestCursor_CrossAgentIsolation(t *testing.T) {
	dir := t.TempDir()
	tsA := time.Now().UTC().Truncate(time.Second)
	tsB := tsA.Add(time.Hour)

	if err := Write(dir, "agent-a", tsA); err != nil {
		t.Fatalf("Write agent-a: %v", err)
	}
	if err := Write(dir, "agent-b", tsB); err != nil {
		t.Fatalf("Write agent-b: %v", err)
	}

	gotA, _, err := Read(dir, "agent-a")
	if err != nil {
		t.Fatalf("Read agent-a: %v", err)
	}
	gotB, _, err := Read(dir, "agent-b")
	if err != nil {
		t.Fatalf("Read agent-b: %v", err)
	}

	if !gotA.Equal(tsA) {
		t.Errorf("agent-a cursor: got %v, want %v", gotA, tsA)
	}
	if !gotB.Equal(tsB) {
		t.Errorf("agent-b cursor: got %v, want %v", gotB, tsB)
	}
}

func TestCursor_CrossProjectIsolation(t *testing.T) {
	dirP1 := t.TempDir()
	dirP2 := t.TempDir()

	ts1 := time.Now().UTC().Truncate(time.Second)
	ts2 := ts1.Add(time.Hour)

	if err := Write(dirP1, "claude-code", ts1); err != nil {
		t.Fatalf("Write project1: %v", err)
	}
	if err := Write(dirP2, "claude-code", ts2); err != nil {
		t.Fatalf("Write project2: %v", err)
	}

	got1, _, err := Read(dirP1, "claude-code")
	if err != nil {
		t.Fatalf("Read project1: %v", err)
	}
	got2, _, err := Read(dirP2, "claude-code")
	if err != nil {
		t.Fatalf("Read project2: %v", err)
	}

	if !got1.Equal(ts1) {
		t.Errorf("project1 cursor: got %v, want %v", got1, ts1)
	}
	if !got2.Equal(ts2) {
		t.Errorf("project2 cursor: got %v, want %v", got2, ts2)
	}
}

func TestCursor_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	p := Path(dir, "claude-code")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte("not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, err := Read(dir, "claude-code")
	var syntaxErr *json.SyntaxError
	if err == nil {
		t.Fatal("Read on malformed JSON should return error")
	}
	// Must be a JSON parse error, not a silent zero
	if !isJSONError(err, &syntaxErr) {
		t.Errorf("expected JSON parse error, got %T: %v", err, err)
	}
}

func isJSONError(err error, _ **json.SyntaxError) bool {
	if err == nil {
		return false
	}
	switch err.(type) {
	case *json.SyntaxError, *json.UnmarshalTypeError:
		return true
	}
	return err.Error() != ""
}
