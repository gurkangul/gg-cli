#!/bin/sh
# gg PreToolUse hook: master-guard (BLOCKING).
# Installed in the MASTER session's hooks/pre-tool-use.d/.
# Blocks Edit/Write/MultiEdit tool calls from the master pane when this project
# has a configured developer command, or when a worker queue is active. The
# master coordinates and reviews; implementation must be routed through
# `gg spawn worker --task <id>` so the configured role command owns the code.
#
# Bypass: set GG_NO_MASTER_GUARD=1 in the master session env (emergency only).
# Env vars available (set by the Claude Code harness):
#   GG_TOOL_NAME  — the tool being invoked (Edit, Write, MultiEdit, etc.)
#   GG_TASK_ID    — current task (if set in the master session)
#   GG_ACTOR      — GG_ROLE or GG_AGENT of the caller

set -e

if [ "${GG_NO_MASTER_GUARD:-0}" = "1" ]; then
  exit 0
fi

# Only block code-writing tools in the master pane.
case "${GG_TOOL_NAME:-}" in
  Edit|Write|MultiEdit)
    ;;
  *)
    exit 0
    ;;
esac

developer_command=""
if [ -f ".gg/config.yaml" ]; then
  developer_command=$(
    awk '
      /^developer:[[:space:]]*$/ { in_dev=1; next }
      /^[^[:space:]#][^:]*:/ { in_dev=0 }
      in_dev && /^[[:space:]]+command:/ {
        line=$0
        sub(/^[[:space:]]+command:[[:space:]]*/, "", line)
        gsub(/^["'\''[:space:]]+|["'\''[:space:]]+$/, "", line)
        print line
        exit
      }
    ' .gg/config.yaml 2>/dev/null || true
  )
fi

if [ -n "$developer_command" ] && [ "$developer_command" != "unconfigured" ]; then
  echo "[master-guard] ✗ BLOCKED: master session must not call ${GG_TOOL_NAME} while developer.command is configured."
  echo "[master-guard]   Configured developer: $developer_command"
  echo "[master-guard]   Route implementation to a worker: gg spawn worker --task <TASK-ID>"
  echo "[master-guard]   Master owns routing/review; developer owns code."
  echo "[master-guard]   To override: set GG_NO_MASTER_GUARD=1"
  exit 1
fi

# Check whether a queue session is active (queue.json exists and has a current task).
SPAWN_DIR="${GG_SPAWN_DIR:-}"
if [ -z "$SPAWN_DIR" ]; then
  # Fall back to discovering via gg spawn status --json.
  if ! gg spawn status --json 2>/dev/null | grep -q '"current_task":.*"TASK-'; then
    # No configured developer and no active queue session — allow legacy solo projects.
    exit 0
  fi
  echo "[master-guard] ✗ BLOCKED: master session must not call ${GG_TOOL_NAME} while a worker queue is active."
  echo "[master-guard]   Route code edits to a worker via 'gg spawn worker --task <id>'."
  echo "[master-guard]   To override: set GG_NO_MASTER_GUARD=1"
  exit 1
fi

QUEUE_FILE="$SPAWN_DIR/queue.json"
if [ ! -f "$QUEUE_FILE" ]; then
  # No queue file — master may write freely.
  exit 0
fi

CURRENT=$(grep -o '"current_task":"TASK-[^"]*"' "$QUEUE_FILE" 2>/dev/null || true)
if [ -z "$CURRENT" ]; then
  # Queue exists but no current task — idle, master may write.
  exit 0
fi

echo "[master-guard] ✗ BLOCKED: master session must not call ${GG_TOOL_NAME} while a worker queue is active."
echo "[master-guard]   Current worker task: $CURRENT"
echo "[master-guard]   Route code edits to a worker via 'gg spawn worker --task <id>'."
echo "[master-guard]   To override: set GG_NO_MASTER_GUARD=1"
exit 1
