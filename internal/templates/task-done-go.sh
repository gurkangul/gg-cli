#!/bin/sh
# gg task-done hook: Go project quality gate
# Runs after `gg task done` to catch regressions before the summary is final.
#
# env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#
# Exit non-zero to trigger a warning (or block in strict mode).

set -e

echo "[hook] go vet ./..."
go vet ./...

echo "[hook] go test ./..."
go test ./... -count=1 -timeout 120s

# golangci-lint is optional — skip if not installed.
if command -v golangci-lint >/dev/null 2>&1; then
    echo "[hook] golangci-lint run"
    golangci-lint run
fi

echo "[hook] task-done-go: all checks passed"
