#!/bin/sh
# Repro for BUG-012: all Symbol nodes were labeled kind=function regardless of
# actual SCIP SymbolInformation_Kind. Fix: parser now maps each kind correctly.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestScipKindMapping ./internal/index/parser/
