#!/bin/sh
# gg pre-task-done hook: Node.js / Bun / pnpm / Yarn verify gate (BLOCKING).
# Runs BEFORE `gg task done` writes the new state. Non-zero exit aborts
# with ExitVerifyFailed(7) and the task stays in its current state.
#
# Env vars available:
#   GG_TASK_ID       — e.g. TASK-042
#   GG_TASK_SUMMARY  — the done summary text
#   GG_PROJECT_ID    — project UUID
#   GG_ACTOR         — GG_ROLE or GG_AGENT of the caller
#
# Auto-detects the package manager from lockfiles. Edit freely — this file
# ships as a starting point, not a contract.

set -e

# Run from the directory containing package.json (injected by the installer).
# Path is relative to the repo root; "." means package.json sits at the root.
cd "$(dirname "$0")/../../.."
cd "__GG_SUBDIR__"

# Pick the package manager by lockfile presence. First match wins.
if [ -f bun.lockb ] || [ -f bun.lock ]; then
    PM="bun"
elif [ -f pnpm-lock.yaml ]; then
    PM="pnpm"
elif [ -f yarn.lock ]; then
    PM="yarn"
else
    PM="npm"
fi

echo "[verify] package manager: $PM"

run_if_script() {
    # $1 = script name (e.g. "build"); run only if package.json defines it.
    # Keeps this hook working on projects without every script.
    script="$1"
    if grep -q "\"$script\"" package.json 2>/dev/null; then
        case "$PM" in
            bun)  echo "[verify] $PM run $script" && bun run "$script" ;;
            pnpm) echo "[verify] $PM run $script" && pnpm run "$script" ;;
            yarn) echo "[verify] $PM $script"     && yarn "$script" ;;
            npm)  echo "[verify] $PM run $script" && npm run "$script" ;;
        esac
    else
        echo "[verify] skip $script (not defined in package.json)"
    fi
}

echo "[verify] install (frozen)"
case "$PM" in
    bun)  bun install --frozen-lockfile ;;
    pnpm) pnpm install --frozen-lockfile ;;
    yarn) yarn install --frozen-lockfile ;;
    npm)  npm ci ;;
esac

run_if_script typecheck
run_if_script build
run_if_script test

echo "[verify] ✓ all checks passed for $GG_TASK_ID"
