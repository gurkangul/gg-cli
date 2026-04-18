#!/bin/sh
# Repro for BUG-006: gg brain status always reported Drift: unknown because
# the conditional in computeDrift was always false. Fix: extracted computeDrift
# as a testable helper with correct logic.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestBrainStatus_ComputeDrift ./cmd/
