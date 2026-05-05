#!/bin/sh
set -eu

# Regression for BUG-044: command tests must not inherit the invoking agent's
# implementation role. This is the same environment the gg protocol uses.
GG_AGENT=codex GG_ROLE=developer go test ./... -count=1 -race -timeout=120s
