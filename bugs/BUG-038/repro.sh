#!/bin/sh
set -eu

test_file="cmd/bug038_repro_test.go"
cleanup() {
  rm -f "$test_file"
}
trap cleanup EXIT INT TERM

mkdir -p .gg/hooks/pre-task-done.d
cp internal/templates/pre-task-done-ac-attestation.sh .gg/hooks/pre-task-done.d/50-ac-attestation.sh
chmod +x .gg/hooks/pre-task-done.d/50-ac-attestation.sh

cat > "$test_file" <<'GO'
package cmd

import (
	"strings"
	"testing"
)

func TestBUG038_Task366SectionsOnlyFive(t *testing.T) {
	detail := `Permanent solution for BUG-037. Five layers, ALL required:

AC-1: Template versioning via content-sha256.
  - For every template constant in internal/templates/: compute sha256(content).
  - Embed marker as two header lines near the top of the deployed file.

AC-2: Drift detection in gg doctor.
  - New section in 'gg doctor' output: 'Hook templates'.

AC-3: gg doctor --refresh-hooks flag.
  - Backup deployed file to <path>.bak.<unix-timestamp>.

AC-4: User-customize protection.
  - Files without 'gg-template-sha256:' header are NEVER touched.

AC-5: gg system sync integration.
  - Suggest action line at end.

ENGINEERING NOTES:
- Template marker is the SINGLE source of truth.

LIVE VERIFICATION (commit body):
1. Build: go install ./cmd/gg
2. Run 'gg doctor' on this repo.

OUT OF SCOPE:
- gg-templates registry CLI.

FILE-SIZE CAP:
- cmd/doctor_install.go MUST stay under 500 lines.

REGRESSION PROOF (commit body):
- Each AC line MUST cite file:line where AC is implemented.`

	commitMsg := `fix: cover explicit ACs

AC-1: covered
AC-2: covered
AC-3: covered
AC-4: covered
AC-5: covered`

	out, code := runACAttestationHook(t, taskJSONWith(detail), commitMsg, nil)
	if code != 0 {
		t.Fatalf("expected only AC-1..AC-5 to be counted, code=%d\n%s", code, out)
	}
	if !strings.Contains(out, "found 5 acceptance criterion/criteria") {
		t.Fatalf("expected exactly 5 ACs, got:\n%s", out)
	}
}
GO

go test ./cmd -run TestBUG038_Task366SectionsOnlyFive -count=1
