package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevRoutingBody_LoadsFromTemplate(t *testing.T) {
	body := DevRoutingBody()
	if body == "" {
		t.Fatal("DevRoutingBody() is empty — dev-routing.md not embedded")
	}
	if !strings.Contains(body, "Developer Routing") {
		t.Errorf("DevRoutingBody() missing expected heading: %q", body[:min(len(body), 80)])
	}
}

func TestDevRoutingBlock_Deterministic(t *testing.T) {
	a := DevRoutingBlock()
	b := DevRoutingBlock()
	if a != b {
		t.Error("DevRoutingBlock() is not deterministic across calls")
	}
	if !strings.HasPrefix(a, DevRoutingBlockBegin) {
		t.Errorf("DevRoutingBlock() must start with begin marker, got: %q", a[:min(len(a), 60)])
	}
	if !strings.HasSuffix(strings.TrimRight(a, "\n"), DevRoutingBlockEnd) {
		t.Errorf("DevRoutingBlock() must end with end marker (before trailing newline)")
	}
}

func TestDevRoutingVersion_IsHexSHA256(t *testing.T) {
	v := DevRoutingVersion()
	if len(v) != 64 {
		t.Errorf("DevRoutingVersion() length = %d, want 64 hex chars", len(v))
	}
	for _, c := range v {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("DevRoutingVersion() contains non-hex char %q", c)
		}
	}
}

func TestCheckDevRouting_OK(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte(DevRoutingBlock()), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	r := CheckDevRouting(dir)
	if r.Status != DevRoutingOK {
		t.Errorf("want DevRoutingOK, got %s (found=%s, want=%s)",
			r.Status, r.FoundVersion[:8], r.WantVersion[:8])
	}
}

func TestCheckDevRouting_Missing_NoFile(t *testing.T) {
	dir := t.TempDir()
	r := CheckDevRouting(dir)
	if r.Status != DevRoutingMISSING {
		t.Errorf("want DevRoutingMISSING when CLAUDE.md absent, got %s", r.Status)
	}
}

func TestCheckDevRouting_Missing_NoMarkers(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte("# My Project\n\nSome content.\n"), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	r := CheckDevRouting(dir)
	if r.Status != DevRoutingMISSING {
		t.Errorf("want DevRoutingMISSING when markers absent, got %s", r.Status)
	}
}

func TestCheckDevRouting_Stale(t *testing.T) {
	dir := t.TempDir()
	staleBlock := DevRoutingBlockBegin + "\n## OLD DEV ROUTING\n\nold content\n" + DevRoutingBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(staleBlock), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	r := CheckDevRouting(dir)
	if r.Status != DevRoutingSTALE {
		t.Errorf("want DevRoutingSTALE, got %s", r.Status)
	}
}

func TestCheckDevRouting_Drifted(t *testing.T) {
	dir := t.TempDir()
	drifted := "# project\n\n" + DevRoutingBlockBegin + "\ncontent without end marker\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(drifted), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	r := CheckDevRouting(dir)
	if r.Status != DevRoutingDRIFTED {
		t.Errorf("want DevRoutingDRIFTED, got %s", r.Status)
	}
}

