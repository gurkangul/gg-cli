#!/bin/sh
set -eu
# BUG-061: GG_AGENT example can be copied literally by GSD sessions
# The paste block in session-start must not contain a copyable "export GG_AGENT=" line.
if grep -rq 'export GG_AGENT=' internal/session/session.go; then
  echo 'BUG-061: session.go still emits copyable export GG_AGENT= assignment'
  exit 1
fi
go test ./internal/session -run 'TestPasteBlock' -count=1
go test ./internal/agenthooks -run 'TestGSD_BridgeBlock' -count=1
