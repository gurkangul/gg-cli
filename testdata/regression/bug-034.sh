#!/usr/bin/env bash
set -euo pipefail
# BUG-034 belonged to removed spawn pane lifecycle orchestration. TASK-413
# retires that surface; the regression guard is now that it stays removed and
# stale managed orchestration blocks are cleaned.
repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"
exec testdata/regression/orchestration-retired.sh
