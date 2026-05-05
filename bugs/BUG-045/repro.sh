#!/bin/sh
set -eu

GG_AGENT=codex GG_ROLE=master go run ./cmd/gg spawn queue start --agent gsd >/tmp/gg-bug-045-start.out
GG_AGENT=codex GG_ROLE=master go run ./cmd/gg spawn status >/tmp/gg-bug-045-spawn-status.out
GG_AGENT=codex GG_ROLE=master go run ./cmd/gg spawn queue status >/tmp/gg-bug-045-queue-status.out

grep -q "Complete:" /tmp/gg-bug-045-spawn-status.out
grep -q "complete:" /tmp/gg-bug-045-queue-status.out
! grep -q "Running:" /tmp/gg-bug-045-spawn-status.out
! grep -q "running:" /tmp/gg-bug-045-queue-status.out
