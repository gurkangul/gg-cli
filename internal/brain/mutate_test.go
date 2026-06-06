package brain

import (
	"errors"
	"testing"
)

func TestFoldLatest_LastWriteWins(t *testing.T) {
	entries := []Entry{
		{UUID: "a", Payload: map[string]any{"status": "open", "version": int64(1)}},
		{UUID: "b", Payload: map[string]any{"status": "open", "version": int64(1)}},
		{UUID: "a", Payload: map[string]any{"status": "fixed", "version": int64(2)}},
	}
	got := FoldLatest(entries)
	if len(got) != 2 {
		t.Fatalf("FoldLatest len = %d, want 2", len(got))
	}
	// "a" must reflect the latest (fixed/v2), and order follows last occurrence:
	// b appears at idx1, a's latest at idx2, so order is [b, a].
	if got[0].UUID != "b" || got[1].UUID != "a" {
		t.Fatalf("order = [%s,%s], want [b,a]", got[0].UUID, got[1].UUID)
	}
	if got[1].Payload["status"] != "fixed" {
		t.Fatalf("a.status = %v, want fixed (last-write-wins)", got[1].Payload["status"])
	}
}

func TestAppendCAS_VersionGuard(t *testing.T) {
	dir := t.TempDir()
	// Seed a create-time record (version 1), mirroring store create paths.
	if err := Append(dir, "bugs", "u1", "tester", map[string]any{"status": "open", "version": int64(1)}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	cur, err := CurrentVersion(dir, "bugs", "u1")
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if cur != 1 {
		t.Fatalf("CurrentVersion = %d, want 1", cur)
	}

	// Successful CAS at the observed version bumps to 2.
	newVer, err := AppendCAS(dir, "bugs", "u1", "tester", cur, map[string]any{"status": "fixed"})
	if err != nil {
		t.Fatalf("AppendCAS: %v", err)
	}
	if newVer != 2 {
		t.Fatalf("newVer = %d, want 2", newVer)
	}

	// A stale CAS (expected version 1 again) must conflict and write nothing.
	if _, err := AppendCAS(dir, "bugs", "u1", "tester", 1, map[string]any{"status": "wontfix"}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale AppendCAS err = %v, want ErrVersionConflict", err)
	}

	// State after the conflict is still the v2 write, not the rejected v3.
	latest, err := ReadLatest(dir, "bugs")
	if err != nil {
		t.Fatalf("ReadLatest: %v", err)
	}
	if len(latest) != 1 {
		t.Fatalf("ReadLatest len = %d, want 1", len(latest))
	}
	if latest[0].Payload["status"] != "fixed" {
		t.Fatalf("status = %v, want fixed", latest[0].Payload["status"])
	}
	if PayloadVersion(latest[0].Payload) != 2 {
		t.Fatalf("version = %d, want 2", PayloadVersion(latest[0].Payload))
	}
}
