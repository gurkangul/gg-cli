package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/outbox"
	"github.com/gurkangul/gg-cli/internal/store"
)

// brainKindToCollection maps an outbox replay kind to the Qdrant collection suffix.
var brainKindToCollection = map[string]string{
	store.OutboxKindDecision:  "decisions",
	store.OutboxKindRejection: "rejections",
	store.OutboxKindTask:      "tasks",
	store.OutboxKindBug:       "bugs",
}

// runDoctorReconcile scans the outbox for incomplete dual-store writes and
// replays brain entries to Qdrant when possible.
//
// For brain replay kinds (AC-3): reads .gg/brain/<kind>.jsonl, finds the entry
// by UUID, and re-upserts to Qdrant. Idempotent on retry — entry stays in outbox
// on failure and is removed on success.
//
// For index operations: shows the repair command (unchanged behaviour).
func runDoctorReconcile(cmd *cobra.Command) error {
	fmt.Println("GG Doctor — Reconcile")
	fmt.Println(strings.Repeat("─", 50))

	ggDir, err := config.GGDir()
	if err != nil {
		return fmt.Errorf("find .gg dir: %w", err)
	}

	entries, err := outbox.List(ggDir)
	if err != nil {
		return fmt.Errorf("read outbox: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("Outbox is empty — stores consistent.")
		return nil
	}

	fmt.Printf("Found %d pending outbox entry(ies):\n\n", len(entries))

	// Load Qdrant client for brain replay — if unavailable, report but don't abort.
	var storeClient *store.Client
	if d, dErr := loadDepsReadOnly(false); dErr == nil && !d.qdrantDown {
		storeClient = d.store
		defer d.Close()
	}

	needsAction := false
	for _, e := range entries {
		fmt.Printf("  ID:      %s\n", e.ID)
		fmt.Printf("  Kind:    %s\n", e.Kind)
		fmt.Printf("  Created: %s\n", e.CreatedAt)
		if e.Retries > 0 {
			fmt.Printf("  Retries: %d\n", e.Retries)
		}

		if collSuffix, isBrainKind := brainKindToCollection[e.Kind]; isBrainKind {
			// AC-3: replay brain entry from JSONL to Qdrant.
			replayed, replayErr := replayBrainEntry(cmd.Context(), storeClient, ggDir, e, collSuffix)
			if replayErr != nil {
				fmt.Printf("  → replay failed: %v\n", replayErr)
				_ = outbox.IncrementRetries(ggDir, e.ID)
				needsAction = true
			} else if replayed {
				fmt.Printf("  ✓ replayed to Qdrant (collection: %s)\n", collSuffix)
				_ = outbox.Delete(ggDir, e.ID)
			} else {
				fmt.Printf("  ~ Qdrant unreachable — replay deferred\n")
				needsAction = true
			}
		} else {
			switch e.Kind {
			case "full-index", "changed-index":
				var p struct {
					Root string `json:"root"`
					Lang string `json:"lang"`
					SHA  string `json:"sha"`
				}
				if jsonErr := json.Unmarshal(e.Payload, &p); jsonErr == nil {
					shortSHA := p.SHA
					if len(shortSHA) > 8 {
						shortSHA = shortSHA[:8]
					}
					fmt.Printf("  → Memgraph write for sha=%s may be incomplete.\n", shortSHA)
					fmt.Printf("    Repair: gg index --lang %s\n", p.Lang)
				} else {
					fmt.Printf("  → Payload unreadable: %v\n", jsonErr)
				}
				needsAction = true
			default:
				fmt.Printf("  → Unknown kind %q — manual inspection required.\n", e.Kind)
				needsAction = true
			}
		}
		fmt.Println()
	}

	if needsAction {
		return fmt.Errorf("pending outbox entries remain — fix issues above and re-run `gg doctor --reconcile`")
	}
	fmt.Println("All entries replayed — outbox is now empty.")
	return nil
}

// replayBrainEntry reads the JSONL entry for e.UUID from .gg/brain/<collSuffix>.jsonl
// and upserts it to Qdrant without a vector (scroll-only collections accept
// zero-vector upserts for payload restoration; semantic search remains degraded
// until a full reindex runs).  Returns (true, nil) on success, (false, nil) when
// Qdrant is unreachable, and (false, err) on a hard error.
func replayBrainEntry(ctx context.Context, sc *store.Client, ggDir string, e outbox.Entry, collSuffix string) (bool, error) {
	if sc == nil {
		return false, nil // Qdrant unreachable
	}

	p, ok := parseBrainOutboxPayload(e.Payload)
	if !ok {
		return false, fmt.Errorf("malformed outbox payload: %s", e.Payload)
	}

	entries, err := brain.ReadAll(ggDir, collSuffix)
	if err != nil {
		return false, fmt.Errorf("read brain jsonl: %w", err)
	}

	var found *brain.Entry
	for i := range entries {
		if entries[i].UUID == p.UUID {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		// Entry not in JSONL — shouldn't happen, but treat as already-replayed.
		return true, nil
	}

	// Replay via a no-vector upsert — the payload is restored in Qdrant but
	// semantic search will return low scores until a `gg brain reindex` runs.
	if replayErr := sc.ReplayBrainEntry(ctx, collSuffix, found.UUID, found.Payload); replayErr != nil {
		if errors.Is(replayErr, store.ErrQdrantDown) {
			return false, nil
		}
		return false, replayErr
	}
	return true, nil
}

// doctorCheckOutbox reports whether there are unresolved outbox entries.
func doctorCheckOutbox(report *doctorReport) {
	ggDir, err := config.GGDir()
	if err != nil {
		report.warn("outbox", "could not locate .gg dir")
		return
	}
	entries, err := outbox.List(ggDir)
	if err != nil {
		report.warn("outbox", fmt.Sprintf("read failed: %v", err))
		return
	}
	if len(entries) == 0 {
		report.ok("outbox", "empty — stores consistent")
		return
	}
	report.fail("outbox", fmt.Sprintf("%d pending entry(ies) — run `gg doctor --reconcile`", len(entries)))
}
