#!/bin/sh
set -eu
cd "$(git rev-parse --show-toplevel)"
# BUG-051: update must warn when active PATH binary is not the installed target.
grep -q 'func installedBinaryWarning' cmd/update.go
grep -q 'updateLookPath("gg")' cmd/update.go
grep -q 'PATH resolves gg to' cmd/update.go
grep -q 'TestInstalledBinaryWarningDetectsPATHSkew' cmd/update_test.go
grep -q 'TestUpdateJSON_StdoutIsSingleObject' cmd/update_test.go
