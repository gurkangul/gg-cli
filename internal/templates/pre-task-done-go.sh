#!/bin/sh
# gg pre-task-done hook: Go project verify gate (BLOCKING).
# Runs BEFORE `gg task done` writes the new state. Non-zero exit aborts
# with ExitVerifyFailed(7) and the task stays in its current state.
#
# Env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#   GG_ACTOR         — GG_ROLE or GG_AGENT of the caller
#
# Edit freely — this file ships as a starting point, not a contract.

set -e

echo "[verify] go build ./..."
go build ./...

echo "[verify] go vet ./..."
go vet ./...

echo "[verify] go test ./..."
go test ./... -count=1 -timeout 120s

echo "[verify] ✓ all checks passed for $GG_TASK_ID"
