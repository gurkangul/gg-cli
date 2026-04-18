#!/bin/sh
# Repro for BUG-003: scip-python install hint pointed to PyPI (pip install),
# but the package is npm-only. Fix: hint now says "npm install".
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestErrIndexerMissing_KnownHints ./internal/index/runner/
