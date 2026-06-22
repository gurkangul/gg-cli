#!/bin/sh
# BUG-095: `gg index --changed` (no --lang) defaulted to --lang go and failed
# "no go modules found" on non-go projects, so the auto-refresh git hooks from
# `gg doctor --install-index-hooks` (which run a language-agnostic
# `gg index --changed`) never updated the CodeGraph for TS/Vue/Swift/Python repos.
#
# Pre-fix (9323ff0): index --changed assumes lang=go → "no go modules found" → exit 1.
# Post-fix: runIndex resolves the language(s) from index-state when --lang is
#           absent → no go-default error on a typescript project → exit 0.
#
# Behavioral repro: build gg from the current checkout, point it at a throwaway
# project whose index-state records only typescript, and assert `gg index
# --changed` does NOT fall back to the go default.
set -eu
unset GG_EMBED_MODEL 2>/dev/null || true

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

bindir=$(mktemp -d)
proj=$(mktemp -d)
cleanup() { rm -rf "$bindir" "$proj"; }
trap cleanup EXIT INT TERM

go build -o "$bindir/gg" ./cmd/gg

# Throwaway project that was "indexed as typescript" (no go module anywhere).
mkdir -p "$proj/.gg"
cat > "$proj/.gg/config.yaml" <<'YAML'
project_id: bug095-repro
embedding:
  host: http://localhost:11434
  model: nomic-embed-text
YAML
git -C "$proj" init -q
git -C "$proj" config user.email repro@example.test
git -C "$proj" config user.name repro
: > "$proj/readme.md"
git -C "$proj" add -A
git -C "$proj" commit -q -m init
head=$(git -C "$proj" rev-parse HEAD)

# index-state records typescript only — mirrors a TS/Vue project's graph.
cat > "$proj/.gg/index-state.json" <<JSON
{"languages":{"typescript":{"last_indexed_sha":"$head","working_tree_fingerprint":"","extensions":[".ts",".tsx"]}}}
JSON

cd "$proj"
out=$("$bindir/gg" index --changed 2>&1 || true)

if printf '%s' "$out" | grep -q "no go modules"; then
  echo "BUG-095 PRESENT: 'gg index --changed' defaulted to go on a typescript-only project:"
  printf '%s\n' "$out"
  exit 1
fi
echo "BUG-095 fixed: 'gg index --changed' resolved the indexed language (no go-default fallback)"
exit 0
