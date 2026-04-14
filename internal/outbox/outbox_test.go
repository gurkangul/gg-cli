package outbox_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/gurkangul/gg-cli/internal/outbox"
)

func TestWriteListDelete(t *testing.T) {
	dir := t.TempDir()

	payload := map[string]string{"sha": "abc123", "lang": "go"}

	// Write two entries.
	id1, err := outbox.Write(dir, "full-index", payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	id2, err := outbox.Write(dir, "changed-index", payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if id1 == id2 {
		t.Fatal("expected distinct IDs")
	}

	// List should return both.
	entries, err := outbox.List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Verify payload round-trips.
	found := false
	for _, e := range entries {
		if e.ID == id1 {
			found = true
			if e.Kind != "full-index" {
				t.Errorf("kind: got %q, want %q", e.Kind, "full-index")
			}
			var got map[string]string
			if err := json.Unmarshal(e.Payload, &got); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if got["sha"] != "abc123" {
				t.Errorf("payload sha: got %q, want %q", got["sha"], "abc123")
			}
		}
	}
	if !found {
		t.Error("id1 not found in List output")
	}

	// Delete id1 — list should shrink to 1.
	if err := outbox.Delete(dir, id1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	entries, err = outbox.List(dir)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after delete, got %d", len(entries))
	}
	if entries[0].ID != id2 {
		t.Errorf("expected remaining id2=%s, got %s", id2, entries[0].ID)
	}

	// Delete is idempotent.
	if err := outbox.Delete(dir, id1); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
}

func TestListEmptyDir(t *testing.T) {
	dir := t.TempDir()
	entries, err := outbox.List(dir)
	if err != nil {
		t.Fatalf("List empty dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestListMissingDir(t *testing.T) {
	dir := t.TempDir() + "/nonexistent"
	entries, err := outbox.List(dir)
	if err != nil {
		t.Fatalf("List missing dir: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil for missing dir, got %v", entries)
	}
}

func TestIncrementRetries(t *testing.T) {
	dir := t.TempDir()
	id, err := outbox.Write(dir, "full-index", "payload")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := outbox.IncrementRetries(dir, id); err != nil {
		t.Fatalf("IncrementRetries: %v", err)
	}
	entries, _ := outbox.List(dir)
	if len(entries) != 1 || entries[0].Retries != 1 {
		t.Errorf("expected retries=1, got %+v", entries)
	}
}

func TestWriteCreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := base + "/nested/ggdir"
	// dir doesn't exist yet — Write should create it.
	_, err := outbox.Write(dir, "full-index", "payload")
	if err != nil {
		t.Fatalf("Write with missing dir: %v", err)
	}
	if _, statErr := os.Stat(dir + "/outbox"); statErr != nil {
		t.Errorf("outbox subdir not created: %v", statErr)
	}
}
