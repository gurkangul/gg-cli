package locks_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gurkangul/gg-cli/internal/orchestrator/locks"
)

func newStore(t *testing.T) *locks.Store {
	t.Helper()
	dir := t.TempDir()
	return locks.New(dir)
}

func TestAcquireAndRelease(t *testing.T) {
	s := newStore(t)

	if err := s.Acquire("TASK-001", []string{"a.go", "b.go"}, false); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	all, err := s.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].TaskID != "TASK-001" {
		t.Fatalf("expected 1 claim for TASK-001, got %+v", all)
	}

	if err := s.Release("TASK-001"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	all, _ = s.ReadAll()
	if len(all) != 0 {
		t.Fatalf("expected 0 claims after release, got %d", len(all))
	}
}

func TestCollisionDetection(t *testing.T) {
	s := newStore(t)

	_ = s.Acquire("TASK-001", []string{"shared.go"}, false)

	// Different task claiming the same path should collide.
	err := s.Acquire("TASK-002", []string{"shared.go", "other.go"}, false)
	if err == nil {
		t.Fatal("expected collision error, got nil")
	}
	cl, ok := err.(locks.CollisionList)
	if !ok {
		t.Fatalf("expected CollisionList, got %T: %v", err, err)
	}
	if len(cl) != 1 || cl[0].HeldBy != "TASK-001" || cl[0].Path != "shared.go" {
		t.Fatalf("unexpected collision: %+v", cl)
	}
}

func TestForceOverride(t *testing.T) {
	s := newStore(t)

	_ = s.Acquire("TASK-001", []string{"shared.go"}, false)

	// Force should succeed even with a collision.
	if err := s.Acquire("TASK-002", []string{"shared.go"}, true); err != nil {
		t.Fatalf("force acquire: %v", err)
	}

	all, _ := s.ReadAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 claims, got %d", len(all))
	}
}

func TestSameTaskReclaim(t *testing.T) {
	s := newStore(t)

	// TASK-001 can re-claim its own paths without conflict.
	_ = s.Acquire("TASK-001", []string{"a.go"}, false)
	if err := s.Acquire("TASK-001", []string{"a.go", "b.go"}, false); err != nil {
		t.Fatalf("re-claim by same task: %v", err)
	}
	paths, _ := s.PathsFor("TASK-001")
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths after re-claim, got %d: %v", len(paths), paths)
	}
}

func TestNoPathsRelease(t *testing.T) {
	s := newStore(t)
	_ = s.Acquire("TASK-001", []string{"a.go"}, false)

	// Acquire with empty paths should release the claim.
	if err := s.Acquire("TASK-001", nil, false); err != nil {
		t.Fatalf("empty acquire: %v", err)
	}
	all, _ := s.ReadAll()
	if len(all) != 0 {
		t.Fatalf("expected 0 claims after empty acquire, got %d", len(all))
	}
}

func TestReadAllAbsent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	s := locks.New(dir)
	// dir doesn't exist yet — ReadAll should return nil, nil.
	claims, err := s.ReadAll()
	if err == nil {
		// Fine: dir not existing is treated as empty.
		t.Log("expected nil error for absent locks file")
	}
	if err != nil {
		// Also acceptable: some OS errors on ReadFile.
		if !os.IsNotExist(err) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if len(claims) != 0 {
		t.Fatalf("expected 0 claims for absent file, got %d", len(claims))
	}
}

func TestCheckConflictsNoFile(t *testing.T) {
	s := newStore(t)
	cl, err := s.CheckConflicts([]string{"x.go"}, "TASK-001")
	if err != nil {
		t.Fatalf("CheckConflicts on empty store: %v", err)
	}
	if len(cl) != 0 {
		t.Fatalf("expected no collisions, got %d", len(cl))
	}
}

func TestPathsFor(t *testing.T) {
	s := newStore(t)
	_ = s.Acquire("TASK-001", []string{"a.go", "b.go"}, false)

	paths, err := s.PathsFor("TASK-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(paths))
	}

	// Unknown task returns nil.
	paths, err = s.PathsFor("TASK-999")
	if err != nil {
		t.Fatal(err)
	}
	if paths != nil {
		t.Fatalf("expected nil for unknown task, got %v", paths)
	}
}

func TestCollisionListError(t *testing.T) {
	single := locks.CollisionList{{Path: "x.go", HeldBy: "TASK-001"}}
	if single.Error() == "" {
		t.Fatal("expected non-empty error string")
	}

	multi := locks.CollisionList{
		{Path: "x.go", HeldBy: "TASK-001"},
		{Path: "y.go", HeldBy: "TASK-002"},
	}
	msg := multi.Error()
	if msg == "" {
		t.Fatal("expected non-empty multi-collision error")
	}
}
