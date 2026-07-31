#!/bin/sh
# Repro for BUG-107: two ways the file-size rule stayed silent about a file that
# was one edit from a hard block.
#
#   1. `gg audit file-size --over N` documented itself as "a custom threshold
#      instead of the per-type defaults" but was implemented as a filter over the
#      already-computed >500 violation set, so it could only ever NARROW that
#      list. `--over 100` and `--over 440` returned byte-identical output and no
#      file under its limit could be reported at any value of N.
#
#   2. The rule only ever fired strictly ABOVE the limit. A file could sit at
#      499/500 producing no signal at all, and the next two-line edit flipped it
#      straight to a violation. A limit with no approach warning reports
#      "compliant" right up to the wall.
#
# At the broken ref step 1 fails behaviourally — the two thresholds return the
# same count — and step 2 finds no "approaching limit" section at all.
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

# 300 = quiet, 460 = inside the >=90% band (450), 600 = a real violation.
awk 'BEGIN{for(i=0;i<300;i++) print "package p"}' > quiet.go
awk 'BEGIN{for(i=0;i<460;i++) print "package p"}' > band.go
awk 'BEGIN{for(i=0;i<600;i++) print "package p"}' > over.go

count_rows() {
  # data rows only: the table body is indented and the separator is dashes
  "$BINDIR/gg" audit file-size --no-baseline --over "$1" 2>/dev/null \
    | grep -cE '^  [A-Za-z0-9_./-]+\.go ' || true
}

LOW=$(count_rows 100)
HIGH=$(count_rows 500)

echo "--over 100 -> $LOW row(s); --over 500 -> $HIGH row(s)"

if [ "$LOW" -ne 3 ]; then
  echo "BUG-107: --over 100 reported $LOW file(s), expected all 3 — --over is not a real threshold" >&2
  exit 1
fi
if [ "$HIGH" -ne 1 ]; then
  echo "BUG-107: --over 500 reported $HIGH file(s), expected 1" >&2
  exit 1
fi
if [ "$LOW" -le "$HIGH" ]; then
  echo "BUG-107: --over is behaving as a post-filter — a lower threshold must report MORE files" >&2
  exit 1
fi

# The warning band: band.go is compliant (460 <= 500) but within 90% of the
# limit, so it must be reported as approaching without becoming a violation.
OUT=$("$BINDIR/gg" audit file-size --no-baseline 2>/dev/null)

if ! printf '%s' "$OUT" | grep -q "approaching limit"; then
  echo "BUG-107: no warning band — a file at 460/500 produces no signal at all" >&2
  printf '%s\n' "$OUT" >&2
  exit 1
fi
if ! printf '%s' "$OUT" | sed -n '/approaching limit/,$p' | grep -q 'band\.go'; then
  echo "BUG-107: band.go (460/500) missing from the approaching-limit section" >&2
  printf '%s\n' "$OUT" >&2
  exit 1
fi
if printf '%s' "$OUT" | sed -n '1,/approaching limit/p' | grep -q 'band\.go'; then
  echo "BUG-107: band.go was reported as a VIOLATION — the band must never block" >&2
  exit 1
fi
if ! printf '%s' "$OUT" | grep -q 'over\.go'; then
  echo "BUG-107: over.go (600/500) is a real violation and must still be reported" >&2
  exit 1
fi
if printf '%s' "$OUT" | grep -q 'quiet\.go'; then
  echo "BUG-107: quiet.go (300/500) is far from the limit and must stay silent" >&2
  exit 1
fi

echo "BUG-107 repro OK: --over is a real threshold and the 90% band reports without blocking"
