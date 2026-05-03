#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

if ! command -v gg >/dev/null 2>&1; then
  echo "error: gg not found in PATH" >&2
  exit 1
fi
if ! command -v rg >/dev/null 2>&1; then
  echo "error: rg not found in PATH" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "error: jq not found in PATH" >&2
  exit 1
fi

: "${LARGE_FIXTURE_DIR:?set LARGE_FIXTURE_DIR to an absolute path of a larger local fixture repo}"
if [[ ! -d "$LARGE_FIXTURE_DIR" ]]; then
  echo "error: LARGE_FIXTURE_DIR does not exist: $LARGE_FIXTURE_DIR" >&2
  exit 1
fi

run_id="task-373-$(date +%Y%m%d-%H%M%S)"
out_dir="docs/benchmark-results"
mkdir -p "$out_dir"
json_out="$out_dir/${run_id}.json"
md_out="$out_dir/${run_id}.md"

primary_fixture_name="gg-cli"
primary_fixture_path="$ROOT_DIR"
large_fixture_name="large-fixture"
large_fixture_path="$LARGE_FIXTURE_DIR"

count_files() {
  local p="$1"
  (cd "$p" && rg --files | wc -l | tr -d ' ')
}

primary_file_count="$(count_files "$primary_fixture_path")"
large_file_count="$(count_files "$large_fixture_path")"

now_ms() {
  python3 - <<'PY'
import time
print(int(time.time() * 1000))
PY
}

measure_cmd() {
  local dir="$1"
  local cmd="$2"

  local tmp
  tmp="$(mktemp)"

  local start_ms end_ms elapsed_ms
  start_ms="$(now_ms)"
  (
    cd "$dir"
    eval "$cmd"
  ) >"$tmp"
  end_ms="$(now_ms)"
  elapsed_ms=$((end_ms - start_ms))

  local bytes
  bytes="$(wc -c <"$tmp" | tr -d ' ')"
  local stdout_b64
  stdout_b64="$(base64 <"$tmp" | tr -d '\n')"

  rm -f "$tmp"
  printf '%s\t%s\t%s\n' "$elapsed_ms" "$bytes" "$stdout_b64"
}

json_escape() {
  jq -Rsa . <<<"$1"
}

emit_question_result() {
  local fixture_name="$1"
  local qid="$2"
  local question="$3"
  local track="$4"
  local elapsed_ms="$5"
  local command_count="$6"
  local bytes_emitted="$7"
  local compact_savings="$8"
  local sufficiency="$9"
  local notes="${10}"

  jq -n \
    --arg fixture "$fixture_name" \
    --arg id "$qid" \
    --arg question "$question" \
    --arg track "$track" \
    --argjson elapsed_ms "$elapsed_ms" \
    --argjson command_count "$command_count" \
    --argjson bytes_emitted "$bytes_emitted" \
    --arg compact_savings "$compact_savings" \
    --argjson sufficiency "$sufficiency" \
    --arg notes "$notes" \
    '{fixture:$fixture,id:$id,question:$question,track:$track,elapsed_ms:$elapsed_ms,command_count:$command_count,bytes_emitted:$bytes_emitted,compact_savings_bytes:$compact_savings,answer_sufficiency_score:$sufficiency,notes:$notes}'
}

# Questions and command templates
# NOTE: This script captures measurable outputs automatically.
# answer_sufficiency_score must be filled manually post-run (set default -1).

results_tmp="$(mktemp)"
: >"$results_tmp"

run_pair() {
  local fixture_name="$1"
  local fixture_path="$2"
  local qid="$3"
  local question="$4"
  local gg_cmd="$5"
  local gg_cmd_noncompact="$6"
  local rg_cmd="$7"

  local fixture_has_gg="0"
  if [[ -d "$fixture_path/.gg" ]]; then
    fixture_has_gg="1"
  fi

  if [[ "$fixture_has_gg" == "1" ]]; then
    local m gg_elapsed gg_bytes gg_b64
    m="$(measure_cmd "$fixture_path" "$gg_cmd")"
    gg_elapsed="${m%%$'\t'*}"
    m="${m#*$'\t'}"
    gg_bytes="${m%%$'\t'*}"
    gg_b64="${m#*$'\t'}"

    local compact_savings="n/a"
    if [[ -n "$gg_cmd_noncompact" ]]; then
      local m2 gg_nc_elapsed gg_nc_bytes gg_nc_b64
      m2="$(measure_cmd "$fixture_path" "$gg_cmd_noncompact")"
      gg_nc_elapsed="${m2%%$'\t'*}"
      m2="${m2#*$'\t'}"
      gg_nc_bytes="${m2%%$'\t'*}"
      gg_nc_b64="${m2#*$'\t'}"
      compact_savings="$((gg_nc_bytes - gg_bytes))"
    fi

    emit_question_result "$fixture_name" "$qid" "$question" "gg" "$gg_elapsed" 1 "$gg_bytes" "$compact_savings" -1 "sufficiency pending manual scoring" >>"$results_tmp"
  else
    emit_question_result "$fixture_name" "$qid" "$question" "gg" 0 0 0 "n/a" -1 "unsupported: fixture has no .gg project; gg track skipped" >>"$results_tmp"
  fi

  local m3 rg_elapsed rg_bytes rg_b64
  m3="$(measure_cmd "$fixture_path" "$rg_cmd")"
  rg_elapsed="${m3%%$'\t'*}"
  m3="${m3#*$'\t'}"
  rg_bytes="${m3%%$'\t'*}"
  rg_b64="${m3#*$'\t'}"

  emit_question_result "$fixture_name" "$qid" "$question" "rg" "$rg_elapsed" 1 "$rg_bytes" "n/a" -1 "sufficiency pending manual scoring" >>"$results_tmp"
}

