package compat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// TestReadWriteManifest verifies round-trip: write → read → contents match.
func TestReadWriteManifest(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Indexers: map[string]string{
			"scip-go":         "0.1.26",
			"scip-typescript": "0.4.0",
		},
		SCIPLib: "v0.7.0",
	}
	if err := WriteManifest(dir, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	got, err := ReadManifest(dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil manifest")
	}
	for bin, want := range m.Indexers {
		if got.Indexers[bin] != want {
			t.Errorf("indexer %s: want %q, got %q", bin, want, got.Indexers[bin])
		}
	}
	if got.SCIPLib != m.SCIPLib {
		t.Errorf("SCIPLib: want %q, got %q", m.SCIPLib, got.SCIPLib)
	}
	if got.UpdatedAt == "" {
		t.Error("UpdatedAt must be set by WriteManifest")
	}
}

// TestReadManifest_Missing verifies that a missing manifest returns (nil, nil).
func TestReadManifest_Missing(t *testing.T) {
	dir := t.TempDir()
	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatalf("expected nil error for missing manifest, got: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil manifest for missing file, got %+v", m)
	}
}

// TestReadManifest_Corrupt verifies that a corrupt manifest returns an error.
func TestReadManifest_Corrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/indexer-manifest.json", []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadManifest(dir)
	if err == nil {
		t.Fatal("expected error for corrupt manifest, got nil")
	}
}

// TestCheck_NoManifest verifies that Check(nil, ...) passes (first run).
func TestCheck_NoManifest(t *testing.T) {
	if err := Check(context.Background(), nil, []string{"scip-go"}); err != nil {
		t.Errorf("Check with nil manifest should pass, got: %v", err)
	}
}

// fakeBin writes a shell script to dir/name that prints `version` on stdout.
// Returns the directory containing the binary so callers can prepend it to PATH.
func fakeBin(t *testing.T, dir, name, version string) {
	t.Helper()
	path := dir + "/" + name
	script := "#!/bin/sh\necho '" + version + "'\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake binary %s: %v", name, err)
	}
}

// TestCheck_Match verifies that matching versions pass.
func TestCheck_Match(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "fake-scip", "1.2.3")
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	m := &Manifest{Indexers: map[string]string{"fake-scip": "1.2.3"}}
	if err := Check(context.Background(), m, []string{"fake-scip"}); err != nil {
		t.Errorf("matching versions should pass Check, got: %v", err)
	}
}

// TestCheck_Mismatch verifies that a version mismatch returns ErrVersionMismatch.
func TestCheck_Mismatch(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "fake-scip", "1.2.4") // binary reports 1.2.4
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	m := &Manifest{Indexers: map[string]string{"fake-scip": "1.2.3"}} // manifest says 1.2.3
	err := Check(context.Background(), m, []string{"fake-scip"})
	if err == nil {
		t.Fatal("expected ErrVersionMismatch for mismatched version, got nil")
	}
	var mismatch *ErrVersionMismatch
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected *ErrVersionMismatch, got %T: %v", err, err)
	}
	if mismatch.Binary != "fake-scip" {
		t.Errorf("Binary: want 'fake-scip', got %q", mismatch.Binary)
	}
	if mismatch.Recorded != "1.2.3" {
		t.Errorf("Recorded: want '1.2.3', got %q", mismatch.Recorded)
	}
	if mismatch.Installed != "1.2.4" {
		t.Errorf("Installed: want '1.2.4', got %q", mismatch.Installed)
	}
}

// TestCheck_BinaryNotInstalled verifies that a missing binary is skipped
// (not a hard error during Check — the runner handles that separately).
func TestCheck_BinaryNotInstalled(t *testing.T) {
	m := &Manifest{
		Indexers: map[string]string{"totally-nonexistent-binary-gg": "1.0.0"},
	}
	if err := Check(context.Background(), m, []string{"totally-nonexistent-binary-gg"}); err != nil {
		t.Errorf("missing binary should be skipped by Check, got: %v", err)
	}
}

// TestErrVersionMismatch_Message verifies the error message is actionable.
func TestErrVersionMismatch_Message(t *testing.T) {
	e := &ErrVersionMismatch{Binary: "scip-go", Recorded: "0.1.25", Installed: "0.1.26"}
	msg := e.Error()
	for _, want := range []string{"scip-go", "0.1.25", "0.1.26", "gg doctor --install-indexers"} {
		if len(msg) == 0 {
			t.Fatal("empty error message")
		}
		found := false
		for i := 0; i <= len(msg)-len(want); i++ {
			if msg[i:i+len(want)] == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("error message should contain %q: %s", want, msg)
		}
	}
}

// TestSCIPLibVersion verifies that SCIPLibVersion returns a non-empty string.
func TestSCIPLibVersion(t *testing.T) {
	ver := SCIPLibVersion()
	if ver == "" {
		t.Error("SCIPLibVersion must not be empty")
	}
	// In test mode, the build info may say "unknown" — that's acceptable.
	t.Logf("scip lib version: %s", ver)
}

// TestWriteManifest_IsValidJSON verifies that the written file is parseable
// as JSON by a third party (not just our own reader).
func TestWriteManifest_IsValidJSON(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Indexers: map[string]string{"scip-go": "0.1.26"},
		SCIPLib:  "v0.7.0",
	}
	if err := WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dir + "/indexer-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("manifest is not valid JSON: %v\n%s", err, data)
	}
}
