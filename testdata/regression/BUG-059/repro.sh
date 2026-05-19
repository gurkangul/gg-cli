#!/bin/sh
set -eu
grep -q 'TestObedience_AllBroadcastsAreNotRoleAcknowledgement' cmd/audit_inbox_obedience_test.go || {
  echo 'BUG-059 repro missing: broadcast obedience regression test not present'
  exit 1
}
go test ./cmd -run 'TestObedience_AllBroadcastsAreNotRoleAcknowledgement|TestObedience_AllBroadcastWithMentionCountsMentionedRole|TestObedience_DuplicateMentionDoesNotDoubleCount' -count=1
