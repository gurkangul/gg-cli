package store

import (
	"path/filepath"
	"testing"
)

func TestCanonReadWriteFold(t *testing.T) {
	dir := t.TempDir()
	if err := WriteCanon(dir, "architecture", "v1 text", "a"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := WriteCanon(dir, "gotchas", "use --full", "a"); err != nil {
		t.Fatalf("write2: %v", err)
	}
	// Overwrite an area — ReadCanon must fold to the latest.
	if err := WriteCanon(dir, "architecture", "v2 text", "b"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, err := ReadCanon(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 areas, got %d: %+v", len(got), got)
	}
	byArea := map[string]string{}
	for _, e := range got {
		byArea[e.Area] = e.Text
	}
	if byArea["architecture"] != "v2 text" {
		t.Errorf("architecture should fold to v2, got %q", byArea["architecture"])
	}
	// Canon must live OUTSIDE brain/ so gg brain export cannot clobber it.
	if canonPath(dir) != filepath.Join(dir, "canon.jsonl") {
		t.Errorf("canon must be at <gg>/canon.jsonl, got %s", canonPath(dir))
	}
	// Empty text clears an area.
	if err := WriteCanon(dir, "gotchas", "", "a"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = ReadCanon(dir)
	if len(got) != 1 {
		t.Fatalf("clearing gotchas should leave 1 area, got %d", len(got))
	}
}
