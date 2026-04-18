#!/bin/sh
# Repro for BUG-008: JSONL scanner 1 MB line limit caused large records to fail
# silently (truncated or dropped). Fix: scanner buffer raised to 64 MB with a
# clear error on oversize instead of silent failure.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestBrainImport_OversizePayload_ClearError ./internal/store/
