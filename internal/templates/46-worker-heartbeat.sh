#!/bin/sh
# gg task-done hook: worker heartbeat ping (AC9).
# Runs AFTER `gg task done` writes the new state (task-done.d/).
# The worker pings the master by calling `gg spawn heartbeat` so the master's
# liveness clock is refreshed at natural task-completion boundaries.
#
# This is intentionally a best-effort hook — if the master is unreachable the
# worker should not be blocked from completing its task.
#
# Env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#   GG_ACTOR         — GG_ROLE or GG_AGENT of the caller

# Do not use set -e here — heartbeat failure must not abort the hook.

TASK_ID="${GG_TASK_ID:-}"

# Call gg spawn heartbeat in the background so the hook returns immediately.
# The --worker flag signals that this heartbeat originates from a worker pane.
if gg spawn heartbeat --worker 2>/dev/null; then
  echo "[worker-heartbeat] ✓ heartbeat ping sent (task: ${TASK_ID:-unknown})"
else
  echo "[worker-heartbeat] ⚠ heartbeat ping failed (non-fatal)"
fi
