#!/bin/sh
set -eu

file="internal/orchestrator/terminal/cmux.go"

grep -q '"new-split", dir' "$file"
! grep -q '"new-pane"' "$file"
