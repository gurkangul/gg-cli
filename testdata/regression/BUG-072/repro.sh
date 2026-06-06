#!/bin/sh
set -eu
# BUG-072: runInboxGatePreflight must be called in runTaskDone
# before the state transition so unread @mentions block closure.
grep -q 'runInboxGatePreflight.*task-done' cmd/task_status.go || {
  echo 'BUG-072: runInboxGatePreflight("task-done") missing from runTaskDone'
  exit 1
}
go build ./...
