#!/bin/sh
# Repro for BUG-108: docs/cli kept orphaned pages for deprecated commands.
#
# cobra's GenMarkdownTree skips any command whose Deprecated field is set
# (IsAvailableCommand() reports false), but the page generated BEFORE the
# deprecation stays on disk and is never rewritten again — so its flag
# documentation freezes and drifts silently from the real --help. CI could not
# catch it either: .github/workflows/ci.yml runs docs-gen and then
# `git diff --exit-code docs/cli/`, and a file nobody rewrites produces no diff.
#
# Concretely, at the broken ref docs/cli/gg_decide.md and gg_reject.md still read
# "--from ... (defaults to $GG_ROLE)" while `gg decide --help` reads
# "(defaults to $GG_ROLE, then the agent identity)".
#
# The fix prunes pages that no longer correspond to an emitted command, which is
# what gives the CI drift check something to see.
#
# This repro asserts the invariant rather than the two specific filenames, so it
# keeps working as commands are deprecated or removed later: after a docs-gen
# run, EVERY page in docs/cli must correspond to a command the tool just wrote.
set -eu
cd "$(git rev-parse --show-toplevel)"

TMP=$(mktemp -d)
# Clean up ONLY the file this script plants. An earlier version ran
# `git checkout -- docs/cli` here and silently restored pages the fix had
# legitimately pruned — a repro must never be able to undo the thing it guards.
ORPHAN="docs/cli/gg_bug108_ghost_command.md"
trap 'rm -rf "$TMP"; rm -f "$ORPHAN"' EXIT

# Plant an orphan: a page for a command that does not exist.
printf '# gg bug108-ghost-command\n\nstale page for a command that no longer exists\n' > "$ORPHAN"

go run ./tools/docs-gen > "$TMP/out.txt" 2>&1 || {
  echo "BUG-108: docs-gen failed" >&2; cat "$TMP/out.txt" >&2; exit 1
}

if [ -f "$ORPHAN" ]; then
  echo "BUG-108: docs-gen left an orphaned page on disk ($ORPHAN) — deprecated-command docs freeze and CI cannot see the drift" >&2
  rm -f "$ORPHAN"
  exit 1
fi

# The live help is the source of truth; no generated page may contradict it.
if grep -rn 'author/role recording this (defaults to \$GG_ROLE)' docs/cli/ >/dev/null 2>&1; then
  echo "BUG-108: a docs/cli page still carries the pre-BUG-106 --from text, so it is not being regenerated" >&2
  grep -rn 'author/role recording this (defaults to \$GG_ROLE)' docs/cli/ >&2
  exit 1
fi

# And the run must leave the tree clean, which is what CI actually asserts.
if ! git diff --quiet docs/cli/; then
  echo "BUG-108: docs/cli is not in sync after regeneration" >&2
  git diff --stat docs/cli/ >&2
  exit 1
fi

echo "BUG-108 repro OK: orphaned pages are pruned and docs/cli matches the emitted command set"
