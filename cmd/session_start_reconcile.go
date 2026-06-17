package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/outbox"
)

// autoReconcileTimeout bounds the session-start outbox replay so a hung Qdrant
// can never hang the briefing. Mirrors the bounded-context discipline of the
// brain-graph self-heal (TASK-489) and brain auto-backup.
const autoReconcileTimeout = 3 * time.Second

// runAutoReconcileReplay is the replay seam exercised by reconcileOutboxIfNeeded.
// Production wires it to the same primitives as `gg doctor --reconcile`
// (replayBrainEntry + runReconcileFromJSONL); tests override it to assert the
// gating/opt-out/bounding logic without a live Qdrant. It returns the number of
// outbox entries successfully drained and any non-fatal error to surface.
var runAutoReconcileReplay = autoReconcileReplay

// reconcileOutboxIfNeeded auto-drains the crash-recovery outbox at session-start
// (TASK-505) so a vector write that failed while Qdrant was down replays itself
// on the next shell — no human ever has to run `gg doctor --reconcile`. It
// mirrors the TASK-489 brain-graph self-heal: bounded, non-fatal, and silent
// when there is nothing to do.
//
//   - empty outbox            → return silently (no output, no Qdrant probe)
//   - entries + Qdrant down    → leave them; no spam (gg status already surfaces
//     the pending count via the semantic-memory notice)
//   - entries + Qdrant up      → replay with autoReconcileTimeout; one-line
//     success note on stdout, short non-fatal warning on stderr on failure.
//
// Opt out with GG_NO_AUTO_RECONCILE=1 (or =true/on), matching the
// GG_AUTO_BACKUP=off opt-out style. Never blocks or fails session-start.
func reconcileOutboxIfNeeded(cmd *cobra.Command, stdout, stderr io.Writer) {
	if autoReconcileDisabled() {
		return
	}

	ggDir, err := config.GGDir()
	if err != nil {
		return // outside a project — nothing to reconcile
	}

	entries, err := outbox.List(ggDir)
	if err != nil {
		// Listing failed (e.g. permission) — surface it, don't swallow, but
		// never block the session.
		fmt.Fprintf(stderr, "[auto-reconcile] could not read outbox: %v\n", err)
		return
	}
	if len(entries) == 0 {
		return // silent — the common, zero-cost path
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	rctx, cancel := context.WithTimeout(ctx, autoReconcileTimeout)
	defer cancel()

	drained, replayErr := runAutoReconcileReplay(rctx, ggDir, len(entries))

	if rctx.Err() != nil {
		fmt.Fprintf(stderr, "[auto-reconcile] timeout after %s — %d pending write(s) remain; run `gg doctor --reconcile`\n",
			autoReconcileTimeout.Round(time.Second), len(entries))
		return
	}
	if replayErr != nil {
		// Qdrant unreachable is the expected steady state when offline — stay
		// quiet so we don't spam every shell; only warn on a genuine failure.
		if errors.Is(replayErr, errAutoReconcileStoreDown) {
			return
		}
		fmt.Fprintf(stderr, "[auto-reconcile] replay incomplete (%v) — run `gg doctor --reconcile`\n", replayErr)
		return
	}
	if drained > 0 {
		fmt.Fprintf(stdout, "✓ auto-reconciled %d pending write(s) to the vector store\n", drained)
	}
}

// autoReconcileDisabled reports whether GG_NO_AUTO_RECONCILE opts the session
// out of the auto-reconcile step.
func autoReconcileDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GG_NO_AUTO_RECONCILE")))
	switch v {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// errAutoReconcileStoreDown signals that Qdrant was unreachable, so the replay
// was deferred (not a failure). reconcileOutboxIfNeeded stays silent on it.
var errAutoReconcileStoreDown = errors.New("vector store unavailable — replay deferred")

// autoReconcileReplay drains the outbox using the same replay primitives as
// `gg doctor --reconcile` (replayBrainEntry), but quietly: it does not print the
// doctor report and returns a count instead. When Qdrant is unreachable it
// returns errAutoReconcileStoreDown so the caller can stay silent. The reconcile
// lock (BUG-073) is taken so an auto-replay can't clobber a concurrent manual
// `gg doctor --reconcile`.
func autoReconcileReplay(ctx context.Context, ggDir string, _ int) (int, error) {
	releaseLock, lockErr := brain.AcquireReconcileLock(ggDir)
	if lockErr != nil {
		if errors.Is(lockErr, brain.ErrReconcileLocked) {
			// A manual reconcile is already running — let it finish. Not a failure.
			return 0, errAutoReconcileStoreDown
		}
		return 0, fmt.Errorf("acquire reconcile lock: %w", lockErr)
	}
	defer releaseLock()

	d, dErr := loadDepsReadOnly(false)
	if dErr != nil {
		return 0, fmt.Errorf("load store client: %w", dErr)
	}
	defer d.Close()
	if d.qdrantDown || d.qdrantSlow {
		return 0, errAutoReconcileStoreDown
	}

	entries, listErr := outbox.List(ggDir)
	if listErr != nil {
		return 0, fmt.Errorf("read outbox: %w", listErr)
	}

	drained := 0
	var firstErr error
	for _, e := range entries {
		if ctx.Err() != nil {
			break // bounded — let the caller report the timeout
		}
		collSuffix, isBrainKind := brainKindToCollection[e.Kind]
		if !isBrainKind {
			// Index-repair kinds (full-index/changed-index) need `gg index`, not a
			// vector replay — leave them for `gg doctor --reconcile` to surface.
			continue
		}
		replayed, replayErr := replayBrainEntry(ctx, d.store, ggDir, e, collSuffix)
		switch {
		case replayErr != nil:
			_ = outbox.IncrementRetries(ggDir, e.ID)
			if firstErr == nil {
				firstErr = replayErr
			}
		case replayed:
			_ = outbox.Delete(ggDir, e.ID)
			drained++
		default:
			// Qdrant went down mid-drain — defer the rest.
			if firstErr == nil {
				firstErr = errAutoReconcileStoreDown
			}
		}
	}
	if drained == 0 && firstErr != nil {
		return 0, firstErr
	}
	// Surface a partial failure only when nothing drained; once we made progress
	// the success note is enough and the remainder waits for the next shell.
	if drained > 0 && errors.Is(firstErr, errAutoReconcileStoreDown) {
		firstErr = nil
	}
	return drained, firstErr
}
