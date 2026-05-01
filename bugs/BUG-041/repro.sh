#!/usr/bin/env bash
set -euo pipefail

# BUG-041 regression guard: worker ACK/completion routing must target the
# active master identity, not only the historical claude-code role.
grep -q 'TestSpawnWorker_PromptRoutesCompletionToActiveCodexMaster' cmd/spawn_test.go
grep -q 'masterMessageTargets' cmd/master_targets.go
grep -q 'GG_MASTER_ROLE' cmd/spawn_worker.go
grep -q 'gg tell %s' cmd/spawn_worker.go
grep -q 'sent to %s' cmd/task_ack.go
