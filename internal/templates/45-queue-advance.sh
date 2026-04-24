#!/bin/sh
# gg task-done hook: queue advance (AC6).
# Runs AFTER `gg task done` writes the new state (task-done.d/).
# Signals the queue runner that the current task completed so it can
# advance to the next pending task.
#
# Mechanism: touches a per-task sentinel file in the spawn dir that the
# queue runner polls. The runner removes it after reading.
#
# Env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#   GG_ACTOR         — GG_ROLE or GG_AGENT of the caller

set -e

TASK_ID="${GG_TASK_ID:-}"
if [ -z "$TASK_ID" ]; then
  # No task context — not running inside a queue worker.
  exit 0
fi

# Resolve the spawn advance dir.
SPAWN_ADVANCE_DIR="${GG_SPAWN_ADVANCE_DIR:-}"
if [ -z "$SPAWN_ADVANCE_DIR" ]; then
  # Derive from gg spawn status if the env var is not injected.
  RUNTIME_DIR=$(gg config get runtime_dir 2>/dev/null || true)
  if [ -n "$RUNTIME_DIR" ]; then
    SPAWN_ADVANCE_DIR="$RUNTIME_DIR/spawn/advance"
  fi
fi

if [ -z "$SPAWN_ADVANCE_DIR" ]; then
  # Cannot locate spawn dir — advance signal is a best-effort.
  exit 0
fi

mkdir -p "$SPAWN_ADVANCE_DIR"
SENTINEL="$SPAWN_ADVANCE_DIR/${TASK_ID}.done"
echo "${GG_TASK_SUMMARY:-done}" > "$SENTINEL"
echo "[queue-advance] ✓ advance sentinel written for $TASK_ID"
