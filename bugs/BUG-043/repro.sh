#!/usr/bin/env bash
set -euo pipefail

test_file="internal/orchestrator/terminal/bug043_repro_test.go"
cat >"$test_file" <<'GO'
package terminal

import (
	"context"
	"testing"
)

func TestBUG043CmuxWorkerPaneIsIndependent(t *testing.T) {
	r := newCapture("OK surface:43 pane:9 workspace:1\n")
	c := newCmuxWithRunner(r.run)

	if _, err := c.NewSplit(context.Background(), SplitOpts{Dir: SplitVertical}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) == 0 {
		t.Fatal("no cmux call recorded")
	}
	if got := r.calls[0][0]; got != "new-pane" {
		t.Fatalf("worker launch must create an independent cmux pane, got %q", got)
	}
}

func TestBUG043ReadScreenRejectsMissingSurface(t *testing.T) {
	r := newCapture("surface:2  type=terminal in_window=false\n")
	c := newCmuxWithRunner(r.run)

	_, err := c.ReadScreen(context.Background(), "surface:1")
	if !IsErrSurfaceNotFound(err) {
		t.Fatalf("missing surface must not fall back to focused surface, got %v", err)
	}
}
GO
trap 'rm -f "$test_file"' EXIT

go test ./internal/orchestrator/terminal -run 'TestBUG043' -count=1
