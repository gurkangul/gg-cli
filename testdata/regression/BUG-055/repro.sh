#!/bin/sh
set -eu
cd "$(git rev-parse --show-toplevel)"
# BUG-055: changed-file and codegraph freshness logic must account for dirty/untracked source.
grep -q 'WorkingTreeFingerprint' internal/index/changed/changed.go
grep -q 'git ls-files' internal/index/changed/changed.go
grep -q 'untracked source' internal/index/changed/changed.go
grep -q 'WorkingTreeFingerprint' cmd/index_status.go
grep -q 'TestFiles_WithChanges_ReturnsChanged' internal/index/changed/changed_test.go
grep -q 'TestCollectCodeGraphStatus_LanguageFingerprintDoesNotHideOtherDirtySources' cmd/index_status_test.go
