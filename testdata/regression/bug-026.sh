#!/bin/sh
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

grep -Fq 'impact[[:space:]]+[^:]+:' internal/templates/pre-task-done-impact-attestation.sh
grep -Fq 'impact:[[:space:]]+' internal/templates/pre-task-done-impact-attestation.sh
go test ./cmd -run 'TestImpactAttestation_LowercaseImpactFileTrailerSatisfies|TestImpactAttestation_CompactImpactLineSatisfies|TestImpactAttestation_TrailerSatisfies' -count=1
