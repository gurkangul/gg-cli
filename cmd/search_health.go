package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/outbox"
	"github.com/gurkangul/gg-cli/internal/store"
)

// search_health.go — TASK-516 semantic-coverage notice for reads.
//
// Two states leave brain records present in the ledger but absent from (or
// filtered out of) the semantic index, and both used to be completely silent:
//
//   - Outbox backlog: a record written while the embedder was unreachable is
//     durable in .gg/brain/*.jsonl and queued in the outbox, but no point was
//     ever inserted into the vector collection.
//   - Degraded placeholder vectors: offline reconcile upserts zero-vectors
//     stamped gg_vector_degraded, which every Search* deliberately filters out
//     (they would otherwise rank by garbage cosine — BUG-066).
//
// Either way semantic recall is partial. Since TASK-516 the always-on lexical
// tier (search_hybrid.go) still surfaces those records, so this is a
// degradation rather than data loss — but the operator must be told, because a
// thin result set otherwise reads as "nothing was ever recorded here".
//
// The notice is strictly best-effort and bounded: any error, or a fully healthy
// index, prints nothing and never blocks or fails the read.

// coverageNoticeTimeout bounds the two diagnostic reads so a slow or wedged
// store can never stall a search behind its own health check.
const coverageNoticeTimeout = 2 * time.Second

// emitSemanticCoverageNotice prints a one-line stderr notice when part of the
// brain is missing from the semantic index. Silent when coverage is complete,
// when the counts cannot be read, or under GG_QUIET=1.
func emitSemanticCoverageNotice(parent context.Context, cmd *cobra.Command, c *store.Client) {
	if os.Getenv("GG_QUIET") == "1" || c == nil {
		return
	}

	ctx, cancel := context.WithTimeout(parent, coverageNoticeTimeout)
	defer cancel()

	degraded := 0
	if counts, err := c.DegradedVectorCounts(ctx); err == nil {
		for _, n := range counts {
			degraded += n
		}
	}

	pending := 0
	if ggDir := config.GGDirOrEmpty(); ggDir != "" {
		if entries, err := outbox.List(ggDir); err == nil {
			pending = len(entries)
		}
	}

	if degraded == 0 && pending == 0 {
		return
	}

	fmt.Fprintln(cmd.OutOrStderr(), semanticCoverageLine(pending, degraded))
}

// semanticCoverageLine renders the notice text. Split out so the wording is
// unit-checkable without a live store.
func semanticCoverageLine(pending, degraded int) string {
	msg := "semantic coverage incomplete:"
	switch {
	case pending > 0 && degraded > 0:
		msg += fmt.Sprintf(" %d record(s) queued unembedded, %d placeholder vector(s) excluded from semantic search", pending, degraded)
	case pending > 0:
		msg += fmt.Sprintf(" %d record(s) queued unembedded", pending)
	default:
		msg += fmt.Sprintf(" %d placeholder vector(s) excluded from semantic search", degraded)
	}
	return msg + " — the lexical tier still finds them; run `gg reembed` to restore full semantic recall"
}
