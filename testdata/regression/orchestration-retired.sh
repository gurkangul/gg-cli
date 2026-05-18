#!/usr/bin/env bash
set -euo pipefail
# Regression guard for fixed spawn/cmux/orchestration bugs whose original
# surface was intentionally removed by TASK-413. Reintroducing cmd/spawn*.go or
# internal/orchestrator would make the old stale repro class meaningful again.

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

python3 - <<'PY'
from pathlib import Path
bad = [str(p) for p in Path('cmd').glob('spawn*.go')]
if Path('internal/orchestrator').exists():
    bad.append('internal/orchestrator')
if bad:
    raise SystemExit('removed orchestration surface present: ' + ', '.join(bad))
PY

go test ./internal/agenthooks -run 'TestRemoveObsoleteBlocks_StripsRemovedOrchestrationBlocks|TestCheckContract_DriftedLegacyVersion|TestFixContract_ForceResetOnDrifted' -count=1
