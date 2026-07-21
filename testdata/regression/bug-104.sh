#!/bin/sh
# Repro for BUG-104: the review-convergence category counter used a bare
# case-insensitive SUBSTRING match (grep -qi "$kw"), so a single-concept trailer
# could trip three keywords via substrings and pass the >=3 bar. The canonical
# offender: "Review-Convergence: retested the latest docstrings for stale imports"
# scores test (retested/latest), docs (docstrings), stale (stale) -> CATS=3 -> a
# false PASS. BUG-101's trailer folding widened the read surface feeding this
# matcher, amplifying the collision.
#
# The fix matches each keyword only at a WORD START ("[^a-zA-Z]<kw>" against a
# space-prefixed copy), a portable POSIX negated class. It must reject the gaming
# trailer while still accepting the legitimate plural/gerund forms
# tests/testing/tested (which grep -qiw would wrongly reject).
#
# Drives the real hook template in a throwaway git repo — no gg store, no network.
set -e

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

commit_case() { echo "$1" >> f.txt; git add f.txt; git commit -q -F -; }

# expect <task-id> <expected-exit> <label>
expect() {
  _tid="$1"; _want="$2"; _label="$3"
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
  echo "ok   [$_label]"
}

# 1. THE GAMING TRAILER: one concept, three substring collisions. Must BLOCK.
commit_case TASK-R104A <<'EOF'
fix(TASK-R104A): single concept, substring-gamed trailer

Review-Convergence: retested the latest docstrings for stale imports
EOF
expect TASK-R104A 7 "single-concept substring trailer blocks (was a false PASS)"

# 2. LEGITIMATE plural/gerund forms must still PASS — the naive grep -qiw fix
#    would wrongly reject these.
commit_case TASK-R104B <<'EOF'
fix(TASK-R104B): legitimate categories in plural/gerund form

Review-Convergence: behavioral tests pass, negative cases covered, legacy paths kept,
  stale strings swept, docs regenerated, smoke checked, testing complete
EOF
expect TASK-R104B 0 "plural/gerund category forms still pass"

# 3. A real terse-but-genuine 3-category single line still passes.
commit_case TASK-R104C <<'EOF'
fix(TASK-R104C): three genuine categories

Review-Convergence: behavior matrix checked, negative path exercised, smoke tested
EOF
expect TASK-R104C 0 "three genuine categories pass"

# 4. Two genuine categories plus substring noise must still BLOCK (bar intact).
commit_case TASK-R104D <<'EOF'
fix(TASK-R104D): two real categories, rest is substring noise

Review-Convergence: behavior matrix, negative path, ran the latest fastest attestation
EOF
expect TASK-R104D 7 "two real + substring noise still blocks"

if [ "$fails" -ne 0 ]; then
  echo "BUG-104 repro FAILED: $fails case(s)"
  exit 1
fi
echo "BUG-104 repro passed: category counting requires word-start keywords, gaming substrings rejected, legit forms kept"
