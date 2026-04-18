#!/bin/sh
# Repro for BUG-016: gg index --reconcile did not clear stale outbox entries
# from prior crashed runs; they accumulated indefinitely. Fix: sweepIndexOutbox()
# deletes all prior entries for the same root+lang after a successful index.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestSweepIndexOutbox ./cmd/
