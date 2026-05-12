#!/bin/sh
# BUG-033: gg init defaults langHint to 'go' for ANY project.
# Pre-fix: detectLangHint() unconditionally returns "go" when no recognized
#           language file is present → bootstrap proposes 'gg index --lang go'
#           on .NET/Java/Rust projects.
# Post-fix: detectLangHint() returns "" on no match; maybeRunIndex skips the
#           prompt; printBootstrapPrompt skips the "Next: run gg index ..." line.
#
# Repro asserts the source-level invariant: detectLangHint must NOT have
# 'return "go"' as the unconditional terminal statement.
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

# Pre-fix: file ends `return "go"` after the for-loop → grep matches (bug present).
# Post-fix: file ends `return ""` → grep fails (exit 1 = fix in place).
! awk '/^func detectLangHint/,/^}/' cmd/init_hints.go | grep -E '^\treturn "go"$'
