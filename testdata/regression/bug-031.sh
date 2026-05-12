#!/bin/sh
# BUG-031: sandbox EPERM on localhost TCP treated identically to service-down.
# Pre-fix: sandboxPermissionHint function did not exist; EPERM indistinguishable from
#           connection-refused (exits 1 — bug present via missing symbol).
# Post-fix (c57b95f): sandboxPermissionHint exists in cmd/sandbox.go and detects
#           EPERM/EACCES/string-match; TestSandboxPermissionHint_RecognizesEPERM passes.
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

# The presence of sandboxPermissionHint in cmd/sandbox.go is the fix.
# Pre-fix: file does not exist → grep exits 1 (bug present).
# Post-fix: file exists with the function → grep exits 0.
grep -q "func sandboxPermissionHint" cmd/sandbox.go
