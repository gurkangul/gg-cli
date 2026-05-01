package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
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

	// Load Qdrant client once — used by both outbox replay and JSONL scan.
	var storeClient *store.Client
	if d, dErr := loadDepsReadOnly(false); dErr == nil && !d.qdrantDown {
		storeClient = d.store
		defer d.Close()
	}

	needsAction := false

	// Phase 1: drain outbox entries.
	entries, err := outbox.List(ggDir)
	if err != nil {
		return fmt.Errorf("read outbox: %w", err)
	}

	if len(entries) > 0 {
		fmt.Printf("Found %d pending outbox entry(ies):\n\n", len(entries))
		for _, e := range entries {
			fmt.Printf("  ID:      %s\n", e.ID)
			fmt.Printf("  Kind:    %s\n", e.Kind)
			fmt.Printf("  Created: %s\n", e.CreatedAt)
			if e.Retries > 0 {
				fmt.Printf("  Retries: %d\n", e.Retries)
			}

			switch collSuffix, isBrainKind := brainKindToCollection[e.Kind]; {
			case isBrainKind:
				replayed, replayErr := replayBrainEntry(cmd.Context(), storeClient, ggDir, e, collSuffix)
				switch {
				case replayErr != nil:
					fmt.Printf("  → replay failed: %v\n", replayErr)
					_ = outbox.IncrementRetries(ggDir, e.ID)
					needsAction = true
				case replayed:
					fmt.Printf("  ✓ replayed to Qdrant (collection: %s)\n", collSuffix)
					_ = outbox.Delete(ggDir, e.ID)
				default:
					fmt.Printf("  ~ Qdrant unreachable — replay deferred\n")
					needsAction = true
				}
			default:
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
	} else {
		fmt.Println("Outbox empty.")
	}

	// Phase 2: scan JSONL ∖ Qdrant — recover entries written to JSONL but never
	// reaching Qdrant (SIGKILL window between Append and queueBrainOutbox).
	fmt.Println("\nJSONL ∖ Qdrant scan:")
	jsonlGap := runReconcileFromJSONL(cmd.Context(), storeClient, ggDir)
	if jsonlGap {
		needsAction = true
	}

	if needsAction {
		return fmt.Errorf("pending entries remain — fix issues above and re-run `gg doctor --reconcile`")
	}
	fmt.Println("\nAll stores consistent.")
	return nil
}

// runReconcileFromJSONL compares every brain JSONL file against Qdrant and
// re-upserts entries that are missing from Qdrant.  Returns true when any
// entries needed recovery or Qdrant was unreachable (caller should retry).
func runReconcileFromJSONL(ctx context.Context, sc *store.Client, ggDir string) bool {
	if sc == nil {
		fmt.Println("  ~ Qdrant unreachable — JSONL scan deferred")
		return true
	}
	anyGap := false
	for _, kind := range brainKinds {
		entries, _, readErr := brain.ReadAllWithCount(ggDir, kind)
		if readErr != nil {
			fmt.Printf("  ✗ %s: read error: %v\n", kind, readErr)
			anyGap = true
			continue
		}
		if len(entries) == 0 {
			continue
		}

		qdrantUUIDs, uuidErr := sc.CollectionUUIDs(ctx, kind)
		if uuidErr != nil {
			// Collection may not exist yet (fresh install) — treat as empty.
			qdrantUUIDs = map[string]struct{}{}
		}

		recovered := 0
		failed := 0
		invalidUUIDs := 0
		for _, e := range entries {
			if !validBrainUUID(e.UUID) {
				invalidUUIDs++
				continue
			}
			if _, exists := qdrantUUIDs[e.UUID]; exists {
				continue
			}
			// Entry is in JSONL but not in Qdrant — re-upsert.
			if replayErr := sc.ReplayBrainEntry(ctx, kind, e.UUID, e.Payload); replayErr != nil {
				if errors.Is(replayErr, store.ErrQdrantDown) {
					fmt.Printf("  ~ %s: Qdrant went down mid-scan — deferred\n", kind)
					anyGap = true
					break
				}
				fmt.Printf("  ✗ %s: replay %s failed: %v\n", kind, shortBrainUUID(e.UUID), replayErr)
				failed++
			} else {
				recovered++
			}
		}
		if recovered > 0 {
			fmt.Printf("  ✓ %s: recovered %d missing entry(ies)\n", kind, recovered)
		}
		if failed > 0 {
			fmt.Printf("  ✗ %s: %d replay failure(s)\n", kind, failed)
			anyGap = true
		}
		if invalidUUIDs > 0 {
			fmt.Printf("  ✗ %s: %d invalid UUID %s skipped — repair .gg/brain/%s.jsonl\n", kind, invalidUUIDs, reconcilePlural("entry", "entries", invalidUUIDs), kind)
			anyGap = true
		}
		if recovered == 0 && failed == 0 && invalidUUIDs == 0 && len(entries) > 0 {
			fmt.Printf("  ✓ %s: %d entries consistent\n", kind, len(entries))
		}
	}
	return anyGap
}

func reconcilePlural(one, many string, n int) string {
	if n == 1 {
		return one
	}
	return many
}

func validBrainUUID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	_, err := uuid.Parse(id)
	return err == nil
}

func shortBrainUUID(uuid string) string {
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return "(empty uuid)"
	}
	if len(uuid) <= 8 {
		return uuid
	}
	return uuid[:8]
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

// brainKinds lists the JSONL files checked by doctorCheckBrainJSONL.
var brainKinds = []string{"decisions", "rejections", "tasks", "bugs", "messages", "discussions", "notes"}

// doctorCheckBrainJSONL scans each .gg/brain/<kind>.jsonl for malformed lines
// and emits an advisory warning per file that has any.  A malformed line means
// the file was corrupted (torn write or manual edit) and those entries will be
// silently skipped by all readers.
func doctorCheckBrainJSONL(report *doctorReport) {
	ggDir, err := config.GGDir()
	if err != nil {
		report.warn("brain jsonl", "could not locate .gg dir")
		return
	}
	total := 0
	for _, kind := range brainKinds {
		_, skipped, scanErr := brain.ReadAllWithCount(ggDir, kind)
		if scanErr != nil {
			report.warn("brain jsonl/"+kind, fmt.Sprintf("scan error: %v", scanErr))
			continue
		}
		if skipped > 0 {
			total += skipped
			report.warn("brain jsonl/"+kind, fmt.Sprintf("⚠ %d malformed line(s) in .gg/brain/%s.jsonl — entries skipped", skipped, kind))
		}
	}
	if total == 0 {
		report.ok("brain jsonl", "all files clean")
	}
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
