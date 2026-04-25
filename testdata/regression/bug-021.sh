#!/bin/sh
# Repro for BUG-021: cmux adapter contract violation. Original Send/SendKey/Close
# methods passed surface-id positional, but cmux CLI rejects --flag=value form.
# Fixed in b4fbf18 (TASK-289) for single-worker path; queue-pool variant fixed
# in d9bd712 (BUG-022). Both paths now route through bootstrapAgentInPane and
# use --surface as a separated token.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -count=1 -run TestBootstrapAgentInPane_LaunchesAgentBeforePrompt -timeout=30s ./cmd/