func TestFixDevRouting_RepairsMissing(t *testing.T) {
	dir := t.TempDir()

	lines, err := FixDevRouting(dir, false)
	if err != nil {
		t.Fatalf("FixDevRouting: %v", err)
	}

	data, readErr := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if readErr != nil {
		t.Fatalf("CLAUDE.md not created after fix: %v", readErr)
	}
	if !strings.Contains(string(data), DevRoutingBlockBegin) {
		t.Errorf("CLAUDE.md after fix missing begin marker:\n%s", string(data))
	}
	if !strings.Contains(string(data), DevRoutingBlockEnd) {
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

func TestFixDevRouting_RepairstStale(t *testing.T) {
	dir := t.TempDir()
	stale := DevRoutingBlockBegin + "\n## OLD\n\nstale body\n" + DevRoutingBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(stale), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := FixDevRouting(dir, false); err != nil {
		t.Fatalf("FixDevRouting: %v", err)
	}

	r := CheckDevRouting(dir)
	if r.Status != DevRoutingOK {
		t.Errorf("after fix: status = %s, want OK", r.Status)
	}
}

func TestFixDevRouting_RefusesDriftedWithoutForceReset(t *testing.T) {
	dir := t.TempDir()
	drifted := DevRoutingBlockBegin + "\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(drifted), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := FixDevRouting(dir, false)
	if err != nil {
		t.Fatalf("FixDevRouting returned unexpected error: %v", err)
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

func TestFixDevRouting_ForceResetOnDrifted(t *testing.T) {
	dir := t.TempDir()
	drifted := DevRoutingBlockBegin + "\nbody without end\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(drifted), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := FixDevRouting(dir, true)
	if err != nil {
		t.Fatalf("FixDevRouting --force-reset on DRIFTED: %v", err)
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

	r := CheckDevRouting(dir)
	if r.Status != DevRoutingOK {
		t.Errorf("after force-reset: status = %s, want OK", r.Status)
	}
}

func TestFixDevRouting_IdempotentWhenOK(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(DevRoutingBlock()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := FixDevRouting(dir, false)
	if err != nil {
		t.Fatalf("FixDevRouting: %v", err)
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

// TestFixMasterRole_UpgradesV2ToV3 covers the brownfield upgrade scenarios where
// an older v2 master-role marker is on disk. FixMasterRole (with forceReset=true)
// must strip ALL legacy marker variants, not just the current v3 begin marker.
func TestFixMasterRole_UpgradesV2ToV3(t *testing.T) {
	const v2Begin = "<!-- gg:master-role:begin v2 -->"
	const v2End = "<!-- gg:master-role:end -->" // end marker is version-agnostic

	t.Run("MISSING_file", func(t *testing.T) {
		dir := t.TempDir()
		lines, err := FixMasterRole(dir, false)
		if err != nil {
			t.Fatalf("FixMasterRole on missing file: %v", err)
		}
		found := false
		for _, l := range lines {
			if strings.Contains(l, "MISSING") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected MISSING in output; got: %v", lines)
		}
		data, readErr := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
		if readErr != nil {
			t.Fatalf("CLAUDE.md not created: %v", readErr)
		}
		content := string(data)
		if strings.Count(content, MasterRoleBlockBegin) != 1 {
			t.Errorf("expected exactly one v3 begin marker; content:\n%s", content)
		}
		if strings.Contains(content, v2Begin) {
			t.Errorf("v2 begin marker must not appear after fix; content:\n%s", content)
		}
	})

	t.Run("DRIFTED_v2_only_begin", func(t *testing.T) {
		dir := t.TempDir()
		// Only the v2 begin marker present — no end marker — DRIFTED from the
		// perspective of any checker that doesn't know about v2.
		diskContent := "# project\n\n" + v2Begin + "\nsome old master-role content\n"
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(diskContent), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		lines, err := FixMasterRole(dir, true) // force-reset required for DRIFTED
		if err != nil {
			t.Fatalf("FixMasterRole force-reset: %v", err)
		}
		_ = lines

		data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
		content := string(data)

		if strings.Contains(content, v2Begin) {
			t.Errorf("v2 begin marker must be stripped after force-reset; content:\n%s", content)
		}
		if strings.Count(content, MasterRoleBlockBegin) != 1 {
			t.Errorf("expected exactly one v3 begin marker after fix; content:\n%s", content)
		}
		if strings.Count(content, MasterRoleBlockEnd) != 1 {
			t.Errorf("expected exactly one end marker after fix; content:\n%s", content)
		}
	})

	t.Run("DRIFTED_v2_full_block", func(t *testing.T) {
		dir := t.TempDir()
		// Full v2 block (begin + body + end) — checker sees MISSING because v2 begin
		// differs from v3 begin (startIdx == -1). replaceOrAppendBlock then errors on
		// the lone end marker. After the stripLegacyMarkers fix, the expected state is:
		// exactly one v3 begin, no v2 begin, exactly one end marker.
		diskContent := "# project\n\n" + v2Begin + "\n## MASTER ROLE\n\nold body\n" + v2End + "\n"
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(diskContent), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		_, err := FixMasterRole(dir, false)
		if err != nil {
			t.Fatalf("FixMasterRole: %v", err)
		}

		data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
		content := string(data)

		if strings.Count(content, MasterRoleBlockBegin) != 1 {
			t.Errorf("expected exactly one v3 begin marker; content:\n%s", content)
		}
		if strings.Contains(content, v2Begin) {
			t.Errorf("v2 begin marker (orphan) must be stripped; content:\n%s", content)
		}
		// End marker: the v2 end is identical to the v3 end (version-agnostic).
		// After fix there should be exactly one end marker.
		if strings.Count(content, MasterRoleBlockEnd) != 1 {
			t.Errorf("expected exactly one end marker; content:\n%s", content)
		}
	})
}

// TestFixDevRouting_UpgradesLegacyMarker is a regression-prevention test for
// future dev-routing version bumps. It uses a synthetic v0 marker (dev-routing
// is currently v1) to verify that if a legacy begin marker ends up on disk, the
// fix path strips it cleanly rather than leaving an orphan line.
func TestFixDevRouting_UpgradesLegacyMarker(t *testing.T) {

	const v0Begin = "<!-- gg:dev-routing:begin v0 -->"

	dir := t.TempDir()
	// Synthetic v0 full block — v0Begin is unknown to CheckDevRouting (which only
	// searches for v1 begin) but the v1 end marker IS present, so the checker sees
	// DRIFTED (begin absent, end present). After stripLegacyMarkers is added, the
	// expected invariants are:
	//   - exactly one v1 begin marker
	//   - no v0 begin marker remaining as orphan
	diskContent := v0Begin + "\n## Developer Routing (old)\n\nlegacy content\n" + DevRoutingBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(diskContent), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := FixDevRouting(dir, false)
	if err != nil {
		t.Fatalf("FixDevRouting: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	content := string(data)

	if strings.Count(content, DevRoutingBlockBegin) != 1 {
		t.Errorf("expected exactly one v1 begin marker after fix; content:\n%s", content)
	}
	if strings.Contains(content, v0Begin) {
		t.Errorf("legacy v0 begin marker must be stripped after fix; content:\n%s", content)
	}
}

// TestDevRouting_CoexistsWithMasterRole verifies that both managed blocks can
// be installed into the same CLAUDE.md without overlap or duplication.
func TestDevRouting_CoexistsWithMasterRole(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")

	// Write dev-routing block first.
	if _, _, err := writeDevRoutingBlock(claudeMD, false); err != nil {
		t.Fatalf("writeDevRoutingBlock: %v", err)
	}
	// Append master-role block to the same file.
	if _, _, err := writeMasterRoleBlock(claudeMD, false); err != nil {
		t.Fatalf("writeMasterRoleBlock: %v", err)
	}

	data, _ := os.ReadFile(claudeMD)
	content := string(data)

	// Both begin markers must be present.
	if !strings.Contains(content, DevRoutingBlockBegin) {
		t.Error("dev-routing begin marker missing from combined CLAUDE.md")
	}
	if !strings.Contains(content, MasterRoleBlockBegin) {
		t.Error("master-role begin marker missing from combined CLAUDE.md")
	}

	// No overlap: dev-routing end must appear before master-role begin.
	devEnd := strings.Index(content, DevRoutingBlockEnd)
	masterBegin := strings.Index(content, MasterRoleBlockBegin)
	if devEnd < 0 || masterBegin < 0 || devEnd >= masterBegin {
		t.Errorf("blocks overlap or are out of order: devEnd=%d masterBegin=%d", devEnd, masterBegin)
	}

	// Both check functions must report OK.
	if r := CheckDevRouting(dir); r.Status != DevRoutingOK {
		t.Errorf("CheckDevRouting after coexistence write: %s", r.Status)
	}
	if r := CheckMasterRole(dir); r.Status != MasterRoleOK {
		t.Errorf("CheckMasterRole after coexistence write: %s", r.Status)
	}
}
