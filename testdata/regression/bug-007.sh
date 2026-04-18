#!/bin/sh
# Repro for BUG-007: graph export used Memgraph internal element IDs which are
# session-scoped and not portable across restarts. Fix: stableDomainKey produces
# deterministic composite IDs from domain properties.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestStableDomainKey ./internal/graph/
