#!/bin/sh
# gg pre-task-done hook: file-size modularity gate.
# Warns (or blocks) when source files exceed 500 lines or test files exceed 800 lines.
#
# BUG-107: also reports a non-blocking warning band at >=90% of the limit (450
# source / 720 test). Without it the gate said nothing at all until the wall was
# already behind you — a file could sit at 499/500 reporting "compliant" and the
# next two-line edit turned it into a violation. The band never affects the exit
# code, and it only covers files this task actually changed.
#
# Mode (env GG_FILE_SIZE_GATE): warn (default) | block | off
# Bypass in block mode: set GG_ALLOW_FILE_SIZE=<reason> to log + skip.
#
# Env vars available:
#   GG_TASK_ID, GG_TASK_SUMMARY, GG_PROJECT_ID, GG_ACTOR

MODE="${GG_FILE_SIZE_GATE:-warn}"
if [ "$MODE" = "off" ]; then
  exit 0
fi

# Determine changed files since last commit; fall back to working tree diff.
CHANGED=$(git diff --name-only HEAD 2>/dev/null)
if [ -z "$CHANGED" ]; then
  CHANGED=$(git diff --name-only HEAD~1 HEAD 2>/dev/null)
fi
if [ -z "$CHANGED" ]; then
  exit 0
fi

VIOLATIONS=""
NEAR=""

for f in $CHANGED; do
  [ -f "$f" ] || continue

  # Exclude paths that should never be gated.
  case "$f" in
    vendor/*|node_modules/*|testdata/*|_bmad*|_bmad-output*|docs/cli/*|*.pb.go|*_generated.go) continue ;;
  esac

  # Extension filter — only gate recognised source languages.
  case "$f" in
    *.go|*.ts|*.tsx|*.js|*.jsx|*.py|*.rs|*.java) ;;
    *) continue ;;
  esac

  # Determine threshold.
  case "$f" in
    *_test.go|*.test.ts|*.test.tsx|*.test.js|*.test.jsx|*.spec.ts|*.spec.tsx|*.spec.js|*.spec.jsx|*.spec.py)
      LIMIT=800 ;;
    *)
      LIMIT=500 ;;
  esac

  # tr strips the leading padding wc emits on BSD/macOS, which otherwise leaks
  # into every reported line as "(     495 lines, ...)".
  LINES=$(wc -l < "$f" 2>/dev/null | tr -d ' ' || echo 0)
  [ -n "$LINES" ] || LINES=0

  # Warning band: still compliant, but close enough that the next edit can push
  # it over. Collected before the baseline/violation logic so a `continue` below
  # can never swallow it. Never blocks.
  SOFT=$(( LIMIT * 90 / 100 ))
  if [ "$LINES" -le "$LIMIT" ] && [ "$LINES" -ge "$SOFT" ]; then
    NEAR="$NEAR\n  $f  ($LINES lines, limit $LIMIT — $(( LIMIT - LINES )) left)"
  fi

  # Baseline check: only flag if current count exceeds baseline.
  BASELINE_FILE=".gg/file-size-baseline.json"
  if [ -f "$BASELINE_FILE" ]; then
    # Use python/python3 to parse JSON if available; otherwise skip baseline check.
    BASELINE_LINES=""
    if command -v python3 >/dev/null 2>&1; then
      BASELINE_LINES=$(python3 -c "
import json, sys
try:
  d = json.load(open('$BASELINE_FILE'))
  print(d.get('files', {}).get('$f', ''))
except Exception:
  pass
" 2>/dev/null)
    elif command -v python >/dev/null 2>&1; then
      BASELINE_LINES=$(python -c "
import json, sys
try:
  d = json.load(open('$BASELINE_FILE'))
  print(d.get('files', {}).get('$f', ''))
except Exception:
  pass
" 2>/dev/null)
    fi

    if [ -n "$BASELINE_LINES" ] && [ "$BASELINE_LINES" -gt 0 ] 2>/dev/null; then
      # File is grandfathered: only flag if it grew beyond baseline AND beyond limit.
      if [ "$LINES" -le "$BASELINE_LINES" ] || [ "$LINES" -le "$LIMIT" ]; then
        continue
      fi
    fi
  fi

  if [ "$LINES" -gt "$LIMIT" ]; then
    VIOLATIONS="$VIOLATIONS\n  $f  ($LINES lines, limit $LIMIT)"
  fi
done

# The band is signal only — printed in every mode, before any exit decision.
if [ -n "$NEAR" ]; then
  printf "[file-size] approaching limit (not a violation):%b\n" "$NEAR" >&2
  echo "[file-size] split these on the next touch rather than at the wall." >&2
fi

if [ -z "$VIOLATIONS" ]; then
  exit 0
fi

printf "[file-size] Oversized files detected:%b\n" "$VIOLATIONS" >&2

if [ "$MODE" = "block" ]; then
  if [ -n "$GG_ALLOW_FILE_SIZE" ]; then
    # Log bypass entry if gg is available.
    if command -v gg >/dev/null 2>&1; then
      gg record "file-size gate bypassed" \
        --reason "GG_ALLOW_FILE_SIZE=${GG_ALLOW_FILE_SIZE}; task=${GG_TASK_ID}" \
        --tags "bypass,file-size" 2>/dev/null || true
    fi
    printf "[file-size] bypass accepted (reason: %s)\n" "$GG_ALLOW_FILE_SIZE" >&2
    exit 0
  fi
  echo "[file-size] Set GG_FILE_SIZE_GATE=warn to downgrade, or GG_ALLOW_FILE_SIZE=<reason> to bypass." >&2
  exit 1
fi

# warn mode: non-blocking
exit 0
