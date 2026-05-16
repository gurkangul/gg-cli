package projectstate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/projectstate"
)

func TestReadMissingFile(t *testing.T) {
	dir := t.TempDir()
	s, err := projectstate.Read(dir)
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if s.LastSeenCLIVersion != "" {
		t.Errorf("expected empty version, got %q", s.LastSeenCLIVersion)
	}
}

func TestWriteRead(t *testing.T) {
	dir := t.TempDir()
	in := projectstate.State{LastSeenCLIVersion: "v0.2.0"}
	if err := projectstate.Write(dir, in); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := projectstate.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.LastSeenCLIVersion != "v0.2.0" {
		t.Errorf("got %q, want v0.2.0", out.LastSeenCLIVersion)
	}
	if out.UpdatedAt == "" {
		t.Error("UpdatedAt should be set after Write")
	}
}

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	_ = projectstate.Write(dir, projectstate.State{LastSeenCLIVersion: "v0.1.0"})
	_ = projectstate.Write(dir, projectstate.State{LastSeenCLIVersion: "v0.2.0"})
	s, _ := projectstate.Read(dir)
	if s.LastSeenCLIVersion != "v0.2.0" {
		t.Errorf("expected v0.2.0 after two writes, got %q", s.LastSeenCLIVersion)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json.tmp")); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after successful write")
	}
}

func TestRecordHydrationAndHasRecentHydration(t *testing.T) {
	dir := t.TempDir()
	if err := projectstate.RecordHydration(dir, "task", "TASK-123"); err != nil {
		t.Fatalf("RecordHydration: %v", err)
	}

	ok, entry, err := projectstate.HasRecentHydration(dir, "task", "TASK-123", 30*time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("HasRecentHydration: %v", err)
	}
	if !ok {
		t.Fatal("expected recent hydration proof")
	}
	if entry.EntityType != "task" || entry.EntityID != "TASK-123" || entry.TS == "" {
		t.Fatalf("unexpected hydration entry: %+v", entry)
	}
}

func TestHasRecentHydrationRejectsStaleAndWrongEntity(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if err := projectstate.Write(dir, projectstate.State{RecentHydrations: []projectstate.HydrationEntry{
		{TS: old, EntityType: "task", EntityID: "TASK-123"},
		{TS: time.Now().UTC().Format(time.RFC3339), EntityType: "decision", EntityID: "TASK-123"},
	}}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	ok, _, err := projectstate.HasRecentHydration(dir, "task", "TASK-123", 30*time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("HasRecentHydration: %v", err)
	}
	if ok {
		t.Fatal("stale or wrong-entity hydration must not satisfy the gate")
	}
}

func TestConcurrentRecordHydrationPreservesOtherState(t *testing.T) {
	dir := t.TempDir()
	if err := projectstate.Write(dir, projectstate.State{
		LastSeenCLIVersion: "v0.2.0",
		BypassLog: []projectstate.BypassEntry{{
			TS:     time.Now().UTC().Format(time.RFC3339),
			Gate:   "pre-task-done",
			TaskID: "TASK-000",
			Actor:  "test",
		}},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- projectstate.RecordHydration(dir, "task", fmt.Sprintf("TASK-%03d", i+1))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("RecordHydration: %v", err)
		}
	}

	s, err := projectstate.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.LastSeenCLIVersion != "v0.2.0" {
		t.Fatalf("LastSeenCLIVersion was clobbered: %q", s.LastSeenCLIVersion)
	}
	if len(s.BypassLog) != 1 || s.BypassLog[0].TaskID != "TASK-000" {
		t.Fatalf("BypassLog was clobbered: %+v", s.BypassLog)
	}
	seen := map[string]bool{}
	for _, h := range s.RecentHydrations {
		seen[h.EntityID] = true
	}
	for i := 0; i < workers; i++ {
		id := fmt.Sprintf("TASK-%03d", i+1)
		if !seen[id] {
			t.Fatalf("missing hydration proof for %s; entries=%+v", id, s.RecentHydrations)
		}
	}
}
