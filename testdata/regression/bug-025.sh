#!/bin/sh
# BUG-025 repro: 50-ac-attestation.sh checked HEAD instead of the task's
# actual commit, causing the hook to block when cosmetic follow-up commits
# shifted HEAD away from the attributed implementation commit.
#
# Broken state: hook used 'git log -1 HEAD' — no task-specific SHA lookup.
# Fixed state:  hook searches last 50 commits via 'git log --grep GG_TASK_ID'
#               and falls back to HEAD only when no match is found.
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
template="$repo_root/internal/templates/pre-task-done-ac-attestation.sh"

if ! grep -q 'git log --pretty=format:"%H %s" -50' "$template"; then
  echo "BUG-025 repro: hook still uses git log -1 HEAD (task-SHA lookup missing)" >&2
  exit 1
fi

if ! grep -q 'grep -iF "\$GG_TASK_ID"' "$template"; then
  echo "BUG-025 repro: hook does not filter by GG_TASK_ID with grep -iF (pattern missing or not fixed-string)" >&2
  exit 1
fi

if ! grep -q 'TASK_SHA="HEAD"' "$template"; then
  echo "BUG-025 repro: hook missing HEAD fallback (fail-open contract violated)" >&2
  exit 1
fi

echo "BUG-025 repro: hook resolves task SHA via git log --grep; HEAD fallback present"
