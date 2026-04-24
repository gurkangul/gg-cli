#!/bin/sh
# gg pre-task-done hook: linter debt gate (BLOCKING).
#
# Grandfathers existing lint warnings captured in .gg/lint-baseline.json.
# Blocks `gg task done` if the current golangci-lint run produces MORE issues
# than the baseline. New code MUST NOT introduce new issues; existing debt is
# allowed to persist (it is tracked and visible but not newly added).
#
# Issue count method: parse the "N issues:" summary line that golangci-lint
# emits on stdout — stable across v1 and v2.
#
# Modes (env GG_LINT_GATE):
#   on    (default) — block (exit 7) when issue count increases
#   warn            — print warning, exit 0 (task close proceeds)
#   off             — skip entirely
#
# Bypass: GG_ALLOW_LINT_REGRESSIONS=<reason> — audited via gg record
#
# Capture/refresh baseline:
#   gg doctor --capture-lint-baseline
#
# Env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#   GG_ACTOR         — GG_ROLE or GG_AGENT of the caller

set -e

MODE="${GG_LINT_GATE:-on}"
if [ "$MODE" = "off" ]; then
  exit 0
fi

# ── 1. Locate golangci-lint ──────────────────────────────────────────────────
if ! command -v golangci-lint >/dev/null 2>&1; then
  echo "[lint-gate] golangci-lint not found — skipping (install via https://golangci-lint.run/usage/install/)" >&2
  exit 0
fi

# ── 2. Run linter, capture output ────────────────────────────────────────────
# golangci-lint exits non-zero when issues found — that is expected; capture
# the combined output and parse it. Suppress progress noise on stderr.
LINT_OUTPUT=$(golangci-lint run ./... 2>/dev/null || true)

# Extract issue count from the "N issues:" summary line.
# Falls back to 0 when no summary line is present (means 0 issues).
CURRENT_COUNT=0
if command -v python3 >/dev/null 2>&1; then
  CURRENT_COUNT=$(python3 -c "
import sys, re
text = '''$LINT_OUTPUT'''
m = re.search(r'^(\d+) issues?:', text, re.MULTILINE)
print(m.group(1) if m else '0')
" 2>/dev/null || echo 0)
elif command -v python >/dev/null 2>&1; then
  CURRENT_COUNT=$(python -c "
import sys, re
text = '''$LINT_OUTPUT'''
m = re.search(r'^(\d+) issues?:', text, re.MULTILINE)
print(m.group(1) if m else '0')
" 2>/dev/null || echo 0)
fi

echo "[lint-gate] golangci-lint: $CURRENT_COUNT issue(s) found"

# ── 3. Load baseline ─────────────────────────────────────────────────────────
BASELINE_FILE=".gg/lint-baseline.json"
if [ ! -f "$BASELINE_FILE" ]; then
  echo "[lint-gate] no baseline at $BASELINE_FILE — run 'gg doctor --capture-lint-baseline' to initialize"
  echo "[lint-gate] ✓ skipping gate (no baseline)"
  exit 0
fi

# Extract baseline count using python3/python (matches file-size-gate pattern).
BASELINE_COUNT=""
if command -v python3 >/dev/null 2>&1; then
  BASELINE_COUNT=$(python3 -c "
import json
try:
  d = json.load(open('$BASELINE_FILE'))
  print(d.get('issue_count', ''))
except Exception:
  pass
" 2>/dev/null || true)
elif command -v python >/dev/null 2>&1; then
  BASELINE_COUNT=$(python -c "
import json
try:
  d = json.load(open('$BASELINE_FILE'))
  print(d.get('issue_count', ''))
except Exception:
  pass
" 2>/dev/null || true)
fi

if [ -z "$BASELINE_COUNT" ]; then
  echo "[lint-gate] could not read baseline count from $BASELINE_FILE — skipping gate" >&2
  exit 0
fi

echo "[lint-gate] baseline: $BASELINE_COUNT issue(s)"

# ── 4. Compare and auto-shrink ───────────────────────────────────────────────
if [ "$CURRENT_COUNT" -lt "$BASELINE_COUNT" ] 2>/dev/null; then
  # Debt reduced — shrink the baseline automatically so the gate tightens.
  NEW_BASELINE=$(printf '{\n  "version": 1,\n  "captured_at": "%s",\n  "issue_count": %d\n}\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)" \
    "$CURRENT_COUNT")
  printf '%s' "$NEW_BASELINE" > "$BASELINE_FILE" 2>/dev/null || true
  SHRUNK=$((BASELINE_COUNT - CURRENT_COUNT))
  printf '[lint-gate] ✓ lint debt reduced by %d (current=%d → baseline updated to %d)\n' \
    "$SHRUNK" "$CURRENT_COUNT" "$CURRENT_COUNT"
  exit 0
fi

if [ "$CURRENT_COUNT" -eq "$BASELINE_COUNT" ] 2>/dev/null; then
  echo "[lint-gate] ✓ no new lint regressions (current=$CURRENT_COUNT baseline=$BASELINE_COUNT)"
  exit 0
fi

DELTA=$((CURRENT_COUNT - BASELINE_COUNT))
printf '[lint-gate] ✗ %d NEW lint issue(s) introduced (current=%d baseline=%d)\n' \
  "$DELTA" "$CURRENT_COUNT" "$BASELINE_COUNT" >&2

printf '\nFull lint output:\n' >&2
printf '%s\n' "$LINT_OUTPUT" >&2

printf '\nTo fix:\n' >&2
printf '  1. Resolve the new lint issues (golangci-lint run ./... to see all)\n' >&2
printf '  2. Verify count is back to <= %d\n' "$BASELINE_COUNT" >&2
printf '\nTo update the baseline when debt is intentionally accepted:\n' >&2
printf '  gg doctor --capture-lint-baseline\n' >&2
printf '\nTo bypass (audited):\n' >&2
printf '  GG_ALLOW_LINT_REGRESSIONS="<reason>" gg task done %s ...\n' "${GG_TASK_ID:-TASK-NNN}" >&2

# ── 5. Bypass ─────────────────────────────────────────────────────────────────
if [ -n "$GG_ALLOW_LINT_REGRESSIONS" ]; then
  if command -v gg >/dev/null 2>&1; then
    gg record "lint-gate bypassed for ${GG_TASK_ID:-unknown}" \
      --reason "GG_ALLOW_LINT_REGRESSIONS=${GG_ALLOW_LINT_REGRESSIONS}; current=${CURRENT_COUNT} baseline=${BASELINE_COUNT}" \
      --tags "bypass,lint-gate,${GG_TASK_ID:-unknown}" 2>/dev/null || true
  fi
  printf '[lint-gate] bypass accepted (reason: %s)\n' "$GG_ALLOW_LINT_REGRESSIONS" >&2
  exit 0
fi

if [ "$MODE" = "warn" ]; then
  exit 0
fi

exit 7
