#!/bin/sh
# Repro for BUG-103: identity.Agent()'s per-tab branch read CLAUDE_SESSION_ID, but
# Claude Code exports CLAUDE_CODE_SESSION_ID, so every tab collapsed to the generic
# "claude-code" id — per-tab leases, per-recipient read-state (BUG-082), and
# verifier separation were all inert across tabs. The fix reads
# CLAUDE_CODE_SESSION_ID first (CLAUDE_SESSION_ID fallback).
#
# Per the recorded 2026-07-21 decision, per-tab identity ships TOGETHER with an
# inbox-gate recency window (GG_INBOX_GATE_WINDOW, default 14d): a fresh per-tab id
# has an empty read_by, so an unbounded gate would re-block every accumulated
# handoff for every new tab — BUG-102 from the other side. The window bounds the
# fresh-tab candidate set to recent activity by wall-clock, independent of
# identity/read_by.
#
# This repro proves both, end to end, against the real CLI:
#   Phase A — per-tab identity ISOLATION: tab A's read does not clear tab B's gate.
#   Phase B1 — window skips STALE handoffs (fresh tab stays satisfiable).
#   Phase B2 — window still blocks a RECENT unread handoff (gate not defanged).
# At the pre-fix ref both tabs collapse to "claude-code" (Phase A fails) and there
# is no window (Phase B1 fails), so the script fails there.
#
# `gg init` registers the project in ~/.gg/projects.json; the trap deregisters this
# exact project_id. The gate reads the real store, so Phase 0 asserts it is
# serving before we trust any "not blocked" result (a fail-open must not pass).
#
# TIMING: Phase B1 uses a tiny 2s window + a 4s sleep (stale margin is generous);
# Phase B2 uses a large 1h window so a slow store can never age the fresh handoff
# out of it. Neither margin is tight.
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

# A repro must not inherit the caller's bypass / in-hook / window context, nor the
# outer Claude session id (we set a per-tab one on every gate-triggering call).
unset GG_EMBED_MODEL GG_ALLOW_INBOX_SKIP GG_INSIDE_HOOK GG_TASK_ID GG_INBOX_GATE_WINDOW 2>/dev/null || true
unset CLAUDE_CODE_SESSION_ID CLAUDE_SESSION_ID 2>/dev/null || true
export GG_ENFORCEMENT=on

# tab <session-id> <cmd...> — run gg as a Claude tab: generic GG_AGENT so the
# session-id branch fires, plus a per-tab CLAUDE_CODE_SESSION_ID.
tab() {
  _sid="$1"; shift
  env GG_AGENT=claude-code GG_ROLE=reviewer CLAUDE_CODE_SESSION_ID="$_sid" "$GG" "$@"
}
send() { # send a reviewer handoff from master (master role is never self-blocked)
  env GG_AGENT=sender GG_ROLE=master CLAUDE_CODE_SESSION_ID=sender-sid "$GG" tell reviewer "$1" --from master
}

# ─── Phase 0: store must actually serve ──────────────────────────────────────
send "phase0 probe handoff" >/dev/null 2>&1
if ! tab aaaaaaaa0000 inbox --role reviewer --include-agents --peek 2>&1 | grep -q "phase0 probe handoff"; then
  echo "FAIL: message store not serving — gate would fail-open, cannot test"
  exit 1
fi
# Clear it so it doesn't interfere (anonymous global dismiss, isolated project).
env -u GG_AGENT GG_ROLE=reviewer "$GG" inbox --role reviewer --include-agents --dismiss-all >/dev/null 2>&1

# ─── Phase A: per-tab identity isolation ─────────────────────────────────────
# Default 14d window is active (unset) — a just-sent handoff is inside it.
send "recent handoff A" >/dev/null 2>&1

# Tab A reads (records read_by=[claude-code-aaaaaaaa]) then writes → cleared.
tab aaaaaaaa0000 inbox --role reviewer --include-agents >/dev/null 2>&1
set +e
OUT_A=$(tab aaaaaaaa0000 record "probe A after read" 2>&1); RC_A=$?
set -e
if [ "$RC_A" -ne 0 ]; then
  echo "FAIL [Phase A]: tab A read its mail but its own gate still blocks"
  printf '%s\n' "$OUT_A" | tail -3
  exit 1
fi

# Tab B (a DIFFERENT session) never read it → must still block, naming its own id.
set +e
OUT_B=$(tab bbbbbbbb0000 record "probe B" 2>&1); RC_B=$?
set -e
if [ "$RC_B" -eq 0 ]; then
  echo "FAIL [Phase A]: tab B was cleared by tab A's read — identity collapsed (BUG-103)"
  exit 1
fi
case "$OUT_B" in
  *"claude-code-bbbbbbbb"*) ;;
  *) echo "FAIL [Phase A]: block message does not name tab B's per-tab id (identity not per-tab)"; printf '%s\n' "$OUT_B" | grep -i handoff | head -1; exit 1;;
esac

# ─── Phase B1: recency window skips STALE handoffs ───────────────────────────
export GG_INBOX_GATE_WINDOW=2s
send "stale handoff B1" >/dev/null 2>&1
sleep 4   # every accumulated handoff is now older than the 2s window
set +e
OUT_C=$(tab cccccccc0000 record "probe C fresh tab" 2>&1); RC_C=$?
set -e
if [ "$RC_C" -ne 0 ]; then
  echo "FAIL [Phase B1]: fresh tab blocked on handoffs older than the window (gate not bounded — BUG-102 from the other side)"
  printf '%s\n' "$OUT_C" | tail -3
  exit 1
fi
unset GG_INBOX_GATE_WINDOW

# ─── Phase B2: window still blocks a RECENT unread handoff (not defanged) ─────
export GG_INBOX_GATE_WINDOW=1h
send "fresh handoff B2" >/dev/null 2>&1
set +e
OUT_D=$(tab dddddddd0000 record "probe D fresh tab" 2>&1); RC_D=$?
set -e
if [ "$RC_D" -eq 0 ]; then
  echo "FAIL [Phase B2]: fresh unread handoff inside the window did not block — the window defanged the gate"
  exit 1
fi
unset GG_INBOX_GATE_WINDOW

echo "PASS: BUG-103 — per-tab identity from CLAUDE_CODE_SESSION_ID + recency window (fresh tab skips stale handoffs, blocks recent unread, gate stays satisfiable)"
