#!/bin/sh
# gg pre-commit hook: secret scan via gitleaks (BLOCKING).
# Runs against staged files before a commit lands. Exits 7 on findings.
#
# Installed by: gg doctor --install-task-hooks
# Hook location: .gg/hooks/pre-commit.d/20-secret-scan.sh
#
# Bypass (audited):
#   GG_BYPASS_RATIONALE="<reason>" git commit ...
#
# Modes (via GG_SECRET_SCAN env var):
#   on   (default) — block commit on findings (exit 7)
#   warn           — print warning, exit 0 (commit proceeds)
#   off            — skip entirely

set -e

# Bypass (audited): GG_BYPASS_RATIONALE="<reason>" git commit ...
# The rationale is logged to stderr for auditability — no silent bypasses.
if [ -n "$GG_BYPASS_RATIONALE" ]; then
  echo "[secret-scan] ⚠ bypass active: $GG_BYPASS_RATIONALE" >&2
  exit 0
fi

mode="${GG_SECRET_SCAN:-on}"
if [ "$mode" = "off" ]; then
  exit 0
fi

# Locate gitleaks binary: $GG_GITLEAKS_BIN override, then ~/.gg/bin/gitleaks, then PATH.
GITLEAKS_BIN="${GG_GITLEAKS_BIN:-}"
if [ -z "$GITLEAKS_BIN" ]; then
  home_bin="$HOME/.gg/bin/gitleaks"
  if [ -x "$home_bin" ]; then
    GITLEAKS_BIN="$home_bin"
  elif command -v gitleaks >/dev/null 2>&1; then
    GITLEAKS_BIN="gitleaks"
  fi
fi

if [ -z "$GITLEAKS_BIN" ]; then
  echo "[secret-scan] ⚠ gitleaks not found — falling back to narrow-regex scrub" >&2
  echo "[secret-scan]   For full coverage: gg doctor --install-secret-scanner" >&2
  # Narrow fallback: grep staged files for high-signal patterns.
  staged_files=$(git diff --cached --name-only --diff-filter=ACMRT 2>/dev/null || true)
  if [ -z "$staged_files" ]; then
    exit 0
  fi
  found=0
  for f in $staged_files; do
    if [ -f "$f" ]; then
      if git show ":$f" 2>/dev/null | grep -qE '(sk-[A-Za-z0-9-]{20,}|AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9]{36}|-----BEGIN [A-Z ]+ PRIVATE KEY-----)'; then
        echo "[secret-scan] ✗ possible secret in staged file: $f" >&2
        found=1
      fi
    fi
  done
  if [ "$found" = "1" ]; then
    if [ "$mode" = "warn" ]; then
      echo "[secret-scan] ⚠ secrets detected (warn mode — commit proceeds)" >&2
      exit 0
    fi
    echo "[secret-scan] ✗ commit blocked — fix secrets or set GG_BYPASS_RATIONALE to bypass" >&2
    exit 7
  fi
  exit 0
fi

echo "[secret-scan] running gitleaks on staged files ..."
# 'git --staged' scans only what's in the git index (staged files), not the full tree.
# gitleaks v8.21+ moved git scanning from 'detect --staged' into the 'git' subcommand.
if "$GITLEAKS_BIN" git --staged --no-banner --exit-code 1 2>/dev/null; then
  echo "[secret-scan] ✓ no secrets found"
  exit 0
fi

if [ "$mode" = "warn" ]; then
  echo "[secret-scan] ⚠ gitleaks found secrets in staged files (warn mode — commit proceeds)" >&2
  exit 0
fi

echo "[secret-scan] ✗ commit blocked — gitleaks found secrets in staged files" >&2
echo "[secret-scan]   Fix: remove the secret, rotate it, then recommit." >&2
echo "[secret-scan]   Bypass (audited): GG_BYPASS_RATIONALE='<reason>' git commit ..." >&2
exit 7
