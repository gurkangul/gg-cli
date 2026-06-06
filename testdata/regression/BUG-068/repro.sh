#!/bin/sh
set -eu
# BUG-068: gg record --rejects must mark the superseded decision as "superseded"
# in the store, not only write a Memgraph REJECTS edge.
grep -q 'UpdateDecisionStatus.*superseded' cmd/record.go || {
  echo 'BUG-068: record.go missing UpdateDecisionStatus call on --rejects'
  exit 1
}
go build ./cmd/...
