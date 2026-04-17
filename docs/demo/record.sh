#!/usr/bin/env bash
# gg 90-second demo recording script.
# Produces docs/demo/demo.cast (asciinema v2) and docs/demo/demo.svg.
#
# Prerequisites:
#   - asciinema 3.x installed: brew install asciinema
#   - svg-term-cli: npm install -g svg-term-cli
#   - gg installed and gg init run in a project dir
#   - Qdrant + Ollama running
#
# Usage:
#   cd docs/demo && bash record.sh
#   DEMO_LIVE_DATA=1 bash record.sh  # use real gg-cli project stats

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CAST="$SCRIPT_DIR/demo.cast"
SVG="$SCRIPT_DIR/demo.svg"

DEMO_DIR="${DEMO_DIR:-/tmp/gg-demo-project}"
DEMO_LIVE_DATA="${DEMO_LIVE_DATA:-0}"

# ── Helpers ─────────────────────────────────────────────────────────────────

GREEN='\033[1;32m'
RESET='\033[0m'

_type() {
  printf "${GREEN}❯${RESET} "
  local line="$1"
  for ((i=0; i<${#line}; i++)); do
    printf '%s' "${line:$i:1}"
    sleep 0.060
  done
  printf '\n'
}

run() {
  _type "$*"
  sleep 0.4
  eval "$*" 2>&1 || true
  sleep 1.2
}

pause() {
  sleep "${1:-0.8}"
}

# ── Demo project setup ───────────────────────────────────────────────────────

setup_demo_project() {
  if [[ "$DEMO_LIVE_DATA" == "1" ]]; then
    echo "Using live gg-cli project data…"
    return
  fi
  mkdir -p "$DEMO_DIR"
  cd "$DEMO_DIR"
  if [[ ! -d ".gg" ]]; then
    gg init --yes 2>/dev/null || true
    # Import demo seed data.
    REPO_ROOT="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel 2>/dev/null || echo '')"
    if [[ -n "$REPO_ROOT" && -d "$REPO_ROOT/seed/demo_project" ]]; then
      gg brain import "$REPO_ROOT/seed/demo_project" --yes --skip-embed-check 2>/dev/null || true
    fi
  fi
}

# ── Record ───────────────────────────────────────────────────────────────────

echo "Recording demo to $CAST …"
setup_demo_project

if [[ "$DEMO_LIVE_DATA" == "1" ]]; then
  WORK_DIR="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)"
else
  WORK_DIR="$DEMO_DIR"
fi

export GG_SKIP_SHIP_CHECK=1
export GG_TELEMETRY=0

asciinema rec "$CAST" \
  --title "gg — shared brain for AI agents" \
  --cols 80 --rows 24 \
  --command "bash -c '
    cd \"$WORK_DIR\"

    # Act 1: Hook
    sleep 0.5
    gg status
    sleep 2

    # Act 2: Install (simulated — already installed)
    echo
    printf \"\\033[2;37m# Install (see docs/ONBOARDING.md for full steps)\\033[0m\n\"
    sleep 0.5
    echo \"❯ go install github.com/gurkangul/gg-cli/cmd/gg@latest\"
    sleep 0.8
    echo \"❯ gg init && gg doctor\"
    sleep 1
    gg doctor 2>/dev/null || true
    sleep 2

    # Act 3a: Record a decision
    echo
    gg record \"use PostgreSQL for primary store\" \\
      --reason \"ACID compliance, team familiarity\" \\
      --tags \"database,architecture\"
    sleep 2

    # Act 3b: Create + close a task
    echo
    TASK_ID=\$(gg task create \"set up database migrations\" --priority high --json 2>/dev/null | grep -o \"TASK-[0-9]*\" | head -1)
    sleep 0.5
    gg task done \"\$TASK_ID\" \"golang-migrate integrated, 3 migrations committed\" 2>/dev/null || true
    sleep 1.5

    # Act 3c: Search
    echo
    gg search \"database\" --compact
    sleep 2

    # Act 4: Close
    echo
    gg status
    sleep 3
  '"

echo "Cast saved to $CAST"

# ── Convert to SVG ───────────────────────────────────────────────────────────

if command -v svg-term &>/dev/null; then
  echo "Generating SVG…"
  svg-term --in "$CAST" --out "$SVG" --width 80 --height 24 --window
  echo "SVG saved to $SVG"
else
  echo "svg-term not found — skipping SVG generation."
  echo "Install with: npm install -g svg-term-cli"
fi

echo "Done! Commit both $CAST and $SVG."
