package embedding_test

import (
	"errors"
	"testing"

	"github.com/gurkangul/gg-cli/internal/embedding"
)

func TestCheckMeta_FirstRun(t *testing.T) {
	dir := t.TempDir()
	// No meta file — should write defaults and return nil.
	if err := embedding.CheckMeta(dir, "nomic-embed-text", 768); err != nil {
		t.Fatalf("CheckMeta first run: %v", err)
	}
	// Meta should now exist.
	meta, err := embedding.ReadMeta(dir)
	if err != nil {
		t.Fatalf("ReadMeta after first run: %v", err)
	}
	if meta == nil {
		t.Fatal("expected meta to be written on first run")
	}
	if meta.ModelName != "nomic-embed-text" {
		t.Errorf("ModelName: got %q, want %q", meta.ModelName, "nomic-embed-text")
	}
	if meta.Dim != 768 {
		t.Errorf("Dim: got %d, want 768", meta.Dim)
	}
}

func TestCheckMeta_MatchingModel(t *testing.T) {
	dir := t.TempDir()
	// Write meta, then check with same model — should be fine.
	if err := embedding.WriteMeta(dir, &embedding.Meta{ModelName: "nomic-embed-text", Dim: 768}); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}
	if err := embedding.CheckMeta(dir, "nomic-embed-text", 768); err != nil {
		t.Fatalf("CheckMeta same model: %v", err)
	}
}

func TestCheckMeta_ModelMismatch(t *testing.T) {
	dir := t.TempDir()
	// Write meta for nomic-embed-text, then check with a different model.
	if err := embedding.WriteMeta(dir, &embedding.Meta{ModelName: "nomic-embed-text", Dim: 768}); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}
	err := embedding.CheckMeta(dir, "text-embedding-3-small", 1536)
	if err == nil {
		t.Fatal("expected ErrModelMismatch, got nil")
	}
	if !errors.Is(err, embedding.ErrModelMismatch) {
		t.Errorf("expected ErrModelMismatch, got: %v", err)
	}
}

func TestWriteReadMeta(t *testing.T) {
	dir := t.TempDir()
	want := &embedding.Meta{ModelName: "mxbai-embed-large", Dim: 1024}
	if err := embedding.WriteMeta(dir, want); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}
	got, err := embedding.ReadMeta(dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if got.ModelName != want.ModelName || got.Dim != want.Dim {
		t.Errorf("round-trip: got %+v, want %+v", got, want)
	}
	if got.CreatedAt == "" {
		t.Error("CreatedAt should be auto-set")
	}
}

func TestReadMeta_Missing(t *testing.T) {
	dir := t.TempDir()
	meta, err := embedding.ReadMeta(dir)
	if err != nil {
		t.Fatalf("ReadMeta missing: %v", err)
	}
	if meta != nil {
		t.Errorf("expected nil for missing file, got %+v", meta)
	}
}
