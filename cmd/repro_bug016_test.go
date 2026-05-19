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

// TestSweepIndexOutbox_DeletesMovedProjectEntries guards project-root moves:
// stale outbox payloads may still contain the old absolute path, but the outbox
// directory is project-local, so a successful same-language index supersedes
// them. Other languages must remain pending.
func TestSweepIndexOutbox_DeletesMovedProjectEntries(t *testing.T) {
	ggDir := t.TempDir()

	currentRoot := "/workspace/projects/oneliftui"
	oldRoot := "/Users/gurkangul/my-projects/oneliftui"
	lang := "typescript"

	stalePayload, _ := json.Marshal(indexOutboxPayload{Kind: "full", Root: oldRoot, Lang: lang, SHA: "old"})
	staleID, err := outbox.Write(ggDir, "full-index", json.RawMessage(stalePayload))
	if err != nil {
		t.Fatalf("write moved-root stale entry: %v", err)
	}

	otherLangPayload, _ := json.Marshal(indexOutboxPayload{Kind: "full", Root: oldRoot, Lang: "go", SHA: "old"})
	otherLangID, err := outbox.Write(ggDir, "full-index", json.RawMessage(otherLangPayload))
	if err != nil {
		t.Fatalf("write other-language stale entry: %v", err)
	}

	sweepIndexOutbox(ggDir, currentRoot, lang, "")

	if _, err := os.Stat(filepath.Join(ggDir, "outbox", staleID+".json")); !os.IsNotExist(err) {
		t.Fatalf("same-language stale moved-root entry should be deleted, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(ggDir, "outbox", otherLangID+".json")); err != nil {
		t.Fatalf("other-language entry should remain pending, stat err=%v", err)
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
