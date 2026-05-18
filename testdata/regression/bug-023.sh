#!/usr/bin/env bash
set -euo pipefail
# BUG-023: version-bumped managed blocks must not coexist with stale prior
# versions; drift must be detected and force reset must leave one clean block.
repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

go test ./internal/agenthooks -run 'TestCheckContract_DriftedLegacyVersion|TestFixContract_ForceResetOnDrifted|TestRemoveObsoleteBlocks_StripsRemovedOrchestrationBlocks' -count=1
