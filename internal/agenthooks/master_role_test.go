package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMasterRoleBody_LoadsFromTemplate(t *testing.T) {
	body := MasterRoleBody()
	if body == "" {
		t.Fatal("MasterRoleBody() is empty — master-role.md not embedded")
	}
	if !strings.Contains(body, "MASTER ROLE") {
		t.Errorf("MasterRoleBody() missing expected heading: %q", body[:min(len(body), 80)])
	}
}

func TestMasterRoleBlock_Deterministic(t *testing.T) {
	a := MasterRoleBlock()
	b := MasterRoleBlock()
	if a != b {
		t.Error("MasterRoleBlock() is not deterministic across calls")
	}
	if !strings.HasPrefix(a, MasterRoleBlockBegin) {
		t.Errorf("MasterRoleBlock() must start with begin marker, got: %q", a[:min(len(a), 60)])
	}
	if !strings.HasSuffix(strings.TrimRight(a, "\n"), MasterRoleBlockEnd) {
		t.Errorf("MasterRoleBlock() must end with end marker (before trailing newline)")
	}
}

func TestMasterRoleVersion_IsHexSHA256(t *testing.T) {
	v := MasterRoleVersion()
	if len(v) != 64 {
		t.Errorf("MasterRoleVersion() length = %d, want 64 hex chars", len(v))
	}
	for _, c := range v {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("MasterRoleVersion() contains non-hex char %q", c)
		}
	}
}

func TestCheckMasterRole_OK(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte(MasterRoleBlock()), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	r := CheckMasterRole(dir)
	if r.Status != MasterRoleOK {
		t.Errorf("want MasterRoleOK, got %s (found=%s, want=%s)",
			r.Status, r.FoundVersion[:8], r.WantVersion[:8])
	}
}

func TestCheckMasterRole_Missing_NoFile(t *testing.T) {
	dir := t.TempDir()
	r := CheckMasterRole(dir)
	if r.Status != MasterRoleMISSING {
		t.Errorf("want MasterRoleMISSING when CLAUDE.md absent, got %s", r.Status)
	}
}

func TestCheckMasterRole_Missing_NoMarkers(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte("# My Project\n\nSome content.\n"), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	r := CheckMasterRole(dir)
	if r.Status != MasterRoleMISSING {
		t.Errorf("want MasterRoleMISSING when markers absent, got %s", r.Status)
	}
}

func TestCheckMasterRole_Stale(t *testing.T) {
	dir := t.TempDir()
	staleBlock := MasterRoleBlockBegin + "\n## OLD MASTER ROLE\n\nold content\n" + MasterRoleBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(staleBlock), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	r := CheckMasterRole(dir)
	if r.Status != MasterRoleSTALE {
		t.Errorf("want MasterRoleSTALE, got %s", r.Status)
	}
}

func TestCheckMasterRole_Drifted(t *testing.T) {
	dir := t.TempDir()
	drifted := "# project\n\n" + MasterRoleBlockBegin + "\ncontent without end marker\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(drifted), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	r := CheckMasterRole(dir)
	if r.Status != MasterRoleDRIFTED {
		t.Errorf("want MasterRoleDRIFTED, got %s", r.Status)
	}
}

func TestFixMasterRole_RepairsMissing(t *testing.T) {
	dir := t.TempDir()

	lines, err := FixMasterRole(dir, false)
	if err != nil {
		t.Fatalf("FixMasterRole: %v", err)
	}

	data, readErr := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if readErr != nil {
		t.Fatalf("CLAUDE.md not created after fix: %v", readErr)
	}
	if !strings.Contains(string(data), MasterRoleBlockBegin) {
		t.Errorf("CLAUDE.md after fix missing begin marker:\n%s", string(data))
	}
	if !strings.Contains(string(data), MasterRoleBlockEnd) {
		t.Errorf("CLAUDE.md after fix missing end marker:\n%s", string(data))
	}

	found := false
	for _, l := range lines {
		if strings.Contains(l, "MISSING") {
			found = true
		}
	}
	if !found {
		t.Errorf("fix report did not mention MISSING; got: %v", lines)
	}
}

func TestFixMasterRole_RepairstStale(t *testing.T) {
	dir := t.TempDir()
	stale := MasterRoleBlockBegin + "\n## OLD\n\nstale body\n" + MasterRoleBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(stale), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := FixMasterRole(dir, false); err != nil {
		t.Fatalf("FixMasterRole: %v", err)
	}

	r := CheckMasterRole(dir)
	if r.Status != MasterRoleOK {
		t.Errorf("after fix: status = %s, want OK", r.Status)
	}
}

func TestFixMasterRole_RefusesDriftedWithoutForceReset(t *testing.T) {
	dir := t.TempDir()
	drifted := MasterRoleBlockBegin + "\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(drifted), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := FixMasterRole(dir, false)
	if err != nil {
		t.Fatalf("FixMasterRole returned unexpected error: %v", err)
	}

	refused := false
	for _, l := range lines {
		if strings.Contains(l, "DRIFTED") && strings.Contains(l, "force-reset") {
			refused = true
		}
	}
	if !refused {
		t.Errorf("expected DRIFTED refusal message; lines: %v", lines)
	}
}

