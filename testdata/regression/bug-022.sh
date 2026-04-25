#!/bin/sh
# Repro for BUG-022: queue-pool path silently dropped the agent launch and
# only sent a shell startup, leaving the pane as an idle bash. Fixed by
# unifying both spawn paths through bootstrapAgentInPane (commit d9bd712).
# This test asserts the Send order: agentCmd before buildWorkerPrompt.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -count=1 -run TestBootstrapAgentInPane_NoTaskIDSkipsPrompt -timeout=30s ./cmd/
