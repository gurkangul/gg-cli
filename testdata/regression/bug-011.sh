#!/bin/sh
# Repro for BUG-011: Go build-cache paths (../../Library/Caches/go-build) were
# leaking into Memgraph as File nodes. Fix: normalizeProjectPath rejects paths
# that escape the project root via '..' — same test as BUG-010.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run "TestNormalizeProjectPath/escape_via_dotdot" ./cmd/
