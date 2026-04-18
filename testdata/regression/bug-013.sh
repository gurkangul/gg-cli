#!/bin/sh
# Repro for BUG-013: only DEFINES edges were populated; IMPORTS/CALLS/REFERENCES
# edges were never written because OnReference did nothing. Fix: OnReference
# appends to pendingRefs; flushRefs() resolves and writes IMPORTS edges.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestGraphHandler_OnReference_PopulatesPendingRefs ./cmd/
