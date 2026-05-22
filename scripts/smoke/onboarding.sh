#!/usr/bin/env bash
# scripts/smoke/onboarding.sh — Time-to-productivity smoke test
# Verifies a fresh agent can get project context in 5-6 commands.
#
# Usage: scripts/smoke/onboarding.sh
# Requires: gg binary installed, Docker services running
set -euo pipefail

PASS=0
FAIL=0
TOTAL=0

check() {
    local label="$1"
    shift
    TOTAL=$((TOTAL + 1))
    if "$@" > /dev/null 2>&1; then PASS=$((PASS + 1)); echo "  ✓ $label"; else FAIL=$((FAIL + 1)); echo "  ✗ $label"; fi
}

check_output() {
    local label="$1" pattern="$2"
    shift 2
    TOTAL=$((TOTAL + 1))
    local output; output=$("$@" 2>&1) || true
    if printf '%s\n' "$output" | grep -qiE "$pattern"; then PASS=$((PASS + 1)); echo "  ✓ $label"; else FAIL=$((FAIL + 1)); echo "  ✗ $label (expected: $pattern)"; echo "    got: $(printf '%s\n' "$output" | head -3)"; fi
}

echo "=== Time-to-Productivity Onboarding Smoke ==="
echo ""

export GG_AGENT="smoke-onboarding-$$"
export GG_ROLE="implementer"

echo "Step 1: Session start"
check "gg session-start" gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"

echo "Step 2: Next-step recommendation"
check_output "gg next shows recommendation" "inbox|task|read" gg next --agent "$GG_AGENT" --role "$GG_ROLE"

echo "Step 3: Search for architecture decisions"
check_output "gg search finds decisions" "search|decision|task" gg search "architecture" --compact

echo "Step 4: Get context bundle"
check_output "gg context returns results" "context|decision|task|note" gg context "blockers" --compact

echo "Step 5: Check code impact"
check_output "gg impact shows dependencies" "impact|deps|sym" gg impact cmd/task.go --compact

echo "Step 6: List runnable tasks"
check_output "gg task list works" "TASK|Pending|no.*task|ready" gg task list --ready --compact

echo ""
echo "=== Results: $PASS/$TOTAL passed, $FAIL failed ==="
if [ "$FAIL" -gt 0 ]; then echo "SMOKE FAILED"; exit 1; fi
echo "SMOKE PASSED"
