#!/bin/sh
# Repro for BUG-099: the AC-attestation gate documents GG_ALLOW_INCOMPLETE_AC as
# its audited escape hatch, but the pre-task-done chain runs the repro gate in the
# SAME environment. The TestACAttestation_* tests built their env from
# os.Environ(), so the exported bypass leaked in, flipped the hook onto the bypass
# path and failed the suite — meaning using the documented bypass for gate A
# guaranteed gate B refused, and the task could not be closed at all.
#
# At the broken ref this run fails with the bypass exported.
set -e
cd "$(git rev-parse --show-toplevel)"

# The tests t.Skip when the hook script is absent (.gg/ is gitignored, so a fresh
# worktree has none). Without this guard the whole suite would skip and the repro
# would pass vacuously — proving nothing.
HOOK=".gg/hooks/pre-task-done.d/50-ac-attestation.sh"
if [ ! -f "$HOOK" ]; then
  echo "FAIL: $HOOK missing — the AC tests would skip and this guard would prove nothing"
  exit 1
fi

GG_ALLOW_INCOMPLETE_AC="bug-099 repro: ambient bypass must not change what these tests exercise" \
  go test -count=1 -run 'TestACAttestation' ./cmd/

echo "PASS: BUG-099 — AC attestation tests are immune to an ambient bypass env"
