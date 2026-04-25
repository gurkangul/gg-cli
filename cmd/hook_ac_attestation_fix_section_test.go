// Package cmd — tests for TASK-312: FIX/REWORK section bullets must not be
// treated as acceptance criteria by the AC attestation hook.
package cmd

import "testing"

// TestACAttestation_FixSectionBulletsNotCounted: FIX/REWORK bullets are
// implementation steps, not ACs. Only ACCEPTANCE bullets require attestation.
func TestACAttestation_FixSectionBulletsNotCounted(t *testing.T) {
	for _, section := range []string{"FIX", "REWORK"} {
		t.Run(section, func(t *testing.T) {
			detail := "ACCEPTANCE\n- gate blocks on missing AC\n\n" + section + "\n- step 1: add regex\n- step 2: add test"
			commitMsg := "fix: implement gate\n\nAC-1: gate blocks on missing AC"
			out, code := runACAttestationHook(t, taskJSONWith(detail), commitMsg, nil)
			if code != 0 {
				t.Errorf("expected exit 0 (%s bullets not ACs), got %d\noutput:\n%s", section, code, out)
			}
		})
	}
}

// TestACAttestation_GapInsideFixNotCounted: Gap lines inside a FIX section are
// implementation notes, not ACs — excluded even in strict colon form.
func TestACAttestation_GapInsideFixNotCounted(t *testing.T) {
	detail := "ACCEPTANCE\n- parser distinguishes fix steps from ACs\n\nFIX\nGap A: rewrite scoping logic\nGap B: add tests"
	commitMsg := "fix: scope gap extraction\n\nAC-1: parser distinguishes fix steps from ACs"
	out, code := runACAttestationHook(t, taskJSONWith(detail), commitMsg, nil)
	if code != 0 {
		t.Errorf("expected exit 0 (Gap items inside FIX not ACs), got %d\noutput:\n%s", code, out)
	}
}
