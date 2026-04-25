#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

go test ./cmd -run 'TestACAttestation_ImplementationHintsBullets_NotCounted|TestACAttestation_FixReworkBullets_StillSkipped' -count=1
