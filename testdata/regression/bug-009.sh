#!/bin/sh
# Repro for BUG-009: brain import failed on fresh Qdrant collections because
# vector is mandatory but was not seeded. Fix: UpsertBrainRecords seeds a
# zero-vector placeholder detected by isZeroVector() for later re-embedding.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestIsZeroVector_DetectsPlaceholder ./internal/store/
