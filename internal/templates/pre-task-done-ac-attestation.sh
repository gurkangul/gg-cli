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

# ── 3. Parse AC-like anchors from anywhere in Detail ──────────────────────────
# Extracts ALL enumerable criteria from the Detail field regardless of section:
#   (1) Explicit "AC-N:" lines (standalone or prefixed)
#   (2) "Gap A / Gap B / Gap N" lines
#   (3) Numbered-item lines at line start: "1." / "1)" / "1:"
#   (4) "- " bullet lines under ACCEPTANCE/CRITERIA/TESTS/CRITERIA heading
#   (5) "- " bullet lines elsewhere (lower priority, deduplicated)
#
# Items are collected in priority order; duplicates suppressed by text.
# The output is tab-separated: sequential_number\ttext
ACS=$(printf '%s' "$DETAIL" | $PY -c "
import sys, re

text = sys.stdin.read()
lines = text.splitlines()
seen = set()
acs = []

def add(t):
    t = t.strip()
    if t and t not in seen:
        seen.add(t)
        acs.append(t)

# Pass 1: explicit AC-N: anchors anywhere (e.g. 'AC-1: something')
for line in lines:
    m = re.match(r'^\s*AC-(\d+)[:\s]+(.*)', line, re.IGNORECASE)
    if m:
        add(m.group(2).strip() or 'AC-' + m.group(1))

# Pass 2: Gap lines — 'Gap A:', 'Gap B:', 'Gap 1:', '**Gap A**' etc.
for line in lines:
    m = re.match(r'^\s*(?:\*{1,2})?Gap\s+([A-Z0-9]+)(?:\*{1,2})?[:\s]+(.*)', line, re.IGNORECASE)
    if m:
        add(('Gap ' + m.group(1) + ': ' + m.group(2)).strip(': '))

# Pass 3: numbered items at line start — '1. text', '1) text', '1: text'
for line in lines:
    m = re.match(r'^\s*(\d+)[.):\s]\s+(.+)', line)
    if m:
        add(m.group(2).strip())

# Pass 4: '- ' bullets under ACCEPTANCE / CRITERIA / TESTS heading
acc_m = re.search(r'(?im)^ACCEPTANCE(?:\s+(?:CRITERIA|TESTS?))?\s*$', text)
if acc_m:
    after = text[acc_m.end():]
    for line in after.splitlines():
        s = line.strip()
        if not s:
            continue
        if s.startswith('- '):
            add(s[2:].strip())
        elif acs:
            # Non-blank, non-bullet after bullets = new section
            break

# Pass 5: all remaining '- ' bullets not already captured
for line in lines:
    s = line.strip()
    if s.startswith('- '):
        add(s[2:].strip())

for i, ac in enumerate(acs, 1):
    print(str(i) + '\t' + ac)
" 2>/dev/null || true)

if [ -z "$ACS" ]; then
  echo "[ac-attestation] ✓ $GG_TASK_ID: no AC anchors found in Detail — nothing to attest"
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

printf 'To fix — pick any one format per AC:\n' >&2
printf '  (a) AC-N: line in commit body (e.g. "AC-1: implemented via X")\n' >&2
while IFS="	" read -r NUM _; do
  [ -z "$NUM" ] && continue
  printf '       AC-%s: <how this criterion was addressed>\n' "$NUM" >&2
done << ACEOF2
$ACS
ACEOF2
printf '  (b) numbered reference at line start (e.g. "1. addressed via X")\n' >&2
printf '  (c) "AC N" phrase in commit body (e.g. "AC %s is covered by Y")\n' \
  "$(printf '%s' "$ACS" | head -1 | cut -f1)" >&2

printf '\nTo bypass (audited):\n  GG_ALLOW_INCOMPLETE_AC="<reason>" gg task done %s ...\n' \
  "$GG_TASK_ID" >&2

if [ "$MODE" = "warn" ]; then
  exit 0
fi

exit 7
