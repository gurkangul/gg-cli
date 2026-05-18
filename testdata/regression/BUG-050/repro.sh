#!/bin/sh
set -eu
cd "$(git rev-parse --show-toplevel)"
# BUG-050: task block and ready-for-live must share compact hydration gate.
grep -q 'func checkTaskHydrationGate' cmd/hydration_gate.go
grep -q 'enforceTaskHydrationGate(cmd.ErrOrStderr(), nil, taskID, "task block", "compact-hydration-task-block")' cmd/task_status.go
grep -q 'enforceTaskHydrationGate(cmd.ErrOrStderr(), nil, taskID, "task ready-for-live", "compact-hydration-task-ready-for-live")' cmd/task_ready_for_live.go
