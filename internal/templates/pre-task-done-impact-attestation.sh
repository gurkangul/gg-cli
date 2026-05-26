#!/bin/sh
# gg pre-task-done hook: impact-attestation gate.
#
# Checks the current staged/unstaged/untracked task diff plus, when the working
# tree is clean, the HEAD commit only if its message references GG_TASK_ID.
# This keeps no-diff/evidence-only closures from being blocked by an unrelated
# historical commit while still enforcing Impact-Reviewed evidence for committed
# task work.
# Gate is mandatory when:
#   (a) the task diff changes >=3 source files (excluding _test.go), OR
#   (b) ANY changed file has >=5 graph dependents (escalation regardless of count)
#
# When there is no task source diff, exits 0 with an explicit skip. When there
# is source diff below threshold, prints a soft advisory to stderr and exits 0.
#
# Modes (env GG_IMPACT_ATTESTATION):
#   on    (default) — block (exit 7) when trailer is missing at threshold
#   warn            — always print advisory, never block
#   off             — skip entirely
#
# Bypass: GG_BYPASS_RATIONALE=<reason> — audited via gg record
#
# Sample trailer format (add to commit body):
#   Impact-Reviewed: cmd/task_status.go — 2 callers, tests green; internal/store/client.go — 0 callers
#
# Env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#   GG_ACTOR         — GG_ROLE or GG_AGENT of the caller

set -e

MODE="${GG_IMPACT_ATTESTATION:-on}"
if [ "$MODE" = "off" ]; then
  exit 0
fi

# ── 1. Read HEAD commit metadata ──────────────────────────────────────────────
COMMIT_MSG=$(git log -1 --pretty=%B 2>/dev/null || true)
if [ -z "$COMMIT_MSG" ]; then
  echo "[impact-attestation] no commits yet — skipping"
  exit 0
fi

# ── 2. Select task diff files and count changed source files ────────────────
CHANGED_FILES=$( (git diff --name-only 2>/dev/null; git diff --cached --name-only 2>/dev/null; git ls-files --others --exclude-standard 2>/dev/null) | sort -u || true)
DIFF_SOURCE="current staged/unstaged/untracked diff"
ALLOW_HEAD_TRAILER=0

count_source_files() {
  SOURCE_COUNT=0
  SOURCE_LIST=""
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    # Only count source files that are not test files.
    case "$f" in
      *_test.go|*.test.*|*.spec.*) continue ;;
      *.go|*.ts|*.js|*.py|*.rs|*.java|*.c|*.cpp|*.h) ;;
      *) continue ;;
    esac
    SOURCE_COUNT=$((SOURCE_COUNT + 1))
    SOURCE_LIST="${SOURCE_LIST}${f}
"
  done << FILESEOF
$1
FILESEOF
}

count_source_files "$CHANGED_FILES"
if [ "$SOURCE_COUNT" = "0" ] && [ -n "${GG_TASK_ID:-}" ]; then
  if printf '%s' "$COMMIT_MSG" | grep -Eqi "(^|[^[:alnum:]_-])${GG_TASK_ID}([^[:alnum:]_-]|$)"; then
    CHANGED_FILES=$(git log -1 --name-only --pretty="" 2>/dev/null || true)
    DIFF_SOURCE="HEAD commit for ${GG_TASK_ID}"
    ALLOW_HEAD_TRAILER=1
    count_source_files "$CHANGED_FILES"
  fi
fi

echo "[impact-attestation] diff source: $DIFF_SOURCE"
echo "[impact-attestation] changed source files: $SOURCE_COUNT"

if [ "$SOURCE_COUNT" = "0" ]; then
  echo "[impact-attestation] no task source diff detected; skipping impact attestation"
  exit 0
fi

# ── 3. Check for impact attestation trailer in commit body ───────────────────
HAS_TRAILER=0
if [ "$ALLOW_HEAD_TRAILER" = "1" ]; then
  if printf '%s' "$COMMIT_MSG" | grep -Eqi "^(Impact-Reviewed:|impact[[:space:]]+[^:]+:|impact:[[:space:]]+)"; then
    HAS_TRAILER=1
  fi
