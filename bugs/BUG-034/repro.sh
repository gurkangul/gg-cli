#!/usr/bin/env bash
set -euo pipefail
# BUG-034 regression guard: runTaskDone must invoke closeWorkerPaneForTask
# so 'gg task done' atomically removes the worker pane entry from panes.json.
# Pre-fix (a2f2a04~1): cmd/task_status.go has no such call → bug present.
# Post-fix: call is wired → bug absent.
grep -q 'closeWorkerPaneForTask' cmd/task_status.go