// TestFixMasterRole_ForceResetOnDrifted verifies that --force-reset repairs
// malformed (DRIFTED) files by stripping orphaned marker fragments and
// appending the clean block.
func TestFixMasterRole_ForceResetOnDrifted(t *testing.T) {
	dir := t.TempDir()
	// Only begin marker — DRIFTED state.
	drifted := MasterRoleBlockBegin + "\nbody without end\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(drifted), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := FixMasterRole(dir, true)
	if err != nil {
		t.Fatalf("FixMasterRole --force-reset on DRIFTED: %v", err)
	}

	fixed := false
	for _, l := range lines {
		if strings.Contains(l, "DRIFTED") && strings.Contains(l, "fixed") {
			fixed = true
		}
	}
	if !fixed {
		t.Errorf("expected DRIFTED→fixed message; lines: %v", lines)
	}

	// File should now pass CheckMasterRole as OK.
	r := CheckMasterRole(dir)
	if r.Status != MasterRoleOK {
		t.Errorf("after force-reset: status = %s, want OK", r.Status)
	}
}

func TestCheckMasterRole_Extended(t *testing.T) {
	dir := t.TempDir()
	// Block has all template lines PLUS extra local content.
	extended := MasterRoleBlockBegin + "\n" + MasterRoleBody() + "\n## Our team: architect reviews DB migrations first\n" + MasterRoleBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(extended), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	r := CheckMasterRole(dir)
	if r.Status != MasterRoleEXTENDED {
		t.Errorf("want MasterRoleEXTENDED, got %s", r.Status)
	}
}

func TestFixMasterRole_ExtendedBacksUpAndFixes(t *testing.T) {
	dir := t.TempDir()
	// Create extended block: template + local addition.
	extended := MasterRoleBlockBegin + "\n" + MasterRoleBody() + "\n## Local Team Rule\nSome rule.\n" + MasterRoleBlockEnd + "\n"
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte(extended), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := FixMasterRole(dir, false)
	if err != nil {
		t.Fatalf("FixMasterRole on EXTENDED: %v", err)
	}

	// Backup should exist.
	backupFound := false
	for _, l := range lines {
		if strings.Contains(l, "backed up") {
			backupFound = true
		}
	}
	if !backupFound {
		t.Errorf("expected backup message in output; lines: %v", lines)
	}

	// File should now be OK.
	r := CheckMasterRole(dir)
	if r.Status != MasterRoleOK {
		t.Errorf("after EXTENDED fix: status = %s, want OK", r.Status)
	}

	// Backup file should contain the original extended content.
	backupDir := filepath.Join(dir, ".gg", "backups")
	entries, readErr := os.ReadDir(backupDir)
	if readErr != nil {
		t.Fatalf("backup dir not created: %v", readErr)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one backup file")
	}
	backupContent, _ := os.ReadFile(filepath.Join(backupDir, entries[0].Name()))
	if !strings.Contains(string(backupContent), "Local Team Rule") {
		t.Error("backup file should contain original extended content")
	}
}

func TestFixMasterRole_IdempotentWhenOK(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(MasterRoleBlock()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := FixMasterRole(dir, false)
	if err != nil {
		t.Fatalf("FixMasterRole: %v", err)
	}

	found := false
	for _, l := range lines {
		if strings.Contains(l, "no action needed") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'no action needed' in output; got: %v", lines)
	}
}

func TestWriteMasterRoleBlock_PreservesNonManagedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	existing := "# My Project CLAUDE.md\n\nHuman-authored content.\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := writeMasterRoleBlock(path, false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.HasPrefix(s, existing) {
		t.Errorf("original content should be preserved at start of file")
	}
	if !strings.Contains(s, MasterRoleBlockBegin) {
		t.Error("master-role begin marker missing")
	}
	if !strings.Contains(s, MasterRoleBlockEnd) {
		t.Error("master-role end marker missing")
	}
}

func TestWriteMasterRoleBlock_DryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	action, _, err := writeMasterRoleBlock(path, true)
	if err != nil {
		t.Fatalf("dry-run writeMasterRoleBlock: %v", err)
	}
	if action != ActionDryRun {
		t.Errorf("action = %q, want %q", action, ActionDryRun)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("dry-run should not create the file")
	}
}

func TestWriteMasterRoleBlock_IdempotentOnRerun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if _, _, err := writeMasterRoleBlock(path, false); err != nil {
		t.Fatal(err)
	}
	action, _, err := writeMasterRoleBlock(path, false)
	if err != nil {
		t.Fatalf("second writeMasterRoleBlock: %v", err)
	}
	if action != ActionUpToDate {
		t.Errorf("second call action = %q, want %q", action, ActionUpToDate)
	}
}