fi

# Early pass: trailer present — soft log and exit 0 regardless of thresholds.
if [ "$HAS_TRAILER" = "1" ]; then
  echo "[impact-attestation] ✓ impact attestation trailer found — gate satisfied"
  exit 0
fi

# ── 4. Check dependent count escalation (AC-3) ───────────────────────────────
# For each changed source file, run gg impact --compact to count dependents.
# If any file has >=5 dependents, escalate to mandatory regardless of file count.
MAX_DEPENDENTS=0
ESCALATION_FILE=""

if command -v gg >/dev/null 2>&1 && [ -n "$SOURCE_LIST" ]; then
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    IMPACT_OUT=$(gg impact --compact "$f" 2>/dev/null || true)
    # Count non-empty, non-comment lines in the impact output.
    DEP_COUNT=0
    if [ -n "$IMPACT_OUT" ]; then
      DEP_COUNT=$(printf '%s\n' "$IMPACT_OUT" | grep -v "^[[:space:]]*$" | grep -v "^#" | wc -l | tr -d ' ' || echo 0)
    fi
    printf '[impact-attestation]   %s: %s dependent(s)\n' "$f" "$DEP_COUNT" >&2
    if [ "$DEP_COUNT" -gt "$MAX_DEPENDENTS" ]; then
      MAX_DEPENDENTS=$DEP_COUNT
      ESCALATION_FILE="$f"
    fi
  done << SRCEOF
$SOURCE_LIST
SRCEOF
fi

# ── 5. Determine if gate is mandatory ────────────────────────────────────────
MANDATORY=0
REASON=""

if [ "$SOURCE_COUNT" -ge 3 ]; then
  MANDATORY=1
  REASON="$SOURCE_COUNT source files changed (threshold: 3)"
fi

if [ "$MAX_DEPENDENTS" -ge 5 ]; then
  MANDATORY=1
  REASON="${ESCALATION_FILE} has ${MAX_DEPENDENTS} dependents (threshold: 5)"
fi

# ── 6. Advisory path (below threshold, no trailer) ───────────────────────────
if [ "$MANDATORY" = "0" ]; then
  printf '[impact-attestation] advisory: Impact-Reviewed: trailer not found (%d source file(s) changed, max %d dependents).\n' \
    "$SOURCE_COUNT" "$MAX_DEPENDENTS" >&2
  printf '  Consider adding: Impact-Reviewed: <file> — <N> callers, tests green\n' >&2
  exit 0
fi

# ── 7. Bypass ─────────────────────────────────────────────────────────────────
if [ -n "$GG_BYPASS_RATIONALE" ]; then
  if command -v gg >/dev/null 2>&1; then
    gg record "impact-attestation gate bypassed for ${GG_TASK_ID:-unknown}" \
      --reason "GG_BYPASS_RATIONALE=${GG_BYPASS_RATIONALE}" \
      --tags "bypass,impact-attestation,${GG_TASK_ID:-unknown}" 2>/dev/null || true
  fi
  printf '[impact-attestation] bypass accepted (reason: %s)\n' "$GG_BYPASS_RATIONALE"
  exit 0
fi

# ── 8. Block — trailer missing at mandatory threshold ─────────────────────────
printf '[impact-attestation] ✗ %s: Impact-Reviewed: trailer missing\n' "${GG_TASK_ID:-TASK}" >&2
printf '  Reason gate is mandatory: %s\n' "$REASON" >&2
printf '\nAdd to your commit body:\n' >&2
printf '  Impact-Reviewed: <file> — <N> callers, tests green\n' >&2
printf '  Impact-Reviewed: <file2> — 0 callers\n' >&2
printf '\nThen amend: git commit --amend (or add the trailer to your next commit body).\n' >&2
printf '\nTo bypass (audited):\n  GG_BYPASS_RATIONALE="<reason>" gg task done %s ...\n' \
  "${GG_TASK_ID:-TASK}" >&2

if [ "$MODE" = "warn" ]; then
  exit 0
fi

exit 7
