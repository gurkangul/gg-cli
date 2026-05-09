package agenthooks

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSyncManagedBlocks_RepairsStaleContract verifies that a STALE contract
// block in CLAUDE.md is repaired and Repaired is set true.
func TestSyncManagedBlocks_RepairsStaleContract(t *testing.T) {
	dir := t.TempDir()

	stale := ContractBlockBegin + "\n## OLD CONTRACT\n\noutdated\n" + ContractBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(stale), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	sr := SyncManagedBlocks(dir)

	if !sr.Repaired {
		t.Error("expected Repaired=true after stale contract repair")
	}
	if len(sr.Errors) > 0 {
		t.Errorf("unexpected errors: %v", sr.Errors)
	}

	// CLAUDE.md should now have the current contract block.
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md after sync: %v", err)
	}
	if !contains(string(data), ContractBlockBegin) || !contains(string(data), ContractBlockEnd) {
		t.Error("CLAUDE.md missing contract markers after sync")
	}
}

// TestSyncManagedBlocks_NoCLAUDEmd verifies that SyncManagedBlocks does not
// report errors when CLAUDE.md is absent.
func TestSyncManagedBlocks_NoCLAUDEmd(t *testing.T) {
	dir := t.TempDir()
	// No CLAUDE.md present.

	sr := SyncManagedBlocks(dir)

	if len(sr.Errors) > 0 {
		t.Errorf("unexpected errors when CLAUDE.md absent: %v", sr.Errors)
	}
}

// TestSyncManagedBlocks_InstallsNewAgentRegistry verifies that when a new
// agent registry file appears (e.g., AGENTS.md for codex), SyncManagedBlocks
// installs the managed block for that agent.
func TestSyncManagedBlocks_InstallsNewAgentRegistry(t *testing.T) {
	dir := t.TempDir()

	// Simulate: codex AGENTS.md present (detect returns true) but without a
	// managed block yet.
	agentsMD := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsMD, []byte("# Agent Guide\n\nSome content.\n"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	sr := SyncManagedBlocks(dir)

	if !sr.Repaired {
		t.Errorf("expected Repaired=true when AGENTS.md present without managed block; lines: %v", sr.Lines)
	}

	// AGENTS.md should now have the codex managed block.
	data, err := os.ReadFile(agentsMD)
	if err != nil {
		t.Fatalf("read AGENTS.md after sync: %v", err)
	}
	if !contains(string(data), codexBlockStart) {
		t.Error("AGENTS.md missing codex managed block after sync")
	}
}

// TestSyncManagedBlocks_BMADDir_InstallsBlock verifies AC-6a: when a _bmad/
// directory is present in the project root alongside AGENTS.md,
// SyncManagedBlocks detects BMAD and installs the gg-bmad relay block in
// AGENTS.md. This exercises the _bmad/ code path (distinct from the codex
// AGENTS.md-only path).
func TestSyncManagedBlocks_BMADDir_InstallsBlock(t *testing.T) {
	dir := t.TempDir()

	// Seed _bmad/ — canonical BMAD detection signal.
	if err := os.MkdirAll(filepath.Join(dir, "_bmad"), 0o755); err != nil {
		t.Fatalf("mkdir _bmad: %v", err)
	}

	// AGENTS.md must exist for the bmad installer to write the relay block.
	agentsMD := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsMD, []byte("# Agent Guidance\n\nSome content.\n"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	sr := SyncManagedBlocks(dir)

	if !sr.Repaired {
		t.Errorf("expected Repaired=true when _bmad/ present; lines: %v", sr.Lines)
	}

	// AC-6a: AGENTS.md must contain the gg-bmad relay block.
	data, err := os.ReadFile(agentsMD)
	if err != nil {
		t.Fatalf("read AGENTS.md after sync: %v", err)
	}
	if !contains(string(data), bmadBlockStart) {
		t.Error("AGENTS.md missing gg-bmad block after SyncManagedBlocks with _bmad/ present (AC-6a)")
	}

	// AC-2: SyncResult.Lines must carry the "BMAD detected" notice.
	found := false
	for _, line := range sr.Lines {
		if contains(line, "BMAD detected") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SyncResult.Lines missing 'BMAD detected' notice (AC-2); got: %v", sr.Lines)
	}
}
