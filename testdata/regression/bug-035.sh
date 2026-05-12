#!/bin/sh
# BUG-035: doctor reconcile panicked on malformed brain UUIDs (e.UUID[:8] slice OOB).
# Pre-fix: cmd/doctor_reconcile.go sliced e.UUID[:8] without length check.
# Post-fix (494921b): validBrainUUID + shortBrainUUID helpers guard the slice and
#           invalidUUIDs counter surfaces malformed entries as a structured warning.
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

grep -q "func validBrainUUID" cmd/doctor_reconcile.go
grep -q "func shortBrainUUID" cmd/doctor_reconcile.go
grep -q "invalidUUIDs" cmd/doctor_reconcile.go
