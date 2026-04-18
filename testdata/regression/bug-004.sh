#!/bin/sh
# Repro for BUG-004: agent detection for global Claude install missed 5 signals.
# Fix: Claude detector now checks all 5 global signals and reports ActionSuggested.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestClaude_GlobalSignals_AllFiveSignals ./internal/agenthooks/
