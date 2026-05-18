#!/bin/sh
set -eu

# BUG-044: cmd task-done tests must not inherit the invoking agent's
# implementation role. Keep this targeted so gg bug run-repros stays within the
# per-repro budget while still exercising the original GG_ROLE=developer leak.
GG_AGENT=codex GG_ROLE=developer go test ./cmd -run 'TestTaskDone_(StoreDown|NoPreHookDir_ReachesStore|PreHookPasses_FallsThroughToStore|PreHookFails_EmitsNDJSONEvent)$' -count=1 -race -timeout=120s
