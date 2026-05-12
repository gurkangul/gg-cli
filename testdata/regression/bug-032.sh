#!/bin/sh
# BUG-032: secret-scan pre-commit hook false-positive on gitleaks v8.21+.
# Pre-fix: hook called 'gitleaks detect --staged' (removed in v8.21) → exit 126 (unknown flag),
#           hook interprets as 'secrets found', blocks commit. Repro: grep template, expect old line.
# Post-fix (5933d4d): template uses 'gitleaks git --staged' subcommand.
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

# Pre-fix: invocation line is `"$GITLEAKS_BIN" detect --staged ...` → grep exits 0 (bug present).
# Post-fix: invocation line is `"$GITLEAKS_BIN" git --staged ...` → grep exits 1.
# We anchor on the actual invocation pattern, not bare 'detect --staged' (which appears in comments).
! grep -E '\$GITLEAKS_BIN" detect --staged' internal/templates/pre-commit-secret-scan.sh
