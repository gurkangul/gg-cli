// Package cmd — gap-section parser tests for .gg/hooks/pre-task-done.d/50-ac-attestation.sh.
// These tests cover narrative "Gap N" prose (should NOT be counted as ACs) and
// Gap items listed under a GAPS/ACCEPTANCE section header (should be counted).
package cmd

import (
	"strings"
	"testing"
)

// TestACAttestation_NarrativeGapNoColon_NotCounted: a WHY section that contains
// 'Gap 2 GSD did X' (no colon) is prose, not an AC definition.
// The hook must NOT extract it as an AC anchor, so the task passes with no ACs.
func TestACAttestation_NarrativeGapNoColon_NotCounted(t *testing.T) {
	detail := `WHY
This rework was triggered because TASK-292 Gap 2 GSD did X incorrectly.
Gap 2 was the cross-process flock issue described in the prior session.
We also saw Gap 3 appear in the context replay.

WHAT
Fix the flock implementation.`

	// No ACs at all — if Gap 2 / Gap 3 were extracted the hook would block.
	json := taskJSONWith(detail)
	out, code := runACAttestationHook(t, json, "fix: implement cross-process flock", nil)
	if code != 0 {
		t.Errorf("expected exit 0 (narrative Gap N without colon = no AC anchors), got %d\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "Gap 2") || strings.Contains(out, "Gap 3") {
		t.Errorf("narrative Gap references should not appear as AC anchors, got:\n%s", out)
	}
}

// TestACAttestation_GapUnderGapsHeader_Counted: 'Gap A' listed under a GAPS
// header (without colon) should be extracted as an AC because it is inside a
// recognised AC section.
func TestACAttestation_GapUnderGapsHeader_Counted(t *testing.T) {
	detail := `WHY
Rework cycle identified two gaps.

GAPS
Gap A no cross-process flock — only in-process sync.Map
Gap B word-boundary regex too broad`

	// Only attest Gap A; Gap B is left unattested → hook must block.
	json := taskJSONWith(detail)
	commitMsg := `fix: address gaps

AC-1: Gap A fixed via syscall.Flock`

	out, code := runACAttestationHook(t, json, commitMsg, nil)
	if code != 7 {
		t.Errorf("expected exit 7 (Gap B under GAPS header is an AC, not attested), got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "Gap B") {
		t.Errorf("output should mention unmatched Gap B, got:\n%s", out)
	}
}
