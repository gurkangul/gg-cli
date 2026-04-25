package agenthooks

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSyncManagedBlocks_NoOp verifies that SyncManagedBlocks is silent (no
// repairs, no errors) when all managed blocks are already current.
func TestSyncManagedBlocks_NoOp(t *testing.T) {
	dir := t.TempDir()

	// Write a current contract block into CLAUDE.md.
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte(ContractBlock()+MasterRoleBlock()+DevRoutingBlock()), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	sr := SyncManagedBlocks(dir)

	if sr.Repaired {
		t.Errorf("expected no repair needed, got Repaired=true; lines: %v", sr.Lines)
	}
	if len(sr.Errors) > 0 {
		t.Errorf("unexpected errors: %v", sr.Errors)
	}
}

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

// TestSyncManagedBlocks_RepairsStaleDevRouting verifies that a STALE
// dev-routing block in an existing CLAUDE.md triggers a repair.
func TestSyncManagedBlocks_RepairsStaleDevRouting(t *testing.T) {
	dir := t.TempDir()

	// Write current contract block + stale dev-routing block.
	staleDevRouting := DevRoutingBlockBegin + "\n## old dev routing\n" + DevRoutingBlockEnd + "\n"
	content := ContractBlock() + staleDevRouting
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	sr := SyncManagedBlocks(dir)

	if !sr.Repaired {
		t.Errorf("expected Repaired=true after stale dev-routing repair; lines: %v", sr.Lines)
	}
}

// TestSyncManagedBlocks_RepairsstaleMasterRole verifies that a STALE
// master-role block in an existing CLAUDE.md triggers a repair.
func TestSyncManagedBlocks_RepairsStaleMasterRole(t *testing.T) {
	dir := t.TempDir()

	staleMasterRole := MasterRoleBlockBegin + "\n## old master role\n" + MasterRoleBlockEnd + "\n"
	content := ContractBlock() + staleMasterRole
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	sr := SyncManagedBlocks(dir)

	if !sr.Repaired {
		t.Errorf("expected Repaired=true after stale master-role repair; lines: %v", sr.Lines)
	}
}

// TestSyncManagedBlocks_NoCLAUDEmd verifies that SyncManagedBlocks does not
// create CLAUDE.md from scratch when it is absent — it only repairs existing
// files for master-role and dev-routing blocks.
func TestSyncManagedBlocks_NoCLAUDEmd(t *testing.T) {
	dir := t.TempDir()
	// No CLAUDE.md present.

	sr := SyncManagedBlocks(dir)

	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
		t.Error("SyncManagedBlocks must not create CLAUDE.md from scratch (master-role/dev-routing guard)")
	}

	// contract block path is created by FixContract (it creates CLAUDE.md for
	// the claude installer). That is acceptable — the installer owns it.
	// What we check here is that no error is returned for missing files.
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

// TestSyncManagedBlocks_Idempotent verifies that running SyncManagedBlocks
// twice produces no repair on the second run.
func TestSyncManagedBlocks_Idempotent(t *testing.T) {
	dir := t.TempDir()

	// Write stale contract block.
	stale := ContractBlockBegin + "\n## OLD\n\nold\n" + ContractBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(stale), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	// First run — repairs.
	sr1 := SyncManagedBlocks(dir)
	if !sr1.Repaired {
		t.Error("first run should repair stale contract")
	}

	// Second run — no-op.
	sr2 := SyncManagedBlocks(dir)
	if sr2.Repaired {
		t.Errorf("second run should be no-op but got Repaired=true; lines: %v", sr2.Lines)
	}
}
