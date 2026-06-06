#!/bin/sh
set -eu
# BUG-065: pre-task-done gate fails open when a hook script is non-executable.
# runner.go must fail closed (return error) in strict mode, not silently skip.
grep -q 'is not executable' internal/hooks/runner.go || {
  echo 'BUG-065: runner.go missing fail-closed path for non-executable hooks in strict mode'
  exit 1
}
go test ./internal/hooks -run 'TestRunHooks_NonExecutableStrict_FailsClosed' -count=1
