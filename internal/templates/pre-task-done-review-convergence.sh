#!/bin/sh
# gg pre-task-done hook: review-convergence gate.
#
# Blocks `gg task done` unless the task commit contains a Review-Convergence:
# attestation line. This makes the implementer perform the same self-review
# matrix before claiming done that a later "review et" pass would run.
#
# Required commit body trailer:
#   Review-Convergence: behavior matrix, negative path, legacy compatibility, stale-string sweep, docs/templates/generated artifacts, live smoke, test evidence
#
# Modes (env GG_REVIEW_CONVERGENCE): on (default) | warn | off
# Bypass: GG_ALLOW_INCOMPLETE_REVIEW=<reason> — audited via gg record.
#
# Env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#   GG_ACTOR         — GG_ROLE or GG_AGENT of the caller

set -e

MODE="${GG_REVIEW_CONVERGENCE:-on}"
if [ "$MODE" = "off" ]; then
  # BUG-079: disabling a gate must be rationalized + durably audited, never a
  # silent env switch. Require a reason and append a searchable brain event so
  # future agents can see enforcement was lowered.
  REASON="${GG_REVIEW_CONVERGENCE_REASON:-$GG_ALLOW_INCOMPLETE_REVIEW}"
  if [ -z "$REASON" ]; then
    echo "[review-convergence] ✗ GG_REVIEW_CONVERGENCE=off requires a rationale — set GG_REVIEW_CONVERGENCE_REASON=\"why\" (durably audited via gg record)" >&2
    exit 7
  fi
  gg record "review-convergence gate disabled (off) for ${GG_TASK_ID:-unknown}" \
    --decision-status rejected \
    --reason "GG_REVIEW_CONVERGENCE=off: ${REASON}" \
    --tags "bypass,review-convergence,enforcement-off,${GG_TASK_ID:-}" >/dev/null 2>&1 || true
  printf '[review-convergence] gate disabled (audited): %s\n' "$REASON" >&2
  exit 0
fi

if [ -z "$GG_TASK_ID" ]; then
  exit 0
fi

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  exit 0
fi

COMMIT=$(git log --grep "$GG_TASK_ID" -1 --format=%H 2>/dev/null || true)
if [ -z "$COMMIT" ]; then
  COMMIT=$(git log -1 --format=%H 2>/dev/null || true)
fi
if [ -z "$COMMIT" ]; then
  exit 0
fi

