#!/bin/sh
# Regression gate: run all registered bug repro scripts before allowing task-done.
#
# GG_ENFORCEMENT controls behaviour:
#   on (default)  — run and block (exit 7) on any regression
#   warn          — run and report, but do not block
#   off           — skip entirely
#
# Repro scripts are attached to fixed bugs via 'gg bug attach-repro'.
# Each script exits 0 if the fix is still in place, non-zero on regression.

MODE="${GG_ENFORCEMENT:-on}"

if [ "$MODE" = "off" ]; then
  exit 0
fi

gg bug run-repros
STATUS=$?

if [ "$STATUS" -ne 0 ]; then
  if [ "$MODE" = "warn" ]; then
    echo "⚠  Bug repro failures detected (GG_ENFORCEMENT=warn — not blocking)" >&2
    exit 0
  fi
  # on: block the task-done transition
  exit 7
fi

exit 0
