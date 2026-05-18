#!/bin/sh
set -eu

cd "$(git rev-parse --show-toplevel)"

test_file="cmd/bug056_typed_nil_repro_test.go"
cleanup() {
  rm -f "$test_file"
}
trap cleanup EXIT INT TERM

cat >"$test_file" <<'GO'
package cmd

import (
	"io"
	"testing"

	"github.com/gurkangul/gg-cli/internal/projectstate"
)

func TestBug056EnforceTaskHydrationGateNoTypedNil(t *testing.T) {
	setupGGDir(t)
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "reviewer")
	if err := projectstate.RecordHydration(testRuntimeDir(t), "task", "TASK-123"); err != nil {
		t.Fatalf("RecordHydration: %v", err)
	}

	err := enforceTaskHydrationGate(io.Discard, &hookConfig{}, "TASK-123", "task done", "compact-hydration-task-done")
	if err != nil {
		t.Fatalf("expected nil error after valid hydration proof; got non-nil error interface of type %T", err)
	}
}
GO

env -u GG_TELEMETRY -u GG_BYPASS_RATIONALE -u GG_ALLOW_INCOMPLETE_REVIEW \
  GG_ENFORCEMENT=on \
  GOMAXPROCS="${GOMAXPROCS:-1}" \
  GOGC="${GOGC:-50}" \
  GOMEMLIMIT="${GOMEMLIMIT:-1200MiB}" \
  GOCACHE="${GOCACHE:-/tmp/go-build-gg-cli}" \
  go test ./cmd -run TestBug056EnforceTaskHydrationGateNoTypedNil -count=1 -v
