#!/bin/sh
# Repro for BUG-096: gg reembed migrated the corpus to a new model but left
# embedding.model in config.yaml pointing at the OLD one, so the config
# permanently contradicted the vectors and GG_EMBED_MODEL stayed mandatory.
#
# Fix: config.UpdateEmbeddingModel writes the migrated model back, refreshes the
# stale "<N>-dim" claim in the trailing comment, and does it as a targeted line
# rewrite so comments survive (Save() would marshal them away) and
# embedding.voyage.model — a different setting, nested deeper — is left alone.
#
# At the broken ref this script fails to build: UpdateEmbeddingModel does not
# exist yet.
set -e
cd "$(git rev-parse --show-toplevel)"

# Underscore prefix keeps the probe out of ./... package matching.
PROBE=./_bug096probe
TMP=$(mktemp -d)
trap 'rm -rf "$TMP" "$PROBE"' EXIT
mkdir -p "$TMP/.gg" "$PROBE"

cat > "$TMP/.gg/config.yaml" <<'YAML'
project_id: bug-096
embedding:
    backend: ollama                # ollama (default, offline) | voyage (cloud)
    host: http://localhost:11434   # Ollama base URL
    model: nomic-embed-text        # 768-dim — matches the vector store size
    voyage:
        model: voyage-3.5-lite     # must NOT be touched
        output_dim: 1024
YAML

cat > "$PROBE/main.go" <<'GO'
package main

import (
	"fmt"
	"os"

	"github.com/gurkangul/gg-cli/internal/config"
)

func main() {
	ggDir := os.Args[1]
	changed, err := config.UpdateEmbeddingModel(ggDir, "qwen3-embedding:0.6b", 1024)
	if err != nil || !changed {
		fmt.Printf("FAIL: first write-back changed=%v err=%v\n", changed, err)
		os.Exit(1)
	}
	// Re-running a completed migration must not rewrite the file again.
	again, err := config.UpdateEmbeddingModel(ggDir, "qwen3-embedding:0.6b", 1024)
	if err != nil || again {
		fmt.Printf("FAIL: not idempotent changed=%v err=%v\n", again, err)
		os.Exit(1)
	}
	// A project without a config must be a silent no-op, never an error.
	missing, err := config.UpdateEmbeddingModel(ggDir+"/absent", "x", 1)
	if err != nil || missing {
		fmt.Printf("FAIL: missing config not a no-op changed=%v err=%v\n", missing, err)
		os.Exit(1)
	}
}
GO

go run "$PROBE" "$TMP/.gg"

CFG="$TMP/.gg/config.yaml"
fail() { echo "FAIL: $1"; echo "--- config.yaml ---"; cat "$CFG"; exit 1; }

grep -q 'model: qwen3-embedding:0.6b' "$CFG" || fail "embedding.model was not updated"
grep -q '1024-dim'                    "$CFG" || fail "stale 768-dim claim was carried over"
# Not `grep … && fail`: under set -e a non-matching grep makes the whole
# && list non-zero and would abort the script on the passing path.
if grep -q '768-dim' "$CFG"; then fail "stale 768-dim claim still present"; fi
grep -q 'voyage-3.5-lite'             "$CFG" || fail "embedding.voyage.model was clobbered"
grep -q 'ollama (default, offline)'   "$CFG" || fail "comments were destroyed"
grep -q 'http://localhost:11434'      "$CFG" || fail "unrelated keys were lost"

echo "PASS: BUG-096 — model+dim written back; comments, voyage.model and other keys intact"
