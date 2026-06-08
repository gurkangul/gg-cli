#!/bin/sh
# BUG-087 regression guard: `gg bug run-repros` must dispatch *_test.go repros to
# `go test`, not `sh`. Before the fix, runSingleRepro ran every repro via `sh <path>`,
# so the 19 bugs that register a *_test.go file as their repro failed in ~3ms
# (sh cannot execute a Go file) and blocked all task closure.
#
# This guard proves the fix is present across the version boundary (it fails at the
# pre-fix ref because reproCommand did not exist). The full behavioral lock is
# cmd/bug_repros_test.go:TestReproCommand_GoTestForTestFiles, which runs in the
# normal repro suite.
set -e
f="cmd/bug_repros.go"
if grep -q 'func reproCommand' "$f" && grep -q '_test.go' "$f"; then
	echo "BUG-087 guard OK: $f dispatches *_test.go repros via go test (reproCommand present)"
	exit 0
fi
echo "BUG-087 REGRESSION: $f no longer dispatches *_test.go repros to 'go test'"
exit 1
