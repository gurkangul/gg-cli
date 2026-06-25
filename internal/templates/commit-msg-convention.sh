#!/bin/sh
# gg commit-msg hook: commit-message convention check (DEFAULT OFF / opt-in).
#
# Installed by: gg doctor --install-task-hooks
# Hook location: .gg/hooks/commit-msg.d/30-commit-msg.sh
# Invoked by:    .git/hooks/commit-msg <path-to-COMMIT_EDITMSG>
#
# This is a commit-msg hook, not pre-commit: pre-commit runs before the message
# exists and cannot see it. The subject is the first non-comment line.
#
# Default-off so that propagation across projects (gg system sync installs task
# hooks everywhere) never surprise-blocks a commit. Enable per project by
# exporting GG_COMMIT_MSG_GATE, or by adding lines to .gg/commit-msg.conf
# (shell key=value, sourced below; a real environment variable wins over it).
#
# Modes (GG_COMMIT_MSG_GATE):
#   off  (default) — skip entirely (inert)
#   warn           — print advice, exit 0 (commit proceeds)
#   on             — block commit on a violation (exit 7)
#
# Knobs:
#   GG_COMMIT_MSG_MAX_SUBJECT      max subject length (default 72)
#   GG_COMMIT_MSG_PREFIX           ERE the subject must match, e.g.
#                                  '^(feat|fix|chore|docs|refactor|test|perf|build|ci)(\(.+\))?: '
#   GG_COMMIT_MSG_ALLOW_FILENAMES  set to 1 to disable the "no file path in subject" check
#
# Bypass (audited): GG_BYPASS_RATIONALE="<reason>" git commit ...

# Optional persistent per-project config. A shell-exported var overrides the file.
_root=$(git rev-parse --show-toplevel 2>/dev/null)
if [ -n "$_root" ] && [ -f "$_root/.gg/commit-msg.conf" ]; then
  # shellcheck disable=SC1090,SC1091
  . "$_root/.gg/commit-msg.conf"
fi

mode="${GG_COMMIT_MSG_GATE:-off}"
if [ "$mode" = "off" ]; then
  exit 0
fi

if [ -n "$GG_BYPASS_RATIONALE" ]; then
  echo "[commit-msg] ⚠ bypass active: $GG_BYPASS_RATIONALE" >&2
  exit 0
fi

msg_file="$1"
if [ -z "$msg_file" ] || [ ! -f "$msg_file" ]; then
  exit 0
fi

# Subject = first line that is neither blank nor a comment.
subject=$(grep -vE '^[[:space:]]*#' "$msg_file" | sed -n '/[^[:space:]]/{p;q;}')
[ -z "$subject" ] && exit 0

# Never police machine-generated commits.
case "$subject" in
  "Merge "*|"Revert "*|"Reapply "*|"fixup! "*|"squash! "*|"amend! "*) exit 0 ;;
esac

max="${GG_COMMIT_MSG_MAX_SUBJECT:-72}"
violations=""

# 1) Subject length.
len=$(printf '%s' "$subject" | wc -m | tr -d ' ')
if [ "$len" -gt "$max" ]; then
  violations="${violations}  • subject is ${len} chars (max ${max})\n"
fi

# 2) File path / source filename in the subject. Conservative to avoid false
#    positives: a slash-path with any extension (src/components/Login.tsx), or a
#    bare file with a strict source extension. Web-ish names like Next.js / a
#    bare README.md are intentionally not flagged.
if [ "${GG_COMMIT_MSG_ALLOW_FILENAMES:-0}" != "1" ]; then
  if printf '%s' "$subject" | grep -qE '(^|[[:space:]])[A-Za-z0-9_.-]*/[A-Za-z0-9_./-]*\.[A-Za-z0-9]{1,6}'; then
    violations="${violations}  • subject contains a file path — describe the change, not the file\n"
  elif printf '%s' "$subject" | grep -qE '[A-Za-z0-9_-]+\.(go|ts|tsx|py|rs|java|rb|php|cpp|cc|swift|kt|kts)([[:space:],.:)]|$)'; then
    violations="${violations}  • subject names a source file — describe the change, not the file\n"
  fi
fi

# 3) Optional required prefix (e.g. conventional commits). Empty = unchecked.
if [ -n "$GG_COMMIT_MSG_PREFIX" ]; then
  if ! printf '%s' "$subject" | grep -qE "$GG_COMMIT_MSG_PREFIX"; then
    violations="${violations}  • subject does not match required prefix: ${GG_COMMIT_MSG_PREFIX}\n"
  fi
fi

[ -z "$violations" ] && exit 0

echo "[commit-msg] convention check:" >&2
printf "%b" "$violations" >&2
echo "  subject: ${subject}" >&2

if [ "$mode" = "warn" ]; then
  echo "[commit-msg] ⚠ warn mode — commit proceeds. Set GG_COMMIT_MSG_GATE=on to enforce." >&2
  exit 0
fi

echo "[commit-msg] ✗ commit blocked. Fix the subject, or bypass (audited):" >&2
echo "[commit-msg]   GG_BYPASS_RATIONALE='<reason>' git commit ..." >&2
exit 7
