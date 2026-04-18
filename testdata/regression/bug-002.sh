#!/bin/sh
# Repro for BUG-002: UpsertNode silently dropped missing merge keys.
# Fix: returns error when a merge key is absent from node properties.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestUpsertNode_MergeKeyNotInProps_Error ./internal/graph/
