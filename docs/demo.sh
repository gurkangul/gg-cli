#!/usr/bin/env bash
# gg-cli demo — shared brain for AI agents

DEMO_DIR="/tmp/gg-demo/my-project"
export GG_AGENT="demo"
export TERM=xterm-256color

# ANSI helpers (using printf + \033 for headless compatibility)
GREEN='\033[1;32m'
BLUE='\033[1;34m'
WHITE='\033[1;37m'
DIM='\033[2;37m'
RESET='\033[0m'

_type() {
  printf "${GREEN}❯${RESET} "
  local line="$1"
  for ((i=0; i<${#line}; i++)); do
    printf '%s' "${line:$i:1}"
    sleep 0.042
  done
  printf '\n'
}

run() {
  _type "$*"
  sleep 0.5
  eval "$*"
  sleep 1.3
}

comment() {
  sleep 0.3
  printf "${DIM}  # %s${RESET}\n" "$*"
  sleep 0.8
}

header() {
  printf '\n'
  printf "${BLUE}%s${RESET}\n" "$*"
  sleep 0.5
}

# ── setup ──
clear
sleep 0.7

printf "${WHITE}  gg  ·  shared brain for AI agents${RESET}\n"
printf "${DIM}  github.com/gurkangul/gg-cli${RESET}\n"
printf '\n'
sleep 1.0

comment "Start in any git repo — run gg init once"
_type "mkdir my-project && cd my-project && git init -b main -q && gg init"
sleep 0.5

mkdir -p "$DEMO_DIR" && cd "$DEMO_DIR" && git init -b main -q
gg init 2>&1 \
  | grep -v "^\[" \
  | grep -v "pulling \|verifying\|writing manifest\|^success\|Pulling nomic\|^$" \
  | sed '/^Paste this/,/^Re-install hooks/d' \
  | grep -v "^Re-install"
sleep 1.5

# ── workflow ──
header "── recording decisions ─────────────────────────────────────────"

comment "Any agent records a decision — all others see it immediately"
run "gg record 'use PostgreSQL for primary storage' --reason 'ACID guarantees, battle-tested' --tags 'architecture,database'"

comment "Rejected approaches are stored — no agent re-proposes them"
run "gg record 'MongoDB for primary storage' --stance=reject --reason 'schema flexibility not needed' --tags 'architecture,database'"

header "── searching shared memory ─────────────────────────────────────"

comment "Before touching the DB layer, search what was already decided"
run "gg search 'database' --compact"

comment "Create a task — any agent can pick it up"
run "gg task create 'implement PostgreSQL schema and migrations' --priority high --tags 'backend,database'"

header "── context bundle before coding ───────────────────────────────"

comment "Pull unified context: decisions + rejections + tasks in one call"
run "gg context 'database schema' --compact"

header "── project state ───────────────────────────────────────────────"

run "gg status"

printf '\n'
printf "${GREEN}  ✓ Every agent. Same decisions. Same tasks. No re-derivation.${RESET}\n"
sleep 2.5
