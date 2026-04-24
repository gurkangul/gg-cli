#!/bin/sh
# gg pre-task-done hook: AC attestation gate.
#
# Blocks `gg task done` when the task spec lists acceptance criteria (ACs)
# that are not referenced in the commit message. This catches silent AC
# narrowing — where a worker commits but omits coverage of some ACs.
#
# Detection rules (any one is sufficient per AC):
#   (a) Commit message body contains "AC-N:" where N is the AC number
#   (b) Commit message body contains a numbered reference "N:" or "N)" at line start
#   (c) Commit message body contains "AC N" (with space, case-insensitive)
#
# Modes (via GG_AC_ATTESTATION env var):
#   on    (default) — block (exit 7) when ACs are unaccounted
#   warn            — print warning, exit 0 (task close proceeds)
#   off             — skip entirely
#
# Bypass: GG_ALLOW_INCOMPLETE_AC=<reason> — audited via gg record
#
# Env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#   GG_ACTOR         — GG_ROLE or GG_AGENT of the caller

set -e

MODE="${GG_AC_ATTESTATION:-on}"
if [ "$MODE" = "off" ]; then
  exit 0
fi

if [ -z "$GG_TASK_ID" ]; then
  exit 0
fi

# ── 1. Fetch task JSON ────────────────────────────────────────────────────────
TASK_JSON=$(gg task get "$GG_TASK_ID" --json 2>/dev/null || true)
if [ -z "$TASK_JSON" ]; then
  # Can't fetch task — skip silently (don't block on infra errors)
  exit 0
fi

# ── 2. Extract ACCEPTANCE section from Detail ────────────────────────────────
# The Detail field is a JSON string. We use python3/python to decode it.

if command -v python3 >/dev/null 2>&1; then
  PY=python3
elif command -v python >/dev/null 2>&1; then
  PY=python
else
  # No python — skip (can't parse JSON or ACCEPTANCE block reliably)
  exit 0
fi

DETAIL=$(printf '%s' "$TASK_JSON" | $PY -c "
import json, sys
try:
    d = json.load(sys.stdin)
    print(d.get('Detail', '') or '')
except Exception:
    pass
" 2>/dev/null || true)

if [ -z "$DETAIL" ]; then
  exit 0
fi

# ── 3. Parse ACCEPTANCE bullet points ─────────────────────────────────────────
# Extract lines from the ACCEPTANCE section. Only "- " bullet lines count as
# ACs — this prevents ordinary prose containing "AC" as a substring from
# producing false positives.
ACS=$(printf '%s' "$DETAIL" | $PY -c "
import sys, re

text = sys.stdin.read()

# Locate the ACCEPTANCE block (case-insensitive standalone heading)
m = re.search(r'(?im)^ACCEPTANCE(?:\s+(?:CRITERIA|TESTS?))?\s*$', text)
if not m:
    sys.exit(0)

after = text[m.end():]

acs = []
for line in after.splitlines():
    stripped = line.strip()
    if not stripped:
        continue
    if stripped.startswith('- '):
        acs.append(stripped[2:].strip())
    elif acs:
        # First non-bullet, non-blank line after bullets = new section
        break

for i, ac in enumerate(acs, 1):
    print(str(i) + '\t' + ac)
" 2>/dev/null || true)

if [ -z "$ACS" ]; then
  echo "[ac-attestation] ✓ $GG_TASK_ID: no ACCEPTANCE bullets found — nothing to attest"
  exit 0
fi

AC_COUNT=$(printf '%s\n' "$ACS" | grep -c '.' || echo 0)
echo "[ac-attestation] $GG_TASK_ID: found $AC_COUNT acceptance criterion/criteria"

# ── 4. Get commit message ─────────────────────────────────────────────────────
COMMIT_MSG=$(git log -1 --pretty=%B 2>/dev/null || true)
if [ -z "$COMMIT_MSG" ]; then
  echo "[ac-attestation] ✓ $GG_TASK_ID: no commits — skipping"
  exit 0
fi

# ── 5. Check each AC against commit message ───────────────────────────────────
UNMATCHED=""

while IFS="	" read -r NUM AC_TEXT; do
  [ -z "$NUM" ] && continue

  # Rule (a): explicit "AC-N:" in commit message (case-insensitive)
  if printf '%s' "$COMMIT_MSG" | grep -qi "AC-${NUM}:"; then
    echo "[ac-attestation]   AC-${NUM}: ✓ (AC-${NUM}: in commit)"
    continue
  fi

  # Rule (b): "N:" or "N)" at start of a line in commit body
  if printf '%s' "$COMMIT_MSG" | grep -qE "^[[:space:]]*${NUM}[):.][[:space:]]"; then
    echo "[ac-attestation]   AC-${NUM}: ✓ (numbered line ${NUM}: in commit)"
    continue
  fi

  # Rule (c): "AC N" (with space, case-insensitive, not followed by more digits)
  if printf '%s' "$COMMIT_MSG" | grep -qiE "AC[[:space:]]+${NUM}([^0-9]|$)"; then
    echo "[ac-attestation]   AC-${NUM}: ✓ (AC ${NUM} in commit)"
    continue
  fi

  echo "[ac-attestation]   AC-${NUM}: ✗ not referenced — ${AC_TEXT}"
  UNMATCHED="${UNMATCHED}
  AC-${NUM}: ${AC_TEXT}"
done << ACEOF
$ACS
ACEOF

# ── 6. Pass if all ACs accounted ──────────────────────────────────────────────
if [ -z "$UNMATCHED" ]; then
  echo "[ac-attestation] ✓ all $AC_COUNT ACs accounted for in commit message"
  exit 0
fi

# ── 7. Bypass ─────────────────────────────────────────────────────────────────
if [ -n "$GG_ALLOW_INCOMPLETE_AC" ]; then
  if command -v gg >/dev/null 2>&1; then
    gg record "ac-attestation gate bypassed for $GG_TASK_ID" \
      --reason "GG_ALLOW_INCOMPLETE_AC=${GG_ALLOW_INCOMPLETE_AC}" \
      --tags "bypass,ac-attestation,${GG_TASK_ID}" 2>/dev/null || true
  fi
  printf '[ac-attestation] bypass accepted (reason: %s)\n' "$GG_ALLOW_INCOMPLETE_AC"
  exit 0
fi

# ── 8. Report unmatched ACs ───────────────────────────────────────────────────
printf '[ac-attestation] ⚠ %s: unmatched acceptance criteria:%s\n\n' \
  "$GG_TASK_ID" "$UNMATCHED" >&2

printf 'To fix: add "AC-N: <evidence>" lines to your commit message body:\n' >&2
while IFS="	" read -r NUM _; do
  [ -z "$NUM" ] && continue
  printf '  AC-%s: <how this criterion was addressed>\n' "$NUM" >&2
done << ACEOF2
$ACS
ACEOF2

printf '\nTo bypass (audited):\n  GG_ALLOW_INCOMPLETE_AC="<reason>" gg task done %s ...\n' \
  "$GG_TASK_ID" >&2

if [ "$MODE" = "warn" ]; then
  exit 0
fi

exit 7
