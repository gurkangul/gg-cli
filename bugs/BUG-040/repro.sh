#!/usr/bin/env bash
set -euo pipefail

# BUG-040 regression guard: heartbeat watch must identify a GSD worker that
# keeps replying in prose after a nudge but never runs a tool command.
grep -q 'TestCheckWorkerPanesMarksPaneStalledAfterNudgeWithoutToolActivity' cmd/spawn_heartbeat_test.go
grep -q 'workerStalledAfterNudge' cmd/spawn_heartbeat.go
grep -q 'MarkWorkerStalled' internal/orchestrator/spawn/panes.go
grep -q 'LastNudgeScreenHash' internal/orchestrator/spawn/panes.go
