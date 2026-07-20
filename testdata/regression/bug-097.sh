#!/bin/sh
# Repro for BUG-097: `gg record` silently dropped a decision when stdin was
# /dev/null. isTerminal() tested only ModeCharDevice, and /dev/null IS a
# character device — so a headless agent was misdetected as interactive, the
# dedup prompt was printed, EOF was read as an empty answer, and the "cancel"
# default discarded the record while exiting 0. Piping into stdin took the
# correct non-interactive path, so the same headless context behaved in two
# opposite ways.
#
# At the broken ref the second record is dropped and the count stays at 1.
#
# `gg init` is required, not a hand-written config: the dedup similarity check
# only engages on a fully initialised project, and without it the prompt never
# fires and the test passes vacuously. init registers the project in
# ~/.gg/projects.json, so the trap deregisters this exact project_id again — a
# regression script must not accumulate entries in the user's global registry.
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

"$BINDIR/gg" record 'Adopt PgBouncer transaction pooling for all backend services' \
  --from tester >/dev/null 2>&1
BEFORE=$(wc -l < .gg/brain/decisions.jsonl | tr -d ' ')

# Near-duplicate: trips the dedup prompt. stdin is /dev/null — no human present.
"$BINDIR/gg" record 'Adopt PgBouncer transaction pooling for all backend services and replicas' \
  --from tester </dev/null >/dev/null 2>&1
AFTER=$(wc -l < .gg/brain/decisions.jsonl | tr -d ' ')

if [ "$AFTER" -le "$BEFORE" ]; then
  echo "FAIL: record dropped with stdin=/dev/null ($BEFORE -> $AFTER)"
  exit 1
fi
echo "PASS: BUG-097 — record survives a closed stdin ($BEFORE -> $AFTER)"
