#!/bin/sh
set -eu

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT"

grep -q 'TestACAttestation_ImplementationHintsBullets_NotCounted' cmd/hook_ac_attestation_test.go
grep -q 'TestACAttestation_FixReworkBullets_StillSkipped' cmd/hook_ac_attestation_test.go

go test ./cmd -run 'TestACAttestation_ImplementationHintsBullets_NotCounted|TestACAttestation_FixReworkBullets_StillSkipped' -count=1
