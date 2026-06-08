#!/bin/sh
# BUG-089 regression guard: module/hook discovery must skip .claude. Nested git
# worktrees under .claude/worktrees carry go.mod and were mis-detected as project
# submodules, installing stale per-worktree hooks that regenerated after every
# delete. .claude must stay in DefaultHookInstallSkipDirs.
set -e
if grep -q '".gsd", ".claude"' internal/config/config.go; then
  echo "BUG-089 guard OK: .claude is in DefaultHookInstallSkipDirs"
  exit 0
fi
echo "BUG-089 REGRESSION: .claude not in DefaultHookInstallSkipDirs"
exit 1
