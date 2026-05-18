#!/bin/sh
set -eu
cd "$(git rev-parse --show-toplevel)"
# BUG-054: offline JSONL fallback must preserve task/bug result kinds instead of decisions.
grep -q 'func taskFromJSONLEntry' cmd/search_jsonl.go
grep -q 'func bugFromJSONLEntry' cmd/search_jsonl.go
grep -q 'tasks = append(tasks, taskFromJSONLEntry(e))' cmd/search.go
grep -q 'bugs = append(bugs, bugFromJSONLEntry(e))' cmd/search.go
grep -q 'TestSearch_OfflineFallback_ScansTasksAndBugs' cmd/fixtures_search_test.go
grep -q 'Tasks      \[\]store.Task' cmd/search.go
grep -q 'Bugs       \[\]store.Bug' cmd/search.go
