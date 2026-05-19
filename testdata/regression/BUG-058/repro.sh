#!/bin/sh
set -eu
grep -q 'TestBugHydrationGateBlocksTaggedSessionWithoutFullBugGet' cmd/hydration_gate_test.go || {
  echo 'BUG-058 repro missing: bug hydration gate regression test not present'
  exit 1
}
go test ./cmd -run 'TestBugHydrationGateBlocksTaggedSessionWithoutFullBugGet|TestBugHydrationGateAllowsRecentFullBugGet' -count=1
