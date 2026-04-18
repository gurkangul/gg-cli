#!/bin/sh
# Repro for BUG-010: Symbol/File node paths were absolute, making brain export
# non-portable across machines. Fix: normalizeProjectPath strips the project root
# and rejects paths that escape it. (Also covers BUG-011 — see below.)
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestNormalizeProjectPath ./cmd/
