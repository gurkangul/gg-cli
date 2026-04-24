#!/bin/sh
# gg pre-task-done hook: master-guard liveness check (BLOCKING).
# Runs BEFORE `gg task done` writes the new state. Non-zero exit aborts
# with ExitVerifyFailed(7) and the task stays in its current state.
#
# This hook is installed by `gg spawn queue` when a worker session is active.
# It verifies the master orchestrator session is still alive (heartbeat fresh)
# before a worker task is allowed to close.
#
# Bypass: set GG_NO_MASTER_GUARD=1 in the worker session env to skip this
# check (emergency use only — e.g. master terminal crashed mid-run).
#
# Env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#   GG_ACTOR         — GG_ROLE or GG_AGENT of the caller

set -e

if [ "${GG_NO_MASTER_GUARD:-0}" = "1" ]; then
  echo "[master-guard] bypass active (GG_NO_MASTER_GUARD=1) — skipping liveness check"
  exit 0
fi

echo "[master-guard] checking master liveness for $GG_TASK_ID..."

# gg spawn status --json exits 0 and sets master_alive=true when heartbeat is fresh.
if ! gg spawn status --json 2>/dev/null | grep -q '"master_alive": *true'; then
  echo "[master-guard] ✗ master session heartbeat is stale or absent"
  echo "[master-guard]   run 'gg spawn heartbeat' from the master terminal, or"
  echo "[master-guard]   set GG_NO_MASTER_GUARD=1 to bypass this check."
  exit 1
fi

echo "[master-guard] ✓ master alive — allowing task done for $GG_TASK_ID"
