#!/usr/bin/env bash
set -euo pipefail

test_file="internal/orchestrator/terminal/probe_surface_bug042_repro_test.go"
cat >"$test_file" <<'GO'
package terminal

import (
	"context"
	"os"
	"testing"
)

func TestBUG042ProbeSurfaceRejectsWrongFocusedSurface(t *testing.T) {
	stub := buildStubCmux(t, `#!/bin/sh
cat <<'JSON'
{
  "focused": {
    "surface_ref": "surface:76",
    "surface_type": "terminal"
  }
}
JSON
exit 0`)

	origPATH := os.Getenv("PATH")
	t.Setenv("PATH", stub+":"+origPATH)

	dead, err := ProbeSurface(context.Background(), "surface:88")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dead {
		t.Fatal("BUG-042: wrong focused surface must be treated as dead")
	}
}
GO
trap 'rm -f "$test_file"' EXIT

go test ./internal/orchestrator/terminal -run TestBUG042ProbeSurfaceRejectsWrongFocusedSurface -count=1
