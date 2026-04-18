#!/bin/sh
# Repro for BUG-014: Package nodes were never populated (LabelPackage existed
# but 0 rows). Fix: graphHandler.OnFile now creates Package nodes and
# CONTAINS edges for each indexed file.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestDerivePackagePath ./cmd/
