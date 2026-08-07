#!/bin/sh
# Repro for BUG-109: a retired managed hook command stayed wired in
# .claude/settings.json and failed on every tool call its matcher covered.
#
# gg once installed a PreToolUse hook calling `gg dev-role-guard` with matcher
# "Bash". The command was later dropped from the CLI, but removing the writer
# only stops NEW installs — the entry stays in every settings.json written
# before the retirement. The result is a hook pointing at a command that does
# not exist: it exits 1 on every single Bash tool call, and because Claude Code
# treats PreToolUse exit 1 as a non-blocking error, the failure is silent. The
# project believes it has a role guard; nothing is running.
#
# The fix (RemoveObsoleteHooks) retires stale hook commands the same way
# RemoveObsoleteBlocks retires stale markdown blocks, and runs from the same
# two call sites — session-start sync and `doctor --check-contract --fix` — so
# existing projects heal themselves.
#
# This repro asserts the surrounding behaviour, not just the deletion: a
# matcher entry that ONLY ran the retired command is pruned whole, a matcher
# entry that also ran something else keeps the sibling, and unrelated hook
# events are left untouched. A cleanup that removes the stale entry by
# flattening the rest of the file would be worse than the bug.
set -eu
cd "$(git rev-parse --show-toplevel)"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

go build -o "$TMP/gg" ./cmd/gg || {
  echo "BUG-109: could not build gg" >&2; exit 1
}

PROJ="$TMP/proj"
mkdir -p "$PROJ/.gg" "$PROJ/.claude"
cat > "$PROJ/.gg/config.yaml" <<'YAML'
schema_version: 1
project_id: 11111111-2222-3333-4444-555555555555
YAML

# A settings.json shaped like one an older gg wrote: the retired command alone
# under "Bash", the retired command beside a keeper under "Edit", a live guard
# that must survive, and an unrelated event.
cat > "$PROJ/.claude/settings.json" <<'JSON'
{
  "env": { "GG_AGENT": "claude-code" },
  "hooks": {
    "PreToolUse": [
      { "matcher": "gsd_plan_milestone", "hooks": [ { "type": "command", "command": "gg gsd-guard" } ] },
      { "matcher": "Bash", "hooks": [ { "type": "command", "command": "gg dev-role-guard" } ] },
      { "matcher": "Edit", "hooks": [
          { "type": "command", "command": "gg dev-role-guard" },
          { "type": "command", "command": "echo keep-me" } ] }
    ],
    "SessionStart": [ { "matcher": "startup", "hooks": [ { "type": "command", "command": "gg session-start" } ] } ]
  }
}
JSON

# The root cause, stated as an assertion: nothing in the CLI answers to the
# command the hook calls. If a future change reintroduces `dev-role-guard` as a
# real command, this repro should be revisited rather than silently passing.
if "$TMP/gg" dev-role-guard </dev/null >/dev/null 2>&1; then
  echo "BUG-109: 'gg dev-role-guard' now exists — the premise of this repro changed; revisit obsoleteHooks" >&2
  exit 1
fi

( cd "$PROJ" && "$TMP/gg" doctor --check-contract --fix ) > "$TMP/out.txt" 2>&1 || {
  echo "BUG-109: doctor --check-contract --fix failed" >&2; cat "$TMP/out.txt" >&2; exit 1
}

python3 - "$PROJ/.claude/settings.json" <<'PY' || exit 1
import json, sys

d = json.load(open(sys.argv[1]))
pre = d["hooks"].get("PreToolUse", [])
cmds = [h["command"] for e in pre for h in e["hooks"]]
fail = []

if any("dev-role-guard" in c for c in cmds):
    fail.append("retired 'gg dev-role-guard' still wired — it exits 1 on every Bash tool call")
if "gg gsd-guard" not in cmds:
    fail.append("live 'gg gsd-guard' hook was removed — cleanup is too broad")
if any(e.get("matcher") == "Bash" for e in pre):
    fail.append("matcher entry that only ran the retired command was left behind as an empty shell")
edit = [e for e in pre if e.get("matcher") == "Edit"]
if not edit or [h["command"] for h in edit[0]["hooks"]] != ["echo keep-me"]:
    fail.append("sibling hook under the 'Edit' matcher did not survive the removal")
if d["hooks"].get("SessionStart", [{}])[0].get("hooks", [{}])[0].get("command") != "gg session-start":
    fail.append("an unrelated hook event was modified")
if d.get("env", {}).get("GG_AGENT") != "claude-code":
    fail.append("settings env block was not preserved")

if fail:
    for f in fail:
        print("BUG-109: " + f, file=sys.stderr)
    sys.exit(1)
PY

# Idempotency: a second pass must find nothing left to retire.
( cd "$PROJ" && "$TMP/gg" doctor --check-contract --fix ) > "$TMP/out2.txt" 2>&1 || true
if grep -q "removed obsolete dev-role-guard" "$TMP/out2.txt"; then
  echo "BUG-109: cleanup is not idempotent — it reported removing the same hook twice" >&2
  exit 1
fi

# The session-start path, separately. This is the one that actually matters for
# already-installed projects: nobody runs doctor by hand, they just open a
# session. Asserting only the doctor path would let the SyncManagedBlocks wiring
# be deleted with the repro still green.
PROJ2="$TMP/proj2"
mkdir -p "$PROJ2/.gg" "$PROJ2/.claude"
cp "$PROJ/.gg/config.yaml" "$PROJ2/.gg/config.yaml"
cat > "$PROJ2/.claude/settings.json" <<'JSON'
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash", "hooks": [ { "type": "command", "command": "gg dev-role-guard" } ] },
      { "matcher": "MyTool", "hooks": [] }
    ]
  }
}
JSON

( cd "$PROJ2" && "$TMP/gg" session-start --agent claude-code --role master ) > "$TMP/out3.txt" 2>&1 || true

python3 - "$PROJ2/.claude/settings.json" <<'PY' || exit 1
import json, sys

pre = json.load(open(sys.argv[1]))["hooks"].get("PreToolUse", [])
cmds = [h["command"] for e in pre for h in e["hooks"]]
fail = []

if any("dev-role-guard" in c for c in cmds):
    fail.append("session-start did not retire the stale hook — SyncManagedBlocks is not wired, so installed projects never heal on their own")
# An entry that ARRIVED with an empty hooks list is a disabled hook the user
# owns, not our leftover. It must survive a removal elsewhere in the same event.
if not any(e.get("matcher") == "MyTool" for e in pre):
    fail.append("an unrelated matcher entry with an empty hooks list was deleted as collateral")

if fail:
    for f in fail:
        print("BUG-109: " + f, file=sys.stderr)
    sys.exit(1)
PY

echo "BUG-109 repro OK: retired hook commands are pruned from settings.json, siblings and unrelated events survive"
