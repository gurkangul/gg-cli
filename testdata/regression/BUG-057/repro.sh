#!/bin/sh
set -eu
grep -q 'TestBuildAffectingDirtyPathsDetectsSourceChanges' cmd/doctor_binary_test.go || {
  echo 'BUG-057 repro missing: dirty-tree binary freshness regression test not present'
  exit 1
}
go test ./cmd -run 'TestBuildAffectingDirtyPathsDetectsSourceChanges|TestBuildAffectingDirtyPathFilter|TestPorcelainPathUsesRenameDestination' -count=1
