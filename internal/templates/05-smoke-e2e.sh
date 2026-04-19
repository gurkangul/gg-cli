#!/bin/sh
# Test-tier smoke gate — runs `make test-smoke` before allowing task-done.
#
# The hook is written by `gg doctor --install-task-hooks` into every project
# so the hook file is ready when the team adopts the test-pyramid pattern
# (see .gg/templates/makefile-test-tiers.mk). Until then it is a quiet no-op.
#
# Skip matrix:
#   GG_NO_SMOKE=1            — per-session opt-out
#   no Makefile / makefile   — project has not adopted the tier pattern
#   no test-smoke target     — Makefile exists but target not defined
#
# On blocking failure, exit 1 — the pre-task-done runner wraps this as
# exit 7 (ExitVerifyFailed) and the task stays in its current state.

if [ "${GG_NO_SMOKE:-0}" = "1" ]; then
  exit 0
fi

if [ ! -f Makefile ] && [ ! -f makefile ]; then
  exit 0
fi

# `make -n` resolves the target without executing recipes; absence of the
# target produces a non-zero exit that we treat as "not adopted yet".
if ! make -n test-smoke >/dev/null 2>&1; then
  exit 0
fi

echo "→ gg pre-task-done smoke gate: make test-smoke" >&2
if ! make test-smoke; then
  echo "✗ test-smoke failed — task blocked (set GG_NO_SMOKE=1 to bypass intentionally)" >&2
  exit 1
fi
exit 0
