#!/bin/sh
# gg pre-task-done hook: Go project verify gate (BLOCKING).
# Runs BEFORE `gg task done` writes the new state. Non-zero exit aborts
# with ExitVerifyFailed(7) and the task stays in its current state.
#
# Env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#   GG_ACTOR         — GG_ROLE or GG_AGENT of the caller
#
# Edit freely — this file ships as a starting point, not a contract.

set -e

# Run from the directory containing go.mod (injected by the installer). This
# path is relative to the repo root; "." means go.mod sits at the root.
cd "$(dirname "$0")/../../.."
cd "__GG_SUBDIR__"

echo "[verify] (__GG_SUBDIR__) go build ./..."
go build ./...

echo "[verify] (__GG_SUBDIR__) go vet ./..."
go vet ./...

# When running inside a hook invoked by gg task done (GG_INSIDE_HOOK=1), the
# parent gg process holds live connections. Use -short to skip tests that
# access real project state (git log, Qdrant) and would race the parent.
TEST_FLAGS="-count=1 -timeout 120s"
if [ "${GG_INSIDE_HOOK:-0}" = "1" ]; then
  TEST_FLAGS="$TEST_FLAGS -short"
  echo "[verify] (__GG_SUBDIR__) running in hook context — using -short to skip live-state tests"
fi

echo "[verify] (__GG_SUBDIR__) go test ./..."
TEST_OUT="${TMPDIR:-/tmp}/gg-go-test.$$.out"
TEST_JSON="${TMPDIR:-/tmp}/gg-go-test.$$.json"
cleanup_go_test_logs() {
  rm -f "$TEST_OUT" "$TEST_JSON"
}
trap cleanup_go_test_logs EXIT INT TERM

# shellcheck disable=SC2086
if go test ./... $TEST_FLAGS >"$TEST_OUT" 2>&1; then
  cat "$TEST_OUT"
else
  TEST_STATUS=$?
  cat "$TEST_OUT"
  echo "[verify] ✗ go test failed (exit $TEST_STATUS); collecting JSON failure summary" >&2
  # Run a second diagnostic pass so the verify-gate tail contains the package
  # or test action that failed instead of ending with an opaque bare FAIL.
  # shellcheck disable=SC2086
  go test -json ./... $TEST_FLAGS >"$TEST_JSON" 2>&1 || true
  awk '/"Action":"fail"/ || /"Action":"panic"/ || (/"Action":"output"/ && /FAIL|panic|--- FAIL/) { print "[verify-json] " $0 }' "$TEST_JSON" >&2
  exit "$TEST_STATUS"
fi

echo "[verify] ✓ all checks passed for $GG_TASK_ID"
