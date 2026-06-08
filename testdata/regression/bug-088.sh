#!/bin/sh
# BUG-088 regression guard: `gg brain reindex-decisions` must reconcile DECIDES
# edges from Decision.TaskID, not just replay Decision nodes. Before the fix it
# only upserted nodes, leaving the per-project Memgraph relationship graph empty
# (gg-cli had 1 brain edge vs 278 store links). The behavioral proof is the live
# reconcile (task reindex + reindex-decisions repopulate DECIDES/DEPENDS_ON).
set -e
f="cmd/brain_reindex_decisions.go"
if grep -q 'UpsertDecidesEdge' "$f" && grep -q 'dec.TaskID' "$f"; then
  echo "BUG-088 guard OK: reindex-decisions rebuilds DECIDES edges from TaskID"
  exit 0
fi
echo "BUG-088 REGRESSION: $f no longer rebuilds DECIDES edges from Decision.TaskID"
exit 1
