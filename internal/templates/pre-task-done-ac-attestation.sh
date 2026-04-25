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
#   (d) Diff content (git log -1 -p) or test file paths (git log -1 --name-only)
#       contain a test name matching TestACN_* or TestGapN_*
#   (e) Diff content contains func/comment with ac<N>_ prefix, "// AC-N", "// Gap N"
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
# The output is tab-separated: sequential_number\ttext\tgap_label
# gap_label is the Gap letter/number (e.g. "A", "B", "1") for Gap-style items,
# or empty for all other item types. Used by diff-scan rules to match TestGapA_* patterns.
ACS=$(printf '%s' "$DETAIL" | $PY -c "
import sys, re

text = sys.stdin.read()
lines = text.splitlines()
seen = set()
seen_gap_labels = set()   # dedup Gap items by label, not full text
acs = []   # list of (text, gap_label)

def add(t, gap_label=''):
    t = t.strip()
    if t and t not in seen:
        seen.add(t)
        acs.append((t, gap_label))

# Pass 1: explicit AC-N: anchors anywhere (e.g. 'AC-1: something')
for line in lines:
    m = re.match(r'^\s*AC-(\d+)[:\s]+(.*)', line, re.IGNORECASE)
    if m:
        add(m.group(2).strip() or 'AC-' + m.group(1))

# Pass 2: Gap lines — 'Gap A:', 'Gap B:', 'Gap 1:', '**Gap A**:' etc.
# Only count Gap lines that are in a recognised AC section (ACCEPTANCE, ACS, GAPS)
# or that use the strict 'Gap N:' colon-terminated form as an isolated definition.
# FIX / REWORK sections are implementation steps, not acceptance criteria — Gap
# items inside them are excluded even in strict colon form.
# Lines like 'TASK-292 Gap 2 GSD did X' that start with identifiers before the
# word Gap are excluded by re.match anchoring.
AC_SECTION_RE = re.compile(
    r'(?im)^(ACCEPTANCE(?:\s+(?:CRITERIA|TESTS?))?|ACS?|GAPS?)\s*$'
)
# FIX / REWORK / IMPLEMENTATION / NOTES sections contain implementation steps
# or supporting context, not acceptance criteria.
SKIP_SECTION_RE = re.compile(
    r'(?im)^(FIX(?:ES)?|REWORK|IMPLEMENTATION(?:\s+(?:HINTS?|GUIDANCE))?|HINTS?|NOTES?|REFERENCES?|REFS?)\s*:?\s*$'
)

def _collect_section_lines(header_re):
    lines_set = set()
    for sec_m in header_re.finditer(text):
        start_line = text[:sec_m.start()].count('\n')
        in_section = False
        for idx in range(start_line + 1, len(lines)):
            s = lines[idx].strip()
            if not s:
                if in_section:
                    break
                continue
            if re.match(r'^[A-Z][A-Z0-9 _-]{2,}:?\s*$', s):
                break
            lines_set.add(idx)
            in_section = True
    return lines_set

# Build set of line indices that fall inside a recognised AC section.
ac_section_lines = _collect_section_lines(AC_SECTION_RE)
# Build set of line indices that fall inside FIX/REWORK (skip) sections.
skip_section_lines = _collect_section_lines(SKIP_SECTION_RE)

for idx, line in enumerate(lines):
    # Skip lines inside FIX/REWORK sections — those are implementation steps.
    if idx in skip_section_lines:
        continue
    # Strict form: 'Gap N:' with a colon — accepted if NOT inside a skip section.
    strict_m = re.match(r'^\s*(?:\*{1,2})?Gap\s+([A-Z0-9]+)(?:\*{1,2})?:\s*(.*)', line, re.IGNORECASE)
    # Loose form: 'Gap N text' without colon — only inside recognised AC sections.
    loose_m = None if strict_m else re.match(
        r'^\s*(?:\*{1,2})?Gap\s+([A-Z0-9]+)(?:\*{1,2})?\s+(.*)', line, re.IGNORECASE
    )
    m = strict_m or (loose_m if idx in ac_section_lines else None)
    if m:
        label = m.group(1).upper()
        if label in seen_gap_labels:
            continue
        seen_gap_labels.add(label)
        add(('Gap ' + m.group(1) + ': ' + m.group(2)).strip(': '), label)

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

# Pass 5: all remaining '- ' bullets not already captured.
# Skip bullets inside FIX/REWORK sections — those are implementation steps.
for idx, line in enumerate(lines):
    if idx in skip_section_lines:
        continue
    s = line.strip()
    if s.startswith('- '):
        add(s[2:].strip())

for i, (ac, gap_label) in enumerate(acs, 1):
    print(str(i) + '\t' + ac + '\t' + gap_label)
" 2>/dev/null || true)

if [ -z "$ACS" ]; then
  echo "[ac-attestation] ✓ $GG_TASK_ID: no AC anchors found in Detail — nothing to attest"
  exit 0
fi

AC_COUNT=$(printf '%s\n' "$ACS" | grep -c '.' || echo 0)
echo "[ac-attestation] $GG_TASK_ID: found $AC_COUNT acceptance criterion/criteria"

# ── 4. Get commit message, changed file paths, and diff content ──────────────
# Find the task's actual commit — the most recent commit whose message mentions
# GG_TASK_ID — rather than always inspecting HEAD. This prevents false failures
# (or false passes) when follow-up cosmetic commits land on the branch after the
# task commit, shifting HEAD away from the attributed work.
# Search up to 50 commits; fall back to HEAD when none match (first commit, or
# task ID not embedded in message — fail-open so infra gaps don't block work).
TASK_SHA=""
if [ -n "$GG_TASK_ID" ]; then
  TASK_SHA=$(git log --pretty=format:"%H %s" -50 2>/dev/null \
    | grep -iF "$GG_TASK_ID" \
    | head -1 \
    | cut -d' ' -f1 || true)
fi
if [ -z "$TASK_SHA" ]; then
  echo "[ac-attestation] ⚠ $GG_TASK_ID: no commit found in last 50 — falling back to HEAD" >&2
  TASK_SHA="HEAD"
fi

COMMIT_MSG=$(git log -1 --pretty=%B "$TASK_SHA" 2>/dev/null || true)
if [ -z "$COMMIT_MSG" ]; then
  echo "[ac-attestation] ✓ $GG_TASK_ID: no commits — skipping"
  exit 0
fi

echo "[ac-attestation] $GG_TASK_ID: examining commit ${TASK_SHA} ($(git log -1 --pretty=%h "$TASK_SHA" 2>/dev/null || echo '?'))"

# Changed file paths — used to detect test file names like ac1_test.go or TestAC1.
# Works on the very first commit (no parent; --name-only with -1 handles root commits).
COMMIT_FILES=$(git log -1 --name-only --pretty="" "$TASK_SHA" 2>/dev/null || true)

# Unified diff content — used to detect added lines with test names and comments.
COMMIT_DIFF=$(git log -1 -p "$TASK_SHA" 2>/dev/null || true)

# ── 5. Check each AC against commit message and diff ──────────────────────────
UNMATCHED=""

while IFS="	" read -r NUM AC_TEXT GAP_LABEL; do
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

  # Rule (d): test name containing AC number — TestACN_* (case-insensitive).
  # Filters out +++ diff headers before matching so file-path headers don't false-positive.
  # Also scans changed file paths from git log --name-only.
  if printf '%s' "$COMMIT_DIFF" | grep -v "^+++" | grep -qiE "^\+.*[Tt]est_?[Aa][Cc]${NUM}[_A-Z0-9]"; then
    echo "[ac-attestation]   AC-${NUM}: ✓ (test name TestAC${NUM}_ in diff)"
    continue
  fi
  if printf '%s' "$COMMIT_FILES" | grep -qiE "[Tt]est_?[Aa][Cc]${NUM}[_A-Z0-9]"; then
    echo "[ac-attestation]   AC-${NUM}: ✓ (test file path TestAC${NUM}_ in changed files)"
    continue
  fi

  # Rule (d-gap): TestGapLABEL_* in diff added lines or file paths (Gap items only).
  if [ -n "$GAP_LABEL" ]; then
    if printf '%s' "$COMMIT_DIFF" | grep -v "^+++" | grep -qiE "^\+.*[Tt]est[Gg]ap${GAP_LABEL}[_A-Z0-9]"; then
      echo "[ac-attestation]   AC-${NUM}: ✓ (test name TestGap${GAP_LABEL}_ in diff)"
      continue
    fi
    if printf '%s' "$COMMIT_FILES" | grep -qiE "[Tt]est[Gg]ap${GAP_LABEL}[_A-Z0-9]"; then
      echo "[ac-attestation]   AC-${NUM}: ✓ (test file path TestGap${GAP_LABEL}_ in changed files)"
      continue
    fi
  fi

  # Rule (e): func/comment reference in added diff lines.
  # Filter out +++ header lines first (grep -v "^+++"), then match on added lines (^\+).
  if printf '%s' "$COMMIT_DIFF" | grep -v "^+++" | grep -qiE "^\+(.*func[[:space:]]+[a-zA-Z]*[Aa][Cc]${NUM}_|.*//[[:space:]]*AC-${NUM}([^0-9]|$)|.*/\*[[:space:]]*AC-${NUM}([^0-9]|$)|.*#[[:space:]]*AC-${NUM}([^0-9]|$))"; then
    echo "[ac-attestation]   AC-${NUM}: ✓ (func/comment AC-${NUM} in diff)"
    continue
  fi

  # Rule (e-gap): // Gap LABEL or # Gap LABEL in added diff lines (Gap items only).
  if [ -n "$GAP_LABEL" ]; then
    if printf '%s' "$COMMIT_DIFF" | grep -v "^+++" | grep -qiE "^\+.*//[[:space:]]*Gap[[:space:]]+${GAP_LABEL}([^A-Z0-9]|$)"; then
      echo "[ac-attestation]   AC-${NUM}: ✓ (// Gap ${GAP_LABEL} comment in diff)"
      continue
    fi
    if printf '%s' "$COMMIT_DIFF" | grep -v "^+++" | grep -qiE "^\+.*#[[:space:]]*Gap[[:space:]]+${GAP_LABEL}([^A-Z0-9]|$)"; then
      echo "[ac-attestation]   AC-${NUM}: ✓ (# Gap ${GAP_LABEL} comment in diff)"
      continue
    fi
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

FIRST_NUM="$(printf '%s' "$ACS" | head -1 | cut -f1)"
printf 'To fix — any one of (a)–(e) satisfies an AC:\n' >&2
printf '  (a) AC-N: line in commit body — one per unmatched AC:\n' >&2
while IFS="	" read -r NUM _; do
  [ -z "$NUM" ] && continue
  printf '       AC-%s: <how this criterion was addressed>\n' "$NUM" >&2
done << ACEOF2
$ACS
ACEOF2
printf '  (b) numbered reference at line start (e.g. "%s. addressed via X")\n' \
  "$FIRST_NUM" >&2
printf '  (c) "AC N" phrase in commit body (e.g. "AC %s is covered by Y")\n' \
  "$FIRST_NUM" >&2
printf '  (d) test name in diff added lines or changed file paths:\n' >&2
printf '       func TestAC%s_YourTest  or  TestAC%s_something_test.go\n' \
  "$FIRST_NUM" "$FIRST_NUM" >&2
printf '  (e) func/comment in diff added lines:\n' >&2
printf '       func ac%s_impl  or  // AC-%s <note>  or  # AC-%s <note>\n' \
  "$FIRST_NUM" "$FIRST_NUM" "$FIRST_NUM" >&2

printf '\nTo bypass (audited):\n  GG_ALLOW_INCOMPLETE_AC="<reason>" gg task done %s ...\n' \
  "$GG_TASK_ID" >&2

if [ "$MODE" = "warn" ]; then
  exit 0
fi

exit 7
