#!/bin/sh
# Repro for BUG-098: task lifecycle transitions written through
# UpdateTaskStatus recorded no actor, so the terminal "done" event — the one
# verifier_separation exists to police — landed in task-events.jsonl with an
# empty actor while claim and ready_for_live events carried theirs. The audit
# trail could say a task was closed but not who closed it.
#
# At the broken ref the done event's actor is empty.
#
# init registers the project in ~/.gg/projects.json, so the trap deregisters this
# exact project_id again — a regression script must not accumulate entries in the
# user's global registry.
set -e
cd "$(git rev-parse --show-toplevel)"

BINDIR=$(mktemp -d)
TMP=$(mktemp -d)

deregister() {
  pid=$(sed -n 's/^project_id: *//p' "$TMP/.gg/config.yaml" 2>/dev/null | head -1)
  [ -n "$pid" ] && python3 - "$pid" <<'PY' 2>/dev/null || true
import json, os, shutil, sys
pid = sys.argv[1]
path = os.path.expanduser("~/.gg/projects.json")
try:
    reg = json.load(open(path))
except Exception:
    sys.exit(0)
if reg.get("projects", {}).pop(pid, None) is not None:
    json.dump(reg, open(path, "w"), indent=2)
shutil.rmtree(os.path.expanduser(f"~/.gg/projects/{pid}"), ignore_errors=True)
PY
  rm -rf "$TMP" "$BINDIR"
}
trap deregister EXIT

go build -o "$BINDIR/gg" ./cmd/gg
cd "$TMP"
git init -q .
"$BINDIR/gg" init --yes >/dev/null 2>&1 || true

"$BINDIR/gg" task create 'Verify the done event records its actor' \
  --requester user --from tester >/dev/null 2>&1
TID=$("$BINDIR/gg" task list --compact 2>/dev/null | grep -oE 'TASK-[0-9]+' | head -1)
[ -n "$TID" ] || { echo "FAIL: could not create a task"; exit 1; }

# Hydration proof is required before each transition.
"$BINDIR/gg" task get "$TID" --full >/dev/null 2>&1
"$BINDIR/gg" task ready-for-live "$TID" --from implementer --plan "smoke" >/dev/null 2>&1
"$BINDIR/gg" task get "$TID" --full >/dev/null 2>&1
"$BINDIR/gg" task done "$TID" "verified" --verifier reviewer >/dev/null 2>&1

ACTOR=$(python3 - "$TID" <<'PY'
import json, sys
tid = sys.argv[1]
actor = ""
for line in open(".gg/brain/task-events.jsonl"):
    try:
        p = json.loads(line).get("payload") or {}
    except Exception:
        continue
    if p.get("task_id") == tid and p.get("action") == "done":
        actor = p.get("actor") or ""
print(actor)
PY
)

if [ -z "$ACTOR" ]; then
  echo "FAIL: done event for $TID has an empty actor"
  exit 1
fi
echo "PASS: BUG-098 — done event records actor=$ACTOR"
