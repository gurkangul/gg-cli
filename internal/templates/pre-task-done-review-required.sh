#!/bin/sh
# gg pre-task-done hook: Review-required gate.
#
# Warns (non-blocking by default) when a task is closed without an explicit
# review via `gg task review TASK-NNN --approve`. Target: close the
# self-approval loophole surfaced 2026-04-22 where 10/10 recent done tasks
# had Author == ReadyForLiveBy and ReviewStatus=none — verifier_separation
# was only checking the --verifier string label, not review evidence.
#
# Modes (via GG_REVIEW_GATE env var):
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

mode="${GG_REVIEW_GATE:-warn}"
if [ "$mode" = "off" ]; then
  exit 0
fi

if [ -z "$GG_TASK_ID" ]; then
  # No task context (e.g. hook invoked manually) — nothing to check.
  exit 0
fi

# `gg task get <id> --json` emits the task object including ReviewStatus.
# Pretty-printed JSON has a space after the colon; match both compact and
# indented forms. Accept "approved" only — "none"/"rejected"/"" warn.
status=$(gg task get "$GG_TASK_ID" --json 2>/dev/null \
  | grep -o '"ReviewStatus":[[:space:]]*"[^"]*"' \
  | head -1 \
  | sed 's/.*"\([^"]*\)"$/\1/' || true)

if [ "$status" = "approved" ]; then
  echo "[review-gate] ✓ $GG_TASK_ID is approved"
  exit 0
fi

msg="[review-gate] ⚠ $GG_TASK_ID closing without explicit review (status=${status:-unset}).
  Have a different role read the diff, then:
    gg task review $GG_TASK_ID --approve --notes \"<what you verified>\"
  For trivial changes where review would be theatre, skip with:
    GG_REVIEW_GATE=off gg task done $GG_TASK_ID ..."

if [ "$mode" = "block" ]; then
  printf '%s\n' "$msg" >&2
  exit 7
fi

printf '%s\n' "$msg"
exit 0
