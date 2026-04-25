#!/bin/sh
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
template="$repo_root/internal/templates/pre-task-done-go.sh"

if ! grep -q "go test -json ./..." "$template"; then
  echo "BUG-024 repro: pre-task-done Go hook lacks go test -json diagnostic fallback" >&2
  exit 1
fi

if ! grep -q "collecting JSON failure summary" "$template"; then
  echo "BUG-024 repro: hook failure output can still end with opaque bare FAIL" >&2
  exit 1
fi

if ! grep -q "\\[verify-json\\]" "$template"; then
  echo "BUG-024 repro: hook does not prefix diagnostic failure lines for verify-gate tail" >&2
  exit 1
fi

echo "BUG-024 repro: hook diagnostic fallback present"
