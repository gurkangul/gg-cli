#!/bin/sh
set -eu
# BUG-066: degraded zero-vector records must be excluded from all Search* results.
# nonDegradedVectorCondition() must exist and be applied in every Search* function.
grep -q 'func nonDegradedVectorCondition' internal/store/decisions.go || {
  echo 'BUG-066: nonDegradedVectorCondition missing from internal/store/decisions.go'
  exit 1
}
grep -c 'nonDegradedVectorCondition' internal/store/decisions.go | grep -q '[2-9]' || \
grep -c 'nonDegradedVectorCondition' internal/store/decisions.go | awk '{if($1>=2)exit 0;else exit 1}' || {
  echo 'BUG-066: nonDegradedVectorCondition not applied in SearchDecisions'
  exit 1
}
grep -q 'nonDegradedVectorCondition' internal/store/bugs_query.go || {
  echo 'BUG-066: nonDegradedVectorCondition not applied in SearchBugs'
  exit 1
}
grep -q 'nonDegradedVectorCondition' internal/store/tasks.go || {
  echo 'BUG-066: nonDegradedVectorCondition not applied in SearchTasks'
  exit 1
}
grep -q 'nonDegradedVectorCondition' internal/store/discussions.go || {
  echo 'BUG-066: nonDegradedVectorCondition not applied in SearchDiscussions'
  exit 1
}
grep -q 'nonDegradedVectorCondition' internal/store/notes.go || {
  echo 'BUG-066: nonDegradedVectorCondition not applied in SearchNotes'
  exit 1
}
grep -q 'nonDegradedVectorCondition' internal/store/rejections.go || {
  echo 'BUG-066: nonDegradedVectorCondition not applied in SearchRejections'
  exit 1
}
go build ./...
