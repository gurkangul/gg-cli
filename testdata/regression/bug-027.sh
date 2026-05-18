#!/usr/bin/env bash
set -euo pipefail
# BUG-027: default gg task get must render Detail; compact one-liner only when
# compact is explicit.
repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

go test ./cmd -run 'TestBUG027_(TaskGetDefault_ShowsDetail|TaskGetCompact_OneLiner)$' -count=1
