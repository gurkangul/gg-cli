#!/bin/sh
# Repro for BUG-100: `gg doctor --sync-artifacts` reported every artifact as
# "missing" when .gg/installed.json was absent, even with the files on disk.
#
# artifacts.Diff decides purely from the manifest and never stats the
# filesystem, and the report path had no empty-manifest guard — so a project
# whose manifest was never written looked like it had lost everything. Worse,
# the report then advised --apply, which would have rewritten all of them
# including locally customised hooks.
#
# At the broken ref this prints artifacts as "missing"; after the fix it says the
# manifest is absent and drift cannot be assessed.
#
# The config is hand-written rather than via `gg init` so the script leaves no
# entry in the user's global registry.
set -e
cd "$(git rev-parse --show-toplevel)"

BINDIR=$(mktemp -d)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP" "$BINDIR"' EXIT

go build -o "$BINDIR/gg" ./cmd/gg
cd "$TMP"
git init -q .
mkdir -p .gg/hooks/pre-task-done.d
cat > .gg/config.yaml <<'YAML'
schema_version: 1
project_id: 00000000-0000-4000-8000-000000000100
embedding:
    backend: ollama
    host: http://localhost:11434
    model: qwen3-embedding:0.6b
YAML
# Artifacts present on disk; installed.json deliberately absent.
printf '# local rules\n' > .gg/RULES.md
printf '#!/bin/sh\nexit 0\n' > .gg/hooks/pre-task-done.d/30-file-size.sh
printf '# agents\n' > AGENTS.md

OUT=$("$BINDIR/gg" doctor --sync-artifacts 2>&1 || true)

# Match the drift TABLE row ("  ? <key>   missing"), not the word "missing"
# wherever it appears — the fixed report explains the situation in prose that
# legitimately contains the word, and an earlier draft of this check failed on
# its own fix because of that.
if printf '%s' "$OUT" | grep -qE '^[[:space:]]+\?[[:space:]]+\S+[[:space:]]+missing[[:space:]]*$'; then
  echo "FAIL: artifacts reported missing despite being on disk"
  printf '%s\n' "$OUT" | head -12
  exit 1
fi
if ! printf '%s' "$OUT" | grep -qi "manifest"; then
  echo "FAIL: expected the report to explain the absent manifest, got:"
  printf '%s\n' "$OUT" | head -12
  exit 1
fi
echo "PASS: BUG-100 — absent manifest is reported as such, not as missing artifacts"
