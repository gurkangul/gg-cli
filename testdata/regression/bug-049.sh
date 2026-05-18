#!/usr/bin/env bash
set -euo pipefail
# BUG-049: parallel gg config set writes must be serialized by a project-local
# file lock so a stale read/modify/write cannot drop another update.

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

python3 - <<'PY'
from pathlib import Path
cmd = Path('cmd/config.go').read_text(encoding='utf-8')
lock = Path('internal/config/lock.go').read_text(encoding='utf-8')
unix = Path('internal/config/flock_unix.go').read_text(encoding='utf-8')
if 'config.WithWriteLock(func() error {' not in cmd:
    raise SystemExit('runConfigSet does not wrap load/edit/save in config.WithWriteLock')
if 'configWriteLockFile' not in lock or 'withFileLock(f, fn)' not in lock:
    raise SystemExit('config write lock helper missing project-local file lock')
if 'syscall.Flock' not in unix or 'syscall.LOCK_EX' not in unix:
    raise SystemExit('unix config write lock is not an exclusive flock')
PY

go test ./cmd ./internal/config -run 'TestValidate_Backup|TestLoadFromGGDir_AppliesBackupDefaults' -count=1
