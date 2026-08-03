#!/bin/sh
# gg pre-task-done hook: stub-scan gate.
#
# Flags stub markers that this task's diff ADDS to a source file: TODO, FIXME,
# XXX, HACK, "not implemented", "unimplemented". The contract's engineering
# baseline says to ship no TODO stubs, dead flags, or half-wired paths described
# as done — this is the mechanically checkable half of that line.
#
# Only ADDED lines are scanned, never whole files. A pre-existing marker in a
# file you happen to touch is somebody else's debt; blocking on it would punish
# the next person to walk past rather than the person who left it.
#
# Modes (via GG_STUB_GATE):
#   warn  (default) — print findings, exit 0 (task close proceeds)
#   block           — print findings, exit 7 (task stays in current state)
#   off             — skip entirely
# Bypass in block mode: set GG_ALLOW_STUB=<reason> to log + skip.
#
# Env vars available:
#   GG_TASK_ID, GG_TASK_SUMMARY, GG_PROJECT_ID, GG_ACTOR

MODE="${GG_STUB_GATE:-warn}"
if [ "$MODE" = "off" ]; then
  exit 0
fi

# Same diff resolution as the sibling gates: uncommitted work first, last commit
# as the fallback once the work is already committed.
RANGE="HEAD"
CHANGED=$(git diff --name-only HEAD 2>/dev/null)
if [ -z "$CHANGED" ]; then
  RANGE="HEAD~1 HEAD"
  CHANGED=$(git diff --name-only HEAD~1 HEAD 2>/dev/null)
fi
if [ -z "$CHANGED" ]; then
  exit 0
fi

FINDINGS=""

for f in $CHANGED; do
  [ -f "$f" ] || continue

  case "$f" in
    vendor/*|node_modules/*|testdata/*|_bmad*|_bmad-output*|docs/cli/*|*.pb.go|*_generated.go) continue ;;
  esac

  # Source languages only. This deliberately excludes .md and .sh, so a document
  # or a hook that merely names these markers is never a finding.
  case "$f" in
    *.go|*.ts|*.tsx|*.js|*.jsx|*.py|*.rs|*.java) ;;
    *) continue ;;
  esac

  # -U0 drops context lines, so nothing but genuinely added lines is matched.
  # The trailing delimiter class keeps identifiers like "TODOS_ENDPOINT" out.
  HITS=$(git diff -U0 $RANGE -- "$f" 2>/dev/null \
    | grep '^+' | grep -v '^+++' \
    | grep -E '(TODO|FIXME|XXX|HACK)([^A-Za-z0-9_]|$)|[Nn]ot implemented|[Uu]nimplemented|NotImplemented' \
    | sed 's/^+[[:space:]]*//')

  [ -n "$HITS" ] || continue

  COUNT=$(printf '%s\n' "$HITS" | wc -l | tr -d ' ')
  FIRST=$(printf '%s\n' "$HITS" | head -1 | cut -c1-90)
  FINDINGS="$FINDINGS\n  $f  ($COUNT added) — $FIRST"
done

if [ -z "$FINDINGS" ]; then
  exit 0
fi

printf "[stub-gate] This task adds stub markers:%b\n" "$FINDINGS" >&2
echo "[stub-gate] Finish the path, or open a gg task for the remainder instead of leaving a marker." >&2

if [ "$MODE" = "block" ]; then
  if [ -n "$GG_ALLOW_STUB" ]; then
    if command -v gg >/dev/null 2>&1; then
      gg record "stub gate bypassed" \
        --reason "GG_ALLOW_STUB=${GG_ALLOW_STUB}; task=${GG_TASK_ID}" \
        --tags "bypass,stub-gate" 2>/dev/null || true
    fi
    printf "[stub-gate] bypass accepted (reason: %s)\n" "$GG_ALLOW_STUB" >&2
    exit 0
  fi
  echo "[stub-gate] Set GG_STUB_GATE=warn to downgrade, or GG_ALLOW_STUB=<reason> to bypass." >&2
  exit 7
fi

# warn mode: non-blocking
exit 0
