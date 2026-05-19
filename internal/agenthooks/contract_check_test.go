// Package agenthooks — tests for CheckContract, FixContract, HasDrift.
package agenthooks

import (
	"os"
	"path/filepath"
	"testing"
)

// contractFixture sets up a minimal project tree with agent contract files
// already written by the standard installer. Returns the project root.
func contractFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Write a fresh contract block into CLAUDE.md so "claude" is OK.
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte(ContractBlock()), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	return dir
}

func TestCheckContract_OK(t *testing.T) {
	dir := contractFixture(t)

	results := CheckContract(dir)

	var found *ContractCheckResult
	for i := range results {
		if results[i].AgentName == "claude" {
			found = &results[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no result for claude")
		return
	}
	if found.Status != ContractOK {
		t.Errorf("claude: want ContractOK, got %s (found=%s, want=%s)",
			found.Status, found.FoundVersion[:8], found.WantVersion[:8])
	}
}

func TestCheckContract_Missing(t *testing.T) {
	dir := t.TempDir()
	// No CLAUDE.md written — claude entry should be MISSING.

	results := CheckContract(dir)

	var found *ContractCheckResult
	for i := range results {
		if results[i].AgentName == "gsd" {
			t.Fatalf("soft GSD contract should be skipped when .gsd/gsd.db is absent: %+v", results[i])
		}
		if results[i].AgentName == "claude" {
			found = &results[i]
		}
	}
	if found == nil {
		t.Fatalf("no result for claude")
		return
	}
	if found.Status != ContractMISSING {
		t.Errorf("claude: want ContractMISSING, got %s", found.Status)
	}
}

func TestCheckContract_Stale(t *testing.T) {
	dir := t.TempDir()
	// Write a contract block with old content.
	staleBlock := ContractBlockBegin + "\n## GG MANDATORY CONTRACT\n\nold content\n" + ContractBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(staleBlock), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	results := CheckContract(dir)

	var found *ContractCheckResult
	for i := range results {
		if results[i].AgentName == "claude" {
			found = &results[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no result for claude")
		return
	}
	if found.Status != ContractSTALE {
		t.Errorf("claude: want ContractSTALE, got %s", found.Status)
	}
}

func TestCheckContract_Drifted(t *testing.T) {
	dir := t.TempDir()
	// Write file with begin marker but no end marker.
	drifted := "# project\n\n" + ContractBlockBegin + "\ncontent without end marker\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(drifted), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	results := CheckContract(dir)

	var found *ContractCheckResult
	for i := range results {
		if results[i].AgentName == "claude" {
			found = &results[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no result for claude")
		return
	}
	if found.Status != ContractDRIFTED {
		t.Errorf("claude: want ContractDRIFTED, got %s", found.Status)
	}
}

func TestCheckContract_DriftedLegacyVersion(t *testing.T) {
	dir := t.TempDir()
	const v0Begin = "<!-- gg:contract:begin v0 -->"
	legacy := "# project\n\n" + v0Begin + "\nold contract\n" + ContractBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	results := CheckContract(dir)

	var found *ContractCheckResult
	for i := range results {
		if results[i].AgentName == "claude" {
			found = &results[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no result for claude")
		return
	}
	if found.Status != ContractDRIFTED {
		t.Errorf("legacy contract marker must be DRIFTED, got %s", found.Status)
	}
}

func TestFixContract_RepairesMissing(t *testing.T) {
	dir := t.TempDir()
	// CLAUDE.md does not exist → MISSING.

	lines, err := FixContract(dir, false)
	if err != nil {
		t.Fatalf("FixContract: %v", err)
	}

	// After fix, CLAUDE.md should exist with the contract block.
	data, readErr := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if readErr != nil {
		t.Fatalf("CLAUDE.md not created after fix: %v", readErr)
	}
	if !containsMarkers(string(data)) {
		t.Errorf("CLAUDE.md after fix missing contract markers:\n%s", string(data))
	}

	// At least one line should mention MISSING + claude.
	found := false
	for _, l := range lines {
		if contains(l, "MISSING") && contains(l, "claude") {
			found = true
		}
	}
	if !found {
		t.Errorf("fix report did not mention MISSING+claude; got: %v", lines)
	}
}

func TestFixContract_RepaireStale(t *testing.T) {
	dir := t.TempDir()
	stale := ContractBlockBegin + "\n## OLD\n\nstale body\n" + ContractBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(stale), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := FixContract(dir, false); err != nil {
		t.Fatalf("FixContract: %v", err)
	}

	results := CheckContract(dir)
	for _, r := range results {
		if r.AgentName == "claude" && r.Status != ContractOK {
			t.Errorf("after fix: claude status = %s, want OK", r.Status)
		}
	}
}

func TestFixContract_RefusesDriftedWithoutForceReset(t *testing.T) {
	dir := t.TempDir()
	// Only begin marker — DRIFTED.
	drifted := ContractBlockBegin + "\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(drifted), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := FixContract(dir, false)
	if err != nil {
		t.Fatalf("FixContract returned unexpected error: %v", err)
	}

	// Should mention refusal for DRIFTED.
	refused := false
	for _, l := range lines {
		if contains(l, "DRIFTED") && contains(l, "force-reset") {
			refused = true
		}
	}
	if !refused {
		t.Errorf("expected DRIFTED refusal message; lines: %v", lines)
	}
}

func TestFixContract_ForceResetOnDrifted(t *testing.T) {
	dir := t.TempDir()
	// Only begin marker — DRIFTED.
	drifted := ContractBlockBegin + "\nbody without end\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(drifted), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := FixContract(dir, true)
	if err != nil {
		t.Fatalf("FixContract --force-reset on DRIFTED: %v", err)
	}

	fixed := false
	for _, l := range lines {
		if contains(l, "DRIFTED") && contains(l, "fixed") {
			fixed = true
		}
	}
	if !fixed {
		t.Errorf("expected DRIFTED→fixed message; lines: %v", lines)
	}

	// claude entry should now be OK.
	results := CheckContract(dir)
	for _, r := range results {
		if r.AgentName == "claude" && r.Status != ContractOK {
			t.Errorf("after force-reset: claude status = %s, want OK", r.Status)
		}
	}

	data, readErr := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if readErr != nil {
		t.Fatalf("read fixed CLAUDE.md: %v", readErr)
	}
	if contains(string(data), "body without end") {
		t.Errorf("force-reset must strip malformed body, got:\n%s", string(data))
	}
}

func TestCheckContract_Extended(t *testing.T) {
	dir := t.TempDir()
	// Block has all template lines PLUS extra local content.
	extended := ContractBlockBegin + "\n" + ContractBody() + "\n## Local addition\nSome rule.\n" + ContractBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(extended), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	results := CheckContract(dir)
	var found *ContractCheckResult
	for i := range results {
		if results[i].AgentName == "claude" {
			found = &results[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no result for claude")
		return
	}
	if found.Status != ContractEXTENDED {
		t.Errorf("want ContractEXTENDED, got %s", found.Status)
	}
}

func TestFixContract_ExtendedBacksUpAndFixes(t *testing.T) {
	dir := t.TempDir()
	extended := ContractBlockBegin + "\n" + ContractBody() + "\n## Local Team Rule\nSome rule.\n" + ContractBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(extended), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := FixContract(dir, false)
	if err != nil {
		t.Fatalf("FixContract on EXTENDED: %v", err)
	}

	backupFound := false
	for _, l := range lines {
		if contains(l, "backed up") {
			backupFound = true
		}
	}
	if !backupFound {
		t.Errorf("expected backup message in output; lines: %v", lines)
	}

	results := CheckContract(dir)
	for _, r := range results {
		if r.AgentName == "claude" && r.Status != ContractOK {
			t.Errorf("after EXTENDED fix: claude status = %s, want OK", r.Status)
		}
	}
}

func TestHasDrift_TrueWhenExtended(t *testing.T) {
	dir := t.TempDir()
	extended := ContractBlockBegin + "\n" + ContractBody() + "\n## Local addition\n" + ContractBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(extended), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !HasDrift(dir) {
		t.Error("expected HasDrift=true when contract is EXTENDED")
	}
}

func TestHasDrift_TrueWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if !HasDrift(dir) {
		t.Errorf("expected HasDrift=true when no contract files exist")
	}
}

func TestHasDrift_FalseWhenAllOK(t *testing.T) {
	dir := contractFixture(t)
	// Also install the other files so none are MISSING.
	// cursor, codex/bmad (AGENTS.md), gsd.
	_ = os.MkdirAll(filepath.Join(dir, ".cursor", "rules"), 0o755)
	writeBlock(t, filepath.Join(dir, ".cursor", "rules", "gg-mandatory.mdc"))
	writeBlock(t, filepath.Join(dir, "AGENTS.md"))
	_ = os.MkdirAll(filepath.Join(dir, ".gsd"), 0o755)
	writeBlock(t, filepath.Join(dir, ".gsd", "KNOWLEDGE.md"))

	if HasDrift(dir) {
		t.Errorf("expected HasDrift=false when all contracts are current")
	}
}

// helpers

func writeBlock(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(ContractBlock()), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func containsMarkers(s string) bool {
	return contains(s, ContractBlockBegin) && contains(s, ContractBlockEnd)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