COMMIT_MSG=$(git log -1 --format=%B "$COMMIT" 2>/dev/null || true)
# BUG-077: a bare "Review-Convergence:" token is cargo-cult-gameable. Require the
# trailer to actually enumerate >=3 of the convergence matrix categories. This is
# advisory, not semantic proof (commit text is inherently gameable), but it raises
# the bar from "any text present" to "names the matrix it claims to have run".
#
# BUG-101: read the FOLDED trailer block, not one physical line. The previous
# `grep '^Review-Convergence:' | head -1` scanned a single line, so continuation
# lines of a wrapped trailer were invisible and repeated keys were truncated —
# a richer multi-line attestation scored FEWER categories than a terse one-liner
# and was rejected with exit 7, inverting the incentive the gate exists to create.
# Fold continuation lines (the RFC-822 model `git interpret-trailers` implements)
# and aggregate repeated keys before counting. This widens what is READ, never
# what passes: the >=3 bar below is unchanged.
FOLDED=$(printf '%s\n' "$COMMIT_MSG" | awk '
  BEGIN { txt = ""; blocks = 0; folded = 0; collecting = 0 }
  {
    line = $0
    if (tolower(line) ~ /^review-convergence:/) {
      sub(/^[^:]*:/, "", line)
      txt = txt " " line
      blocks++
      collecting = 1
      next
    }
    if (collecting) {
      # A blank line or the next "Key: value" trailer ends the folded block.
      if (line ~ /^[[:space:]]*$/) { collecting = 0; next }
      if (line ~ /^[A-Za-z][A-Za-z0-9_-]*:[[:space:]]/) { collecting = 0; next }
      txt = txt " " line
      folded++
    }
  }
  END { printf "%d %d\n%s\n", blocks, folded, txt }
')
TRAILER_BLOCKS=$(printf '%s\n' "$FOLDED" | head -1 | cut -d' ' -f1)
TRAILER_FOLDED=$(printf '%s\n' "$FOLDED" | head -1 | cut -d' ' -f2)
TRAILER=$(printf '%s\n' "$FOLDED" | tail -n +2)

# Self-diagnosing output (BUG-101): when the count comes from a folded block,
# say so. The old warning reported a category count that silently contradicted
# what the author had written, leaving no signal that lines had been dropped.
FOLD_NOTE=""
if [ "${TRAILER_FOLDED:-0}" -gt 0 ] || [ "${TRAILER_BLOCKS:-0}" -gt 1 ]; then
  FOLD_NOTE=" [folded: ${TRAILER_BLOCKS} Review-Convergence line(s) + ${TRAILER_FOLDED} continuation line(s)]"
fi

CATS=0
if [ -n "$(printf '%s' "$TRAILER" | tr -d '[:space:]')" ]; then
  for kw in behavior negative legacy stale docs smoke test; do
    if printf '%s' "$TRAILER" | grep -qi "$kw"; then
      CATS=$((CATS + 1))
    fi
  done
  if [ "$CATS" -ge 3 ]; then
    echo "[review-convergence] ✓ Review-Convergence trailer enumerates ${CATS} matrix categories${FOLD_NOTE}"
    exit 0
  fi
  echo "[review-convergence] ⚠ Review-Convergence trailer names only ${CATS} matrix category(ies); need >=3 (behavior/negative/legacy/stale-string/docs/smoke/tests)${FOLD_NOTE}" >&2
fi

CHANGED_FILES=$(git show --name-only --pretty="" "$COMMIT" 2>/dev/null | sed '/^[[:space:]]*$/d' || true)
CHANGED_COUNT=$(printf '%s\n' "$CHANGED_FILES" | sed '/^[[:space:]]*$/d' | wc -l | tr -d ' ')
if [ "${CHANGED_COUNT:-0}" -eq 0 ] 2>/dev/null; then
  exit 0
fi

# BUG-101: report the ACTUAL cause. The old headline claimed the trailer was
# "missing" even when one was present but thin, which contradicted the ⚠ line
# printed just above it and sent authors hunting for the wrong problem.
if [ "${TRAILER_BLOCKS:-0}" -gt 0 ]; then
  HEADLINE="[review-convergence] ✗ ${GG_TASK_ID}: Review-Convergence trailer is present but too thin — it names ${CATS} matrix category(ies), need >=3 (commit ${COMMIT})"
else
  HEADLINE="[review-convergence] ✗ ${GG_TASK_ID}: Review-Convergence evidence trailer missing from commit ${COMMIT}"
fi

msg="${HEADLINE}

Future reviewers are missing the compact behavior/diff/test evidence summary.
Record the convergence evidence in the commit body:
  1. behavior matrix: default / configured / invalid / edge inputs
  2. negative path: unconfigured, missing env, bad args, store/tool failure
  3. legacy compatibility: old config, old command names, migration path
  4. stale-string sweep: rg old terms across source, tests, docs, templates, repros
  5. docs/templates/generated artifacts: verify generated output and unrelated churn
  6. live smoke: real CLI/app command output inspected, not just unit tests
  7. test evidence: targeted tests + full relevant suite + diff check

Add a trailer naming at least 3 of those categories, for example:
  Review-Convergence: behavior matrix + negative path + legacy compatibility + stale-string sweep + docs/templates + live smoke + tests verified

It may wrap across continuation lines — indented lines and repeated
Review-Convergence: keys are folded together before counting:
  Review-Convergence: behavior matrix (default/invalid inputs verified),
    negative path (missing env, bad args, store failure),
    live smoke (real CLI output inspected)

To bypass intentionally:
  GG_ALLOW_INCOMPLETE_REVIEW=\"<reason>\" gg task done ${GG_TASK_ID} ..."

if [ -n "$GG_ALLOW_INCOMPLETE_REVIEW" ]; then
  if command -v gg >/dev/null 2>&1; then
    gg record "review-convergence gate bypassed for ${GG_TASK_ID}" \
      --reason "GG_ALLOW_INCOMPLETE_REVIEW=${GG_ALLOW_INCOMPLETE_REVIEW}" \
      --tags "bypass,review-convergence,${GG_TASK_ID}" 2>/dev/null || true
  fi
  printf '[review-convergence] bypass accepted (reason: %s)\n' "$GG_ALLOW_INCOMPLETE_REVIEW" >&2
  exit 0
fi

if [ "$MODE" = "warn" ]; then
  printf '%s\n' "$msg" >&2
  exit 0
fi

printf '%s\n' "$msg" >&2
exit 7
