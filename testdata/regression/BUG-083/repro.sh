#!/bin/sh
set -eu
# BUG-083: the installed Claude Code UserPromptSubmit inbox hook never injected
# because its grep pattern was 'INBOX \(N unread\)' while the real inbox header
# is 'inbox — N unread'. Also the "present" marker matched any --since-cursor
# command, so broken installs were never rewritten.
#
# Behavioral check: a project carrying the OLD broken hook must be rewritten to
# the fixed grep by `gg doctor --install-agent-hooks --agent claude --force`.

# 1. Source template must carry the fixed grep, not the broken one.
grep -q "grep -qE '\[1-9\]\[0-9\]\* unread'" internal/agenthooks/claude_hooks.go || {
  echo "BUG-083: fixed grep pattern missing from claude_hooks.go template"; exit 1; }
if grep -q "INBOX \\\\(" internal/agenthooks/claude_hooks.go; then
  echo "BUG-083: broken 'INBOX (N unread)' grep still present in template"; exit 1; fi

# 2. Behavioral: broken install gets upgraded.
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
( cd "$WORK" && git init -q && gg init >/dev/null 2>&1 || true )
mkdir -p "$WORK/.claude"
cat > "$WORK/.claude/settings.json" <<'OLD'
{ "env": { "GG_AGENT": "claude-code" }, "hooks": { "UserPromptSubmit": [ { "hooks": [ { "type": "command", "command": "OUT=$(gg inbox --peek --since-cursor --advance-cursor --role \"${GG_ROLE:-}\" --include-agents 2>/dev/null); echo \"$OUT\" | grep -qE 'INBOX \\(([1-9][0-9]*) unread\\)' && jq -n --arg ctx \"$OUT\" '{hookSpecificOutput:{hookEventName:\"UserPromptSubmit\",additionalContext:$ctx}}' || true" } ] } ] } }
OLD
( cd "$WORK" && gg doctor --install-agent-hooks --agent claude --force >/dev/null 2>&1 || true )
if grep -q "INBOX" "$WORK/.claude/settings.json"; then
  echo "BUG-083 REGRESSION: broken hook NOT rewritten"; exit 1; fi
grep -q "grep -qE '\[1-9\]\[0-9\]\* unread'" "$WORK/.claude/settings.json" || {
  echo "BUG-083 REGRESSION: fixed grep not present after sync"; exit 1; }
echo "BUG-083 OK: inbox hook template fixed + stale installs auto-upgraded"
