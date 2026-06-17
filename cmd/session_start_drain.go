package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// autoDrainTimeout bounds the session-start inbox auto-drain so a slow store can
// never hang the briefing. Mirrors the bounded-context discipline of the
// auto-reconcile (TASK-505) and brain-graph self-heal (TASK-489) steps.
const autoDrainTimeout = 5 * time.Second

// autoDrainCutoff is the staleness threshold for the routine firehose drain:
// audience=agents broadcasts older than this retire from the inbox. 720h = 30d,
// matching the `gg inbox archive --older-than` default so the manual and
// automatic paths agree.
const autoDrainCutoff = 720 * time.Hour

// runAutoDrainArchive is the archive seam exercised by autoDrainInboxIfNeeded.
// Production wires it to store.ArchiveAgentBroadcasts via loadDepsReadOnly; tests
// override it to assert the gating/opt-out/bounding logic without a live store.
// It returns the number of broadcasts archived and any non-fatal error.
var runAutoDrainArchive = autoDrainArchive

// autoDrainInboxIfNeeded routinely retires the agent firehose at session-start
// (BUG-091): audience=agents status broadcasts older than autoDrainCutoff (30d)
// are archived out of the default inbox so month-old "TASK-N done" pings stop
// bloating it, while recent ones stay and JSONL is preserved (forward-only).
//
// It mirrors the TASK-505 auto-reconcile self-heal: bounded, non-fatal, and
// silent unless it actually archives something. A drain failure (store down,
// timeout) NEVER blocks or fails session-start — the briefing is still useful.
//
// Opt out with GG_NO_AUTO_DRAIN=1 (or =true/on/yes), matching the
// GG_NO_AUTO_RECONCILE opt-out style.
func autoDrainInboxIfNeeded(cmd *cobra.Command, stdout, stderr io.Writer) {
	if autoDrainDisabled() {
		return
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	dctx, cancel := context.WithTimeout(ctx, autoDrainTimeout)
	defer cancel()

	before := time.Now().UTC().Add(-autoDrainCutoff)
	n, err := runAutoDrainArchive(dctx, before)

	if dctx.Err() != nil {
		// Timed out — leave the broadcasts for the next shell or a manual
		// `gg inbox archive`. Never fail the session.
		fmt.Fprintf(stderr, "[auto-drain] timeout after %s — skipping (run `gg inbox archive`)\n",
			autoDrainTimeout.Round(time.Second))
		return
	}
	if err != nil {
		// Store down / read error is the expected offline steady state — log a
		// short non-fatal note and continue. Never block the briefing.
		fmt.Fprintf(stderr, "[auto-drain] skipped (%v)\n", err)
		return
	}
	if n > 0 {
		fmt.Fprintf(stdout, "✓ archived %d stale agent-broadcast(s) (>30d)\n", n)
	}
}

// autoDrainDisabled reports whether GG_NO_AUTO_DRAIN opts the session out of the
// routine inbox auto-drain step.
func autoDrainDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GG_NO_AUTO_DRAIN"))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// autoDrainArchive archives stale audience=agents broadcasts via the now-batched
// store.ArchiveAgentBroadcasts. Read-only deps loader: when the store is down it
// returns an error the caller logs-and-continues on (never fatal at session-start).
func autoDrainArchive(ctx context.Context, before time.Time) (int, error) {
	d, err := loadDepsReadOnly(false)
	if err != nil {
		return 0, fmt.Errorf("load store client: %w", err)
	}
	defer d.Close()
	if d.qdrantDown || d.qdrantSlow {
		return 0, fmt.Errorf("vector store unavailable — drain deferred")
	}
	return d.store.ArchiveAgentBroadcasts(ctx, before)
}
