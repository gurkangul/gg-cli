#!/bin/sh
# BUG-027 repro: gg task get default-format hid Detail field for agents because
# isCompactActive() auto-compacts when GG_AGENT/GG_ROLE env is set. Workers
# spawned via 'gg spawn worker' inherited GG_AGENT and saw only the title.
#
# Broken state: cmd/task_list.go runTaskGet calls isCompactActive(cmd) which
#               returns true for agents → renderTaskGetCompact (one-liner).
# Fixed state:  runTaskGet uses compactExplicit (only true when --compact
#               flag explicitly Changed) → renderTaskGetDefault includes Detail.
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
target="$repo_root/cmd/task_list.go"

if grep -q 'if isCompactActive(cmd) {' "$target" \
  && ! grep -q 'compactExplicit' "$target"; then
  echo "BUG-027 repro: runTaskGet still calls isCompactActive — agents auto-compact and lose Detail" >&2
  exit 1
fi

if ! grep -q 'compactExplicit' "$target"; then
  echo "BUG-027 repro: runTaskGet missing compactExplicit guard — fix not applied" >&2
  exit 1
fi

echo "BUG-027 repro: runTaskGet uses compactExplicit; agent default no longer auto-compacts"
