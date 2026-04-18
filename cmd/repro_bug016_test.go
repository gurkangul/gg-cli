package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gurkangul/gg-cli/internal/outbox"
)

// TestSweepIndexOutbox_DeletesStaleEntries guards BUG-016: before the fix,
// sweepIndexOutbox was never called, so prior crashed runs accumulated
// indefinitely. After the fix, a successful index sweeps all prior entries
// for the same root+lang pair.
func TestSweepIndexOutbox_DeletesStaleEntries(t *testing.T) {
	ggDir := t.TempDir()

	root := "/tmp/proj"
	lang := "go"

	// Write two stale entries for the same root+lang pair.
	payload, _ := json.Marshal(indexOutboxPayload{Kind: "full", Root: root, Lang: lang, SHA: "aaa"})
	stale1, err := outbox.Write(ggDir, "full-index", json.RawMessage(payload))
	if err != nil {
		t.Fatalf("write stale1: %v", err)
	}
	payload2, _ := json.Marshal(indexOutboxPayload{Kind: "full", Root: root, Lang: lang, SHA: "bbb"})
	stale2, err := outbox.Write(ggDir, "full-index", json.RawMessage(payload2))
	if err != nil {
		t.Fatalf("write stale2: %v", err)
	}

	// Simulate a successful re-index: no current entry, just sweep the stale ones.
	sweepIndexOutbox(ggDir, root, lang, "")

	// Both stale entries must have been deleted.
	for _, id := range []string{stale1, stale2} {
		p := filepath.Join(ggDir, "outbox", id+".json")
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("stale entry %s still exists after sweep", id)
		}
	}
}

// TestSweepIndexOutbox_SkipsCurrentEntry verifies that the current entry is
// deleted via the explicit Delete call, not silently preserved.
func TestSweepIndexOutbox_SkipsCurrentEntry(t *testing.T) {
	ggDir := t.TempDir()

	root := "/tmp/proj"
	lang := "go"

	payload, _ := json.Marshal(indexOutboxPayload{Kind: "full", Root: root, Lang: lang, SHA: "ccc"})
	currentID, err := outbox.Write(ggDir, "full-index", json.RawMessage(payload))
	if err != nil {
		t.Fatalf("write current: %v", err)
	}

	// Sweep with currentID — the explicit Delete in sweepIndexOutbox removes it.
	sweepIndexOutbox(ggDir, root, lang, currentID)

	p := filepath.Join(ggDir, "outbox", currentID+".json")
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("current entry %s should have been deleted by sweep", currentID)
	}
}
