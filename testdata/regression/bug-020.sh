#!/usr/bin/env bash
# Repro for BUG-020: claude installer dry-run must surface the CLAUDE.md
# contract block note alongside hook-change notes.
#
# Works at any SHA of the gg-cli source tree because the Go logic is
# embedded and resolved via the worktree's own go.mod. Exits non-zero
# when the bug is present (Notes lack a "contract block" entry),
# exits 0 when the fix is in place.
set -euo pipefail

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# Seed a stale UserPromptSubmit hook so the installer enters the
# hooks-need-update path (not the all-up-to-date early return).
mkdir -p "$TMP/.claude"
cat > "$TMP/.claude/settings.json" <<'JSON'
{"hooks":{"UserPromptSubmit":[{"matcher":"","hooks":[{"type":"command","command":"gg inbox"}]}]}}
JSON

# Drop a tiny Go program inside the current source tree so `go run`
# resolves the agenthooks package from the tree's go.mod (not from
# $TMP, which is not a Go module).
REPRO_DIR="$(pwd)/.bug020-repro"
mkdir -p "$REPRO_DIR"
cat > "$REPRO_DIR/main.go" <<'GO'
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: repro <project-root>")
		os.Exit(2)
	}
	results := agenthooks.InstallNamed(os.Args[1], []string{"claude"},
		agenthooks.Options{Force: true, DryRun: true})
	if len(results) != 1 {
		fmt.Fprintln(os.Stderr, "expected 1 result")
		os.Exit(2)
	}
	for _, n := range results[0].Notes {
		if strings.Contains(strings.ToLower(n), "contract block") {
			fmt.Println("PASS: contract block note present in claude dry-run")
			os.Exit(0)
		}
	}
	fmt.Fprintln(os.Stderr, "FAIL: contract block note MISSING from claude dry-run (BUG-020)")
	for _, n := range results[0].Notes {
		fmt.Fprintf(os.Stderr, "  note: %s\n", n)
	}
	os.Exit(1)
}
GO

STATUS=0
go run "$REPRO_DIR/main.go" "$TMP" || STATUS=$?
rm -rf "$REPRO_DIR"
exit $STATUS
