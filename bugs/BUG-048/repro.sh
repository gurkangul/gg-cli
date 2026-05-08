#!/bin/sh
set -eu

prompt_file="cmd/spawn_worker.go"

if grep -Fq 'If it passes, close with: gg task done' "$prompt_file"; then
  echo "BUG-048 repro: reviewer prompt still tells reviewer pane to run gg task done"
  exit 1
fi

if ! grep -Fq 'gg task review %s --approve --notes' "$prompt_file"; then
  echo "BUG-048 repro: reviewer prompt does not route approval through gg task review"
  exit 1
fi

if ! grep -Fq 'Do not run gg task done in the reviewer pane' "$prompt_file"; then
  echo "BUG-048 repro: reviewer prompt lacks explicit long-running close warning"
  exit 1
fi

echo "BUG-048 repro: reviewer approves via review status; master owns long-running close"
