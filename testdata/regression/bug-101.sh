#!/bin/sh
set -eu

# BUG-101 (regression guard):
# The review-convergence gate read the Review-Convergence attestation with
#   grep -Ei '^Review-Convergence:' | head -1
# so it only ever scanned ONE physical line. Continuation lines of a wrapped
# trailer never matched the anchored grep, and repeated trailer keys were cut by
# head -1. The result was an inverted incentive: an attestation naming all seven
# matrix categories across seven lines counted as ONE category and was rejected
# with exit 7, while the SAME content collapsed onto one line passed. The block
# message compounded it by claiming the trailer was "missing" when it was
# present but thin.
#
# The fix folds the trailer block (the RFC-822 model git interpret-trailers uses)
# and aggregates repeated keys before counting, without lowering the >=3 bar.
#
# This repro drives the real hook script in a THROWAWAY git repo. It never
# touches the gg store, the project's own .gg state, or the network: the hook's
# only `gg` calls live in the GG_REVIEW_CONVERGENCE=off and
# GG_ALLOW_INCOMPLETE_REVIEW bypass paths, neither of which is exercised here.

# The repro runner invokes this from the project root, but resolve the root
# defensively so a manual run from any directory still drives the real hook
# instead of silently missing it.
ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || ROOT=""
[ -n "$ROOT" ] || ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT"

HOOK="$(pwd)/internal/templates/pre-task-done-review-convergence.sh"
[ -f "$HOOK" ] || { echo "FAIL: hook template not found at $HOOK"; exit 1; }

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

cd "$WORK"
git init -q .
git config user.email repro@example.com
git config user.name repro
git config commit.gpgsign false

fails=0

# commit <task-id> <message-body-on-stdin> — always changes a real file, because
# the gate deliberately short-circuits on commits that touch no files.
commit_case() {
  _tid="$1"
  echo "$_tid" >> f.txt
  git add f.txt
  git commit -q -F -
}

# expect <task-id> <expected-exit> <substring-that-must-appear> <label>
expect() {
  _tid="$1"; _want="$2"; _needle="$3"; _label="$4"
  set +e
  _out=$(GG_TASK_ID="$_tid" sh "$HOOK" 2>&1)
  _code=$?
  set -e
  if [ "$_code" -ne "$_want" ]; then
    echo "FAIL [$_label]: expected exit $_want, got $_code"
    echo "$_out" | sed 's/^/    /'
    fails=$((fails + 1))
    return
  fi
  if ! printf '%s' "$_out" | grep -qi "$_needle"; then
    echo "FAIL [$_label]: exit $_code correct, but output missing '$_needle'"
    echo "$_out" | sed 's/^/    /'
    fails=$((fails + 1))
    return
  fi
  echo "ok   [$_label]"
}

# 1. THE REGRESSION ITSELF: a folded trailer naming 7 categories across 7 lines.
#    Pre-fix this counted 1 category and exited 7.
commit_case TASK-R101A <<'EOF'
fix(TASK-R101A): folded trailer naming every matrix category

Review-Convergence: behavior matrix (default/configured/invalid inputs),
  negative path (missing env, bad args, store failure),
  legacy compatibility (old config and command names),
  stale-string sweep (rg across source, tests, docs, templates),
  docs/templates/generated artifacts regenerated and diffed,
  live smoke (real CLI output inspected),
  test evidence (targeted + relevant suite)
EOF
expect TASK-R101A 0 "enumerates 7 matrix categories" "folded 7-category trailer passes"

# 2. NO REGRESSION: the same content on a single line still passes.
commit_case TASK-R101B <<'EOF'
fix(TASK-R101B): single-line trailer

Review-Convergence: behavior matrix, negative path, legacy compatibility, stale-string sweep, docs/templates, live smoke, test evidence
EOF
expect TASK-R101B 0 "enumerates 7 matrix categories" "single-line trailer still passes"

# 3. BAR NOT LOWERED (BUG-077 intent): a genuinely thin trailer still blocks,
#    however it is folded — and the headline names the real cause.
commit_case TASK-R101C <<'EOF'
fix(TASK-R101C): thin trailer, folded across lines

Review-Convergence: behavior matrix only,
  nothing else was actually verified here
EOF
expect TASK-R101C 7 "present but too thin" "thin folded trailer still blocks"

# 4. A bare cargo-cult token names zero categories and must still block.
commit_case TASK-R101D <<'EOF'
fix(TASK-R101D): bare token

Review-Convergence:
EOF
expect TASK-R101D 7 "present but too thin" "bare Review-Convergence: token blocks"

# 5. A genuinely absent trailer must still block AND still say "missing" — the
#    two failure modes must stay distinguishable.
commit_case TASK-R101E <<'EOF'
fix(TASK-R101E): no attestation at all
EOF
expect TASK-R101E 7 "trailer missing" "absent trailer blocks and reads as missing"

# 6. Repeated trailer keys aggregate instead of being truncated to the first.
commit_case TASK-R101F <<'EOF'
fix(TASK-R101F): repeated keys

Review-Convergence: behavior matrix
Review-Convergence: negative path
Review-Convergence: live smoke evidence
EOF
expect TASK-R101F 0 "enumerates 3 matrix categories" "repeated keys aggregate"

# 7. Folding stops at the next "Key: value" trailer, so an adjacent
#    Co-Authored-By / Signed-off-by block is not absorbed into the attestation.
commit_case TASK-R101G <<'EOF'
fix(TASK-R101G): trailer followed by other trailers

Review-Convergence: behavior matrix,
  negative path,
  live smoke
Co-Authored-By: Someone <someone@example.com>
Signed-off-by: Someone <someone@example.com>
EOF
expect TASK-R101G 0 "enumerates 3 matrix categories" "folding stops at the next trailer key"

# 8. A blank line terminates the folded block: prose after it must not be
#    counted as attestation content.
commit_case TASK-R101H <<'EOF'
fix(TASK-R101H): blank line terminates the block

Review-Convergence: behavior matrix,
  negative path

This paragraph mentions legacy compatibility, stale strings, docs and smoke
tests, but it is prose after a blank line and must NOT be folded into the
attestation — otherwise the gate could be satisfied by unrelated commit prose.
EOF
expect TASK-R101H 7 "present but too thin" "blank line ends the folded block"

if [ "$fails" -ne 0 ]; then
  echo "BUG-101 repro FAILED: $fails case(s)"
  exit 1
fi
echo "BUG-101 repro passed: folded trailers are read whole, and the >=3 bar is intact"
