#!/bin/sh
# Verification for the TASK-519 verification-age trust tiers.
#
# The tier a decision renders with (verified / verified·aging / verified·stale /
# unverified) is time-dependent, which is why it originally looked untestable
# without a Go test fabricating a clock — and the project's no-unit-tests policy
# rules those out. It turns out the clock never needed faking: the RECORD dates
# do. Writing decisions straight into .gg/brain/decisions.jsonl with chosen
# created_at values exercises the real end-to-end render path, which is a
# stronger check than a unit test of the pure function would have been.
#
# Covers all five cases: fresh, past the aging threshold, past the stale
# threshold, evidence-less, and pinned-and-ancient (decay-exempt).
#
# The config is written by hand rather than via `gg init`: init registers the
# project in ~/.gg/projects.json, and a verification script must not leave
# entries in the user's global registry.
#
# Not registered as a bug repro (there is no bug) — run it directly:
#   sh testdata/regression/trust-tiers.sh
set -e
cd "$(git rev-parse --show-toplevel)"

command -v python3 >/dev/null 2>&1 || { echo "SKIP: python3 required"; exit 0; }

BINDIR=$(mktemp -d)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP" "$BINDIR"' EXIT

go build -o "$BINDIR/gg" ./cmd/gg
cd "$TMP"
git init -q .
mkdir -p .gg/brain
cat > .gg/config.yaml <<'YAML'
schema_version: 1
project_id: 00000000-0000-4000-8000-000000000519
embedding:
    backend: ollama
    host: http://localhost:11434
    model: qwen3-embedding:0.6b
YAML

# Dates are relative to "now", so the thresholds are exercised whenever this runs
# rather than only on the day it was written.
python3 - <<'PY'
import json, uuid, datetime

now = datetime.datetime.now(datetime.timezone.utc)

def rec(text, days_ago, evidence=True, pinned=False):
    ts = (now - datetime.timedelta(days=days_ago)).strftime('%Y-%m-%dT%H:%M:%SZ')
    return {
        "uuid": str(uuid.uuid4()), "kind": "decisions", "created_at": ts, "author": "tester",
        "payload": {
            "text": text, "reason": "trust tier probe", "status": "active", "tags": [],
            "task_id": "", "author": "tester", "created_at": ts,
            "evidence": "verified by live smoke" if evidence else "", "pinned": pinned,
        },
    }

rows = [
    rec("TIER PROBE fresh", 1),                        # < 60d  -> verified
    rec("TIER PROBE aging", 61),                       # >= 60d -> aging
    rec("TIER PROBE stale", 181),                      # >= 180d -> stale
    rec("TIER PROBE noevidence", 200, evidence=False), # no evidence -> unverified
    rec("TIER PROBE pinned", 400, pinned=True),        # pinned -> exempt from decay
]
with open('.gg/brain/decisions.jsonl', 'w') as f:
    for r in rows:
        f.write(json.dumps(r) + "\n")
PY

"$BINDIR/gg" doctor --reconcile >/dev/null 2>&1 || true
if ! "$BINDIR/gg" reembed --yes >/dev/null 2>&1; then
  echo "SKIP: embedder unavailable — cannot populate the store for the render path"
  exit 0
fi

OUT=$("$BINDIR/gg" context --compact=false 2>/dev/null)

check() { # <probe name> <expected tier text>
  line=$(printf '%s' "$OUT" | grep -A2 "TIER PROBE $1" | grep -E '\[verified|\[unverified' | head -1)
  case "$line" in
    *"$2"*) echo "  ok   $1 -> $2" ;;
    *) echo "FAIL: $1 expected '$2', got: ${line:-<nothing>}"; exit 1 ;;
  esac
}

# stale is checked before aging: "verified · aging" is not a substring of the
# stale label, but checking in the wrong order would still be fragile if the
# labels ever converge.
check fresh      "[verified]"
check aging      "[verified · aging]"
check stale      "[verified · stale — reverify]"
check noevidence "[unverified]"
check pinned     "[verified]"

echo "PASS: TASK-519 trust tiers — fresh / aging / stale / unverified / pinned-exempt all render correctly"
