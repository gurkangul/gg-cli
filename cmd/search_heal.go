package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
)

// search_heal.go — TASK-521 opportunistic repair of semantic coverage on read.
//
// TASK-516 made partial coverage VISIBLE: reads warn when records are queued
// unembedded or carry degraded placeholder vectors, and point at `gg reembed`.
// That remedy assumes a human. A headless agent sees the warning, has nobody to
// run the command, and the project stays degraded indefinitely — the lexical
// tier keeps those records findable, so nothing ever forces the issue.
//
// So the repair happens here instead, under three rules that keep it safe:
//
//   - AFTER the answer. Results are already on stdout before this runs, so a
//     slow or wedged embedder can never delay a read.
//   - BOUNDED. A small batch and a hard timeout per invocation; coverage
//     converges across several reads instead of one long stall.
//   - SINGLE-FLIGHT. Parallel agents share the reconcile lock, so a busy project
//     does not stampede the embedder with duplicate work.
//
// Never fails the command: every error path is silent or a one-line note.

const (
	// healOnReadBatch is how many degraded points one read may repair. Small on
	// purpose — this is a background courtesy, not a migration.
	healOnReadBatch = 8
	// healOnReadTimeout hard-caps the repair pass.
	healOnReadTimeout = 5 * time.Second
)

// healSemanticCoverageOnRead repairs a bounded batch of degraded vectors after a
// read has already produced its output. Fully best-effort and silent unless it
// actually fixed something.
//
// Disabled with GG_NO_HEAL=1 for callers that want reads to touch nothing.
func healSemanticCoverageOnRead(cmd *cobra.Command, d *deps) {
	if d == nil || d.store == nil || d.embedder == nil {
		return
	}
	if d.qdrantDown || d.qdrantSlow {
		return
	}
	if os.Getenv("GG_NO_HEAL") == "1" {
		return
	}

	ggDir := config.GGDirOrEmpty()
	if ggDir == "" {
		return
	}

	// Single-flight across concurrent agents: if another process is already
	// reconciling or healing, skip rather than duplicate the work.
	release, lockErr := brain.AcquireReconcileLock(ggDir)
	if lockErr != nil {
		return
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), healOnReadTimeout)
	defer cancel()

	healed, err := d.store.HealDegradedVectors(ctx, d.embedder, healOnReadBatch)
	if err != nil || healed == 0 {
		return
	}

	noun := "records"
	if healed == 1 {
		noun = "record"
	}
	fmt.Fprintf(cmd.OutOrStderr(),
		"semantic coverage: re-embedded %d %s in the background; run `gg reembed` to finish the rest in one pass\n",
		healed, noun)
}