# Primary fixture questions (Q1-Q5)
run_pair "$primary_fixture_name" "$primary_fixture_path" "Q1" "Prior rejected approach for a topic (use topic: GSD)" \
  "gg search --compact \"GSD\"" \
  "gg search \"GSD\"" \
  "rg -n \"GSD|reject|rejected\" AGENTS.md docs internal cmd"

run_pair "$primary_fixture_name" "$primary_fixture_path" "Q2" "Existing decisions constraining docs changes" \
  "gg search --compact \"docs decision\"" \
  "gg search \"docs decision\"" \
  "rg -n \"decision|decisions|docs\" docs AGENTS.md"

run_pair "$primary_fixture_name" "$primary_fixture_path" "Q3" "Blast radius for docs path" \
  "gg impact docs --compact" \
  "gg impact docs" \
  "rg -n \"docs/|docs\" --glob '!**/*.sum'"

run_pair "$primary_fixture_name" "$primary_fixture_path" "Q4" "Relevant work context for TASK-373" \
  "gg task get TASK-373 --json" \
  "" \
  "rg -n \"TASK-373|socraticode|benchmark|code-intel\" ."

run_pair "$primary_fixture_name" "$primary_fixture_path" "Q5" "Mandatory workflow rules before closing work" \
  "gg context \"task done verify gate\" --compact" \
  "gg context \"task done verify gate\"" \
  "rg -n \"task done|verify gate|pre-task-done|GG MANDATORY CONTRACT|ACK-OK\" AGENTS.md docs"

# Larger fixture question (Q6)
run_pair "$large_fixture_name" "$large_fixture_path" "Q6" "Large fixture retrieval (blast radius/context surrogate)" \
  "gg search --compact \"TODO\"" \
  "gg search \"TODO\"" \
  "rg -n \"TODO\" ."

jq -s \
  --arg run_id "$run_id" \
  --arg primary_path "$primary_fixture_path" \
  --arg primary_name "$primary_fixture_name" \
  --arg large_path "$large_fixture_path" \
  --arg large_name "$large_fixture_name" \
  --argjson primary_files "$primary_file_count" \
  --argjson large_files "$large_file_count" \
  '{
    run_id:$run_id,
    fixtures:[
      {name:$primary_name,path:$primary_path,file_count:$primary_files},
      {name:$large_name,path:$large_path,file_count:$large_files}
    ],
    methodology:"See docs/code-intelligence-benchmark-task-373.md",
    limitations:[
      "Machine/environment specific timing and output size.",
      "Sufficiency scoring is manual and rubric-guided, not fully objective.",
      "gg quality depends on local gg knowledge population.",
      "rg quality depends on operator query strategy.",
      "Single-run numbers are noisy; repeat for stronger confidence."
    ],
    question_results: .
  }' "$results_tmp" >"$json_out"

{
  echo "# TASK-373 Benchmark Run — ${run_id}"
  echo
  echo "- Primary fixture: ${primary_fixture_name} (${primary_fixture_path}) — files: ${primary_file_count}"
  echo "- Larger fixture: ${large_fixture_name} (${large_fixture_path}) — files: ${large_file_count}"
  echo "- Methodology reference: docs/code-intelligence-benchmark-task-373.md"
  echo
  echo "## Results (sufficiency score pending manual rubric pass)"
  echo
  echo "| Fixture | Q | Track | elapsed_ms | command_count | bytes_emitted | compact_savings_bytes | sufficiency |"
  echo "|---|---|---:|---:|---:|---:|---:|---:|"
  jq -r '.question_results[] | "| \(.fixture) | \(.id) | \(.track) | \(.elapsed_ms) | \(.command_count) | \(.bytes_emitted) | \(.compact_savings_bytes) | \(.answer_sufficiency_score) |"' "$json_out"
  echo
  echo "## Next step"
  echo
  echo "Apply manual answer-sufficiency scoring (0-3 rubric) per row and update both ${json_out} and this report before using numbers for conclusions."
  echo
  echo "## Limitations"
  jq -r '.limitations[] | "- " + .' "$json_out"
} >"$md_out"

rm -f "$results_tmp"

echo "wrote: $json_out"
echo "wrote: $md_out"