#!/bin/sh
# gg pre-task-done hook: Decision-capture gate.
#
# Warns (non-blocking by default) when a task is closed without any decision
# explicitly linked to it via `gg record --task <TASK-ID>`. Target: lift the
# record/done ratio (baseline ~12% at 2026-04-20) by making unrecorded closes
# visible at the moment they happen, not weeks later in an audit.
#
# Modes (via GG_DECIDE_GATE env var):
#   warn  (default) — print warning, exit 0 (task close proceeds)
#   block           — print warning, exit 7 (task stays in current state)
#   off             — skip entirely (no check, no output)
#
# Env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#   GG_ACTOR         — GG_ROLE or GG_AGENT of the caller
#
# Edit freely — this file ships as a starting point, not a contract.

set -e

mode="${GG_DECIDE_GATE:-warn}"
if [ "$mode" = "off" ]; then
  exit 0
fi

if [ -z "$GG_TASK_ID" ]; then
  # No task context (e.g. hook invoked manually) — nothing to check.
  exit 0
fi

# `gg task decisions` emits one line per linked decision (JSON array with --json).
# We use the plain text form and count non-empty lines to avoid a jq dependency.
count=$(gg task decisions "$GG_TASK_ID" 2>/dev/null | grep -c '^' || true)
# `grep -c '^'` on empty input returns 0; on "(no decisions)" single line returns 1.
# Treat the sentinel "(no decisions" line as zero.
if gg task decisions "$GG_TASK_ID" 2>/dev/null | grep -q '^(no decisions'; then
  count=0
fi

if [ "$count" -gt 0 ]; then
  echo "[decide-gate] ✓ $GG_TASK_ID has $count linked decision(s)"
  exit 0
fi

msg="[decide-gate] ⚠ $GG_TASK_ID closing without a linked decision.
  If this task made a design choice, record it:
    gg record \"<what you decided>\" --task $GG_TASK_ID --reason \"<why>\"
  For small bugfixes with no design choice, skip with:
    GG_DECIDE_GATE=off gg task done $GG_TASK_ID ..."

if [ "$mode" = "block" ]; then
  printf '%s\n' "$msg" >&2
  exit 7
fi

printf '%s\n' "$msg"
exit 0
