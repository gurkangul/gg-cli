package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/outbox"
)

// runDoctorReconcile scans the outbox for incomplete dual-store writes and
// reports what needs to be re-run to restore consistency.
//
// For index operations the repair is: re-run `gg index` (the idempotent
// UpsertNode/UpsertEdge writes make this safe). The user is shown the exact
// command rather than running it automatically, since indexing may take minutes.
func runDoctorReconcile(_ *cobra.Command) error {
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
		fmt.Println("Outbox is empty — index pipeline is consistent.")
		fmt.Println("Note: gg decide/task/note/etc write to a single Qdrant collection")
		fmt.Println("and are not tracked here (single-store, no outbox needed).")
		return nil
	}

	fmt.Printf("Found %d pending outbox entry(ies):\n\n", len(entries))
	needsAction := false
	for _, e := range entries {
		fmt.Printf("  ID:      %s\n", e.ID)
		fmt.Printf("  Kind:    %s\n", e.Kind)
		fmt.Printf("  Created: %s\n", e.CreatedAt)
		if e.Retries > 0 {
			fmt.Printf("  Retries: %d\n", e.Retries)
		}

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
		fmt.Println()
	}

	if needsAction {
		return fmt.Errorf("%d pending outbox entry(ies) — run the repair commands shown above, then re-run `gg doctor --reconcile`", len(entries))
	}
	return nil
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
