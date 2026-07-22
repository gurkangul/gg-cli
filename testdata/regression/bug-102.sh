#!/bin/sh
# Repro for BUG-102: the inbox handoff gate queried the message store with
# reader="" hardcoded, so it ignored the per-recipient read_by set that `gg inbox`
# actually writes (BUG-082). An agent that had read and dismissed its role-targeted
# mail was STILL blocked from every state-changing gg command — the gate was
# unsatisfiable by its own documented remedy ("Read or respond..."), leaving only
# an anonymous global dismiss (which buries other agents' mail) or
# GG_ALLOW_INBOX_SKIP. The fix threads the reader identity (identity.Agent(), the
# same key gg inbox writes) into CheckInboxGate -> GetInbox.
#
# At the broken ref: reader-a dismisses its handoff, `gg inbox` says "No unread
# messages.", yet `gg record` still exits non-zero with MISSING DURABLE HANDOFF
# CONTEXT. After the fix: the same read clears the gate, while a DIFFERENT agent
# who never read it is still blocked (the gate is not defanged), and a legacy
# anonymous global dismiss still clears everyone.
#
# `gg init` registers the project in ~/.gg/projects.json, so the trap deregisters
# this exact project_id again — a regression script must not accumulate entries in
# the user's global registry. The gate reads through the real message store, so
# this repro needs a reachable store; Phase 0 asserts the store is genuinely
# serving so a fail-open (store down -> gate never blocks) cannot masquerade as a
# pass.
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
GG="$BINDIR/gg"

cd "$TMP"
git init -q .
"$GG" init --yes >/dev/null 2>&1 || true

# A repro must not inherit the caller's gate-bypass or in-hook context.
unset GG_EMBED_MODEL GG_ALLOW_INBOX_SKIP GG_INSIDE_HOOK GG_TASK_ID 2>/dev/null || true
export GG_ENFORCEMENT=on

# --- seed one role-targeted handoff to `reviewer`
GG_AGENT=sender GG_ROLE=master "$GG" tell reviewer "BUG-102 repro handoff" --from master >/dev/null 2>&1

# --- Phase 0: the store must actually be serving. If the seeded message is not
#     visible, the gate would fail OPEN and every later assertion would pass for
#     the wrong reason — fail loudly instead.
if ! GG_AGENT=reader-a GG_ROLE=reviewer "$GG" inbox --role reviewer --peek 2>&1 | grep -q "BUG-102 repro handoff"; then
  echo "FAIL: message store not serving the seeded handoff — cannot exercise the gate (would fail-open)"
  GG_AGENT=reader-a GG_ROLE=reviewer "$GG" inbox --role reviewer --peek 2>&1 | head -5
  exit 1
fi

# --- Phase 1a: before reading, the gate must block reader-a's write. Both the
#     pre-fix and post-fix gate block here (an unread handoff), so this asserts
#     only that a block happened — it is NOT the bug and must not be the thing the
#     broken-ref check trips on.
set +e
OUT1=$(GG_AGENT=reader-a GG_ROLE=reviewer "$GG" record "probe before read" 2>&1)
RC1=$?
set -e
if [ "$RC1" -eq 0 ]; then
  echo "FAIL: gate did not block an unread role-targeted handoff (Phase 1a)"
  exit 1
fi

# --- Phase 1b: reader-a reads/dismisses, which records read_by=[reader-a].
GG_AGENT=reader-a GG_ROLE=reviewer "$GG" inbox --role reviewer --dismiss-all >/dev/null 2>&1

# --- Phase 1c: THE FIX and the heart of BUG-102 — reader-a's read now clears the
#     gate. This is where the broken-ref check lands: pre-fix the gate ignored
#     read_by and reader-a stayed blocked (RC2 != 0); post-fix RC2 == 0.
set +e
OUT2=$(GG_AGENT=reader-a GG_ROLE=reviewer "$GG" record "probe after read" 2>&1)
RC2=$?
set -e
if [ "$RC2" -ne 0 ]; then
  echo "FAIL: reader-a read its mail but the gate still blocks (BUG-102 — the gate ignores read_by)"
  printf '%s\n' "$OUT2" | tail -4
  exit 1
fi

# --- Phase 2: negative path — a DIFFERENT identity that never read it is still
#     blocked. Proves the fix did not defang the gate into always-pass.
GG_AGENT=sender GG_ROLE=master "$GG" tell reviewer "second handoff" --from master >/dev/null 2>&1
set +e
OUT3=$(GG_AGENT=reader-b GG_ROLE=reviewer "$GG" record "probe as second agent" 2>&1)
RC3=$?
set -e
if [ "$RC3" -eq 0 ]; then
  echo "FAIL: gate defanged — an agent that never read the handoff was not blocked (Phase 2)"
  exit 1
fi
case "$OUT3" in
  *"unread by reader-b"*) ;;
  *) echo "FAIL: block message does not name reader-b (Phase 2)"; exit 1;;
esac

# --- Phase 3: backward compatibility — an anonymous global dismiss (no identity)
#     sets the legacy read=true flag and clears the gate for everyone, including a
#     third identity that never read anything.
env -u GG_AGENT GG_ROLE=reviewer "$GG" inbox --role reviewer --dismiss-all >/dev/null 2>&1
set +e
OUT4=$(GG_AGENT=reader-c GG_ROLE=reviewer "$GG" record "probe after global dismiss" 2>&1)
RC4=$?
set -e
if [ "$RC4" -ne 0 ]; then
  echo "FAIL: legacy anonymous global dismiss no longer clears the gate (Phase 3 — backward compat broken)"
  printf '%s\n' "$OUT4" | tail -4
  exit 1
fi

# --- Phase 4: audience-agents handoffs. The gate scans all audiences
#     (GetInbox humanOnly=false), so a role-targeted `--audience agents` handoff
#     must block — AND the remedy the block advertises must be able to clear it.
#     The default `gg inbox --role R` hides agents-audience mail, so the block
#     text must advertise --include-agents and that read must clear the gate.
GG_AGENT=sender GG_ROLE=master "$GG" tell reviewer "agents-audience handoff" --from master --audience agents >/dev/null 2>&1
set +e
OUT5=$(GG_AGENT=reader-d GG_ROLE=reviewer "$GG" record "probe agents-audience" 2>&1)
RC5=$?
set -e
if [ "$RC5" -eq 0 ]; then
  echo "FAIL: role-targeted agents-audience handoff did not block (Phase 4)"
  exit 1
fi
# The block must advertise --include-agents, or the advertised remedy cannot
# clear an agents-audience blocker (the exact unsatisfiable-gate pathology).
case "$OUT5" in
  *"--include-agents"*) ;;
  *) echo "FAIL: block message does not advertise --include-agents for an agents-audience blocker (Phase 4)"; printf '%s\n' "$OUT5" | grep -i "Run:" | head -1; exit 1;;
esac
# And that advertised read must actually clear the gate.
GG_AGENT=reader-d GG_ROLE=reviewer "$GG" inbox --role reviewer --include-agents >/dev/null 2>&1
set +e
OUT6=$(GG_AGENT=reader-d GG_ROLE=reviewer "$GG" record "probe after include-agents read" 2>&1)
RC6=$?
set -e
if [ "$RC6" -ne 0 ]; then
  echo "FAIL: the advertised '--include-agents' remedy did not clear an agents-audience block (Phase 4)"
  printf '%s\n' "$OUT6" | tail -4
  exit 1
fi

echo "PASS: BUG-102 — the inbox gate honours the per-recipient read_by model (reading your mail clears it; others still block; global dismiss still works; agents-audience blockers are satisfiable via the advertised remedy)"
