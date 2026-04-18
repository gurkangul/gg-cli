#!/bin/sh
# Repro for BUG-005: brain export silently dropped data on Qdrant errors (nil,nil return).
# Fix: ExportBrainCollection propagates non-NotFound scroll errors.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestBrainExportCollection_Unavailable_ReturnsError ./internal/store/
