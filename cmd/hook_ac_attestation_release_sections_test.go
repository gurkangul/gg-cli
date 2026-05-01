package cmd

import (
	"strings"
	"testing"
)

func TestACAttestation_Task366StyleDetailCountsOnlyExplicitACs(t *testing.T) {
	detail := `Permanent solution for BUG-037. Five layers, ALL required:

AC-1: Template versioning via content-sha256.
  - For every template constant in internal/templates/: compute sha256(content).
  - Embed marker as two header lines near the top of the deployed file.

AC-2: Drift detection in gg doctor.
  - New section in 'gg doctor' output: 'Hook templates'.
  - For each known hook path: compare marker to current shipped template hash.

AC-3: gg doctor --refresh-hooks flag.
  - Backup deployed file to <path>.bak.<unix-timestamp>.
  - Overwrite with current template body.

AC-4: User-customize protection.
  - Files without 'gg-template-sha256:' header are NEVER touched.
  - Add --refresh-hooks-force to override.

AC-5: gg system sync integration.
  - Output ordering: install-task-hooks first, then drift report.
  - Suggest action line at end.

ENGINEERING NOTES:
- Template marker is the SINGLE source of truth.
- New file: cmd/doctor_refresh_hooks.go.

UNIT TESTS:
* marker write on fresh install
* drift detection

LIVE VERIFICATION (commit body):
1. Build: go install ./cmd/gg
2. Run 'gg doctor' on this repo.

OUT OF SCOPE:
- gg-templates registry CLI.
- Auto-refresh in pre-commit / pre-task-done flows.

FILE-SIZE CAP:
- cmd/doctor_install.go MUST stay under 500 lines.

REGRESSION PROOF (commit body):
- Each AC line MUST cite file:line where AC is implemented.
- Footer: 'fixes: BUG-037'.`

	commitMsg := `feat(TASK-367): fix AC parser section skipping

AC-1: TASK-366-style detail yields only AC-1..AC-5.
AC-2: non-AC verification/notes/scope/size/regression sections are skipped.
AC-3: targeted parser tests pass.
AC-4: full race suite passes.
AC-5: commit references BUG-038 and includes impact footer.`

	out, code := runACAttestationHook(t, taskJSONWith(detail), commitMsg, nil)
	if code != 0 {
		t.Fatalf("expected exit 0 with only AC-1..AC-5 counted, got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "found 5 acceptance criterion/criteria") {
		t.Fatalf("expected exactly 5 AC anchors, got:\n%s", out)
	}
	for _, forbidden := range []string{"AC-6", "Template marker is the SINGLE source", "go install ./cmd/gg", "gg-templates registry CLI"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("non-AC section leaked into parser output via %q\noutput:\n%s", forbidden, out)
		}
	}
}
