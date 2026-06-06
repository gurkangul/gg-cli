#!/bin/sh
set -eu
# BUG-064: SearchDecisions/SearchBugs returned unfiltered results including
# superseded/rejected decisions and fixed/wontfix bugs.
grep -q 'ActiveDecisionsFilter' internal/store/decisions.go || {
  echo 'BUG-064: ActiveDecisionsFilter missing from decisions.go'
  exit 1
}
grep -q 'ActiveBugsFilter' internal/store/bugs_query.go || {
  echo 'BUG-064: ActiveBugsFilter missing from bugs_query.go'
  exit 1
}
grep -q 'include-superseded' cmd/search.go || {
  echo 'BUG-064: --include-superseded flag missing from search command'
  exit 1
}
go test ./internal/store -run 'TestActiveDecisionsFilter\|TestActiveBugsFilter' -count=1 2>/dev/null || true
go test ./internal/store ./cmd -count=1 -timeout=90s
