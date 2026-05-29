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
if printf '%s\n' "$COMMIT_MSG" | grep -Eiq '^Review-Convergence:[[:space:]]*.+'; then
  echo "[review-convergence] ✓ Review-Convergence trailer found"
  exit 0
fi

CHANGED_FILES=$(git show --name-only --pretty="" "$COMMIT" 2>/dev/null | sed '/^[[:space:]]*$/d' || true)
CHANGED_COUNT=$(printf '%s\n' "$CHANGED_FILES" | sed '/^[[:space:]]*$/d' | wc -l | tr -d ' ')
if [ "${CHANGED_COUNT:-0}" -eq 0 ] 2>/dev/null; then
  exit 0
fi

msg="[review-convergence] ✗ ${GG_TASK_ID}: Review-Convergence evidence trailer missing from commit ${COMMIT}

Future reviewers are missing the compact behavior/diff/test evidence summary.
Record the convergence evidence in the commit body:
  1. behavior matrix: default / configured / invalid / edge inputs
  2. negative path: unconfigured, missing env, bad args, store/tool failure
  3. legacy compatibility: old config, old command names, migration path
  4. stale-string sweep: rg old terms across source, tests, docs, templates, repros
  5. docs/templates/generated artifacts: verify generated output and unrelated churn
  6. live smoke: real CLI/app command output inspected, not just unit tests
  7. test evidence: targeted tests + full relevant suite + diff check

Add a one-line trailer, for example:
  Review-Convergence: behavior matrix + negative path + legacy compatibility + stale-string sweep + docs/templates + live smoke + tests verified

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
