#!/bin/sh
# gg pre-task-done hook: dependency-justification gate.
#
# When this task's diff ADDS a dependency to a package manifest, require a
# decision linked to the task that names it. The contract's engineering baseline
# says a new dependency must buy something concrete and that the reason belongs
# in the gg record — the lockfile preserves the fact, never the reason, and a
# dependency is the one kind of change that is permanent by default.
#
# Version bumps are NOT flagged: the dependency name appears on both sides of
# the diff, so only genuinely new names are reported. Removals are ignored.
#
# Modes (via GG_DEP_GATE):
#   warn  (default) — print findings, exit 0 (task close proceeds)
#   block           — print findings, exit 7 (task stays in current state)
#   off             — skip entirely
# Bypass in block mode: set GG_ALLOW_DEP=<reason> to log + skip.
#
# Env vars available:
#   GG_TASK_ID, GG_TASK_SUMMARY, GG_PROJECT_ID, GG_ACTOR

MODE="${GG_DEP_GATE:-warn}"
if [ "$MODE" = "off" ]; then
  exit 0
fi

RANGE="HEAD"
CHANGED=$(git diff --name-only HEAD 2>/dev/null)
if [ -z "$CHANGED" ]; then
  RANGE="HEAD~1 HEAD"
  CHANGED=$(git diff --name-only HEAD~1 HEAD 2>/dev/null)
fi
if [ -z "$CHANGED" ]; then
  exit 0
fi

# Pull dependency names out of one manifest's diff side. Reading from stdin
# keeps the added/removed passes identical apart from the grep that feeds them.
extract_names() {
  case "$1" in
    go.mod)
      grep -oE '[A-Za-z0-9._-]+\.[A-Za-z]{2,}/[A-Za-z0-9._/-]+' ;;
    package.json|composer.json)
      sed -nE 's/^[[:space:]]*"([^"]+)"[[:space:]]*:.*/\1/p' \
        | grep -vxE 'name|version|description|scripts|dependencies|devDependencies|peerDependencies|optionalDependencies|require|require-dev|main|type|license|private|author|keywords|files|engines|exports|repository|homepage|bugs|workspaces' ;;
    requirements.txt)
      sed -nE 's/^([A-Za-z0-9._-]+).*/\1/p' ;;
    pyproject.toml|Cargo.toml)
      sed -nE 's/^[[:space:]]*([A-Za-z0-9._-]+)[[:space:]]*=.*/\1/p' \
        | grep -vxE 'name|version|edition|description|license|authors|readme|repository|homepage|keywords|categories|requires-python' ;;
    Gemfile)
      sed -nE "s/^[[:space:]]*gem[[:space:]]+['\"]([^'\"]+)['\"].*/\1/p" ;;
    pom.xml)
      sed -nE 's|.*<artifactId>([^<]+)</artifactId>.*|\1|p' ;;
    build.gradle|build.gradle.kts)
      sed -nE "s/.*['\"]([A-Za-z0-9._-]+:[A-Za-z0-9._-]+):[^'\"]*['\"].*/\1/p" ;;
    *)
      cat >/dev/null ;;
  esac
}

NEW=""

for f in $CHANGED; do
  case "$f" in
    vendor/*|node_modules/*|testdata/*) continue ;;
  esac

  BASE=$(basename "$f")
  case "$BASE" in
    go.mod|package.json|composer.json|requirements.txt|pyproject.toml|Cargo.toml|Gemfile|pom.xml|build.gradle|build.gradle.kts) ;;
    *) continue ;;
  esac

  DIFF=$(git diff -U0 $RANGE -- "$f" 2>/dev/null)
  [ -n "$DIFF" ] || continue

  ADDED=$(printf '%s\n' "$DIFF" | grep '^+' | grep -v '^+++' | sed 's/^+//' | extract_names "$BASE" | sort -u)
  REMOVED=$(printf '%s\n' "$DIFF" | grep '^-' | grep -v '^---' | sed 's/^-//' | extract_names "$BASE" | sort -u)

  for n in $ADDED; do
    # Present on both sides => a version bump or a reordering, not a new dep.
    if printf '%s\n' "$REMOVED" | grep -qxF "$n"; then
      continue
    fi
    NEW="$NEW $n"
  done
done

if [ -z "$NEW" ]; then
  exit 0
fi

DECISIONS=""
if [ -n "$GG_TASK_ID" ] && command -v gg >/dev/null 2>&1; then
  DECISIONS=$(gg task decisions "$GG_TASK_ID" --json 2>/dev/null)
fi

UNJUSTIFIED=""
for n in $NEW; do
  if [ -n "$DECISIONS" ] && printf '%s' "$DECISIONS" | grep -qF "$n"; then
    continue
  fi
  # A module path is rarely quoted in full in prose, so accept the trailing
  # segment too — but only when it is distinctive enough to mean something.
  SHORT="${n##*/}"
  if [ ${#SHORT} -ge 4 ] && [ -n "$DECISIONS" ] && printf '%s' "$DECISIONS" | grep -qF "$SHORT"; then
    continue
  fi
  UNJUSTIFIED="$UNJUSTIFIED\n  $n"
done

if [ -z "$UNJUSTIFIED" ]; then
  exit 0
fi

printf "[dep-gate] New dependencies with no linked decision naming them:%b\n" "$UNJUSTIFIED" >&2
echo "[dep-gate] Record why each one earns its place:" >&2
echo "[dep-gate]   gg record \"<dependency> adopted for <purpose>\" --task ${GG_TASK_ID:-TASK-XXX} --reason \"<what it buys, what it replaces>\"" >&2

if [ "$MODE" = "block" ]; then
  if [ -n "$GG_ALLOW_DEP" ]; then
    if command -v gg >/dev/null 2>&1; then
      gg record "dependency gate bypassed" \
        --reason "GG_ALLOW_DEP=${GG_ALLOW_DEP}; task=${GG_TASK_ID}" \
        --tags "bypass,dep-gate" 2>/dev/null || true
    fi
    printf "[dep-gate] bypass accepted (reason: %s)\n" "$GG_ALLOW_DEP" >&2
    exit 0
  fi
  echo "[dep-gate] Set GG_DEP_GATE=warn to downgrade, or GG_ALLOW_DEP=<reason> to bypass." >&2
  exit 7
fi

# warn mode: non-blocking
exit 0
