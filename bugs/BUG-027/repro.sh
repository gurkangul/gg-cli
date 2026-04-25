#!/bin/sh
# BUG-027 repro: gg task get default-format hid Detail field for agents because
# isCompactActive() auto-compacts when GG_AGENT/GG_ROLE env is set. Workers
# spawned via 'gg spawn worker' inherited GG_AGENT and saw only the title.
#
# Broken state: cmd/task_list.go runTaskGet calls isCompactActive(cmd) which
#               returns true for agents → renderTaskGetCompact (one-liner).
# Fixed state:  runTaskGet gates compact rendering on an explicit flag check
#               (f.Changed for --compact/--short), not env-driven auto-compact.
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
target="$repo_root/cmd/task_list.go"

# Extract the runTaskGet function body (from `func runTaskGet` to the next
# top-level `func ` declaration) so the semantic checks below only run against
# the function the bug actually lives in.
body=$(awk '
  /^func runTaskGet\(/ { inFn=1 }
  inFn { print }
  inFn && /^}$/ { exit }
' "$target")

if printf '%s' "$body" | grep -q 'isCompactActive('; then
  echo "BUG-027 repro: runTaskGet still calls isCompactActive — agents auto-compact and lose Detail" >&2
  exit 1
fi

if ! printf '%s' "$body" | grep -q 'f\.Changed'; then
  echo "BUG-027 repro: runTaskGet missing explicit-flag guard (f.Changed) — fix not applied" >&2
  exit 1
fi

echo "BUG-027 repro: runTaskGet gates compact on explicit flag; agent default no longer auto-compacts"
