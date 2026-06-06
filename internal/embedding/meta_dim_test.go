package embedding

import (
	"errors"
	"os"
	"testing"
)

func writeRawMeta(ggDir, json string) error {
	return os.WriteFile(metaPath(ggDir), []byte(json), 0o644)
}

// BUG-078: a non-positive dim must never be persisted (it disables the guard).
func TestWriteMeta_RejectsNonPositiveDim(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMeta(dir, &Meta{ModelName: "nomic-embed-text", Dim: 0}); err == nil {
		t.Fatal("WriteMeta(Dim:0) must be rejected")
	}
	if err := WriteMeta(dir, &Meta{ModelName: "nomic-embed-text", Dim: 768}); err != nil {
		t.Fatalf("WriteMeta(Dim:768): %v", err)
	}
}

// BUG-078: CheckMeta must reject a non-positive configured dim rather than
// validating against a meaningless dimension.
func TestCheckMeta_RejectsNonPositiveDim(t *testing.T) {
	if err := CheckMeta(t.TempDir(), "nomic-embed-text", 0); err == nil {
		t.Fatal("CheckMeta(dim:0) must error")
	}
}

// BUG-078: a corrupt stored Dim:0 (model matches) self-heals to the configured
// dim so the guard is re-enabled, instead of silently skipping the check.
func TestCheckMeta_SelfHealsZeroDim(t *testing.T) {
	dir := t.TempDir()
	// Seed a corrupt meta with Dim:0 directly (bypassing WriteMeta's guard).
	if err := writeRawMeta(dir, `{"model_name":"nomic-embed-text","dim":0}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := CheckMeta(dir, "nomic-embed-text", 768); err != nil {
		t.Fatalf("CheckMeta should self-heal Dim:0, got: %v", err)
	}
	m, err := ReadMeta(dir)
	if err != nil || m == nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if m.Dim != 768 {
		t.Fatalf("Dim after self-heal = %d, want 768", m.Dim)
	}
	// And a genuine model mismatch is still reported.
	if err := CheckMeta(dir, "other-model", 768); !errors.Is(err, ErrModelMismatch) {
		t.Fatalf("model mismatch not reported: %v", err)
	}
}
