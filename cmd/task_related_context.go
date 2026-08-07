package cmd

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/gurkangul/gg-cli/internal/store"
)

// Task-scoped related-context packet: the "=== Related Context ===" block of
// top-3 decisions, rejected approaches, and notes semantically near a task.
// Extracted from task_list.go (TASK-538) once it grew a second consumer — it is
// pulled by `gg task get --with-context` and pushed by `gg task start`, so it
// belongs to neither renderer.

// relatedContext holds the top-3 semantically related items for a task.
type relatedContext struct {
	decisions  []store.Decision
	rejections []store.Rejection
	notes      []store.Note
	// searchFailed is set when at least one of the three searches returned an
	// error. Without it an errored search is indistinguishable from an empty
	// brain: both leave the slices empty and would render "(no related items
	// found)". Telling an agent "nothing is recorded on this topic" when the
	// query actually failed is the one lie this block must never tell — it is
	// the exact outcome the packet exists to prevent.
	searchFailed bool
}

// items returns how many related records were actually rendered. Zero means the
// packet carried no memory, whether because the brain is empty or because the
// lookup degraded — use degraded() to tell those apart.
func (rc *relatedContext) items() int {
	if rc == nil {
		return 0
	}
	return len(rc.decisions) + len(rc.rejections) + len(rc.notes)
}

// degraded reports whether the lookup failed, as opposed to succeeding and
// finding nothing. A nil relatedContext means the embedding call failed; a set
// searchFailed means at least one store query did. Callers must not infer this
// from an empty item count: a project that has recorded nothing yet is healthy,
// and reporting it as a failure sends the owner to debug a working backend.
func (rc *relatedContext) degraded() bool {
	return rc == nil || rc.searchFailed
}

const withContextLimit uint64 = 3
const withContextMaxBytes = 3072 // ~800 tokens hard cap

// fetchRelatedContext embeds the task title+detail and searches the embedded vector store for top-3 items.
func fetchRelatedContext(d *deps, t *store.Task) *relatedContext {
	query := t.Title
	if t.Detail != "" {
		query = t.Title + " " + t.Detail
	}

	// Use a fresh timeout so we don't share the parent's deadline.
	ctx, cancel := withTimeout(context.Background())
	defer cancel()

	vector, err := d.embedder.Generate(ctx, query)
	if err != nil {
		return nil
	}

	rc := &relatedContext{}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		anyFail bool
	)
	markFail := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		anyFail = true
		mu.Unlock()
	}
	wg.Add(3)
	go func() {
		defer wg.Done()
		var err error
		rc.decisions, err = d.store.SearchDecisions(ctx, vector, withContextLimit, false)
		markFail(err)
	}()
	go func() {
		defer wg.Done()
		var err error
		rc.rejections, err = d.store.SearchRejections(ctx, vector, withContextLimit)
		markFail(err)
	}()
	go func() {
		defer wg.Done()
		var err error
		rc.notes, err = d.store.SearchNotes(ctx, vector, withContextLimit)
		markFail(err)
	}()
	wg.Wait()
	rc.searchFailed = anyFail
	return rc
}

// renderRelatedContext writes the === Related Context === block to w.
// Each item is a single line (ID/date + 1-sentence summary). The block is
// capped at withContextMaxBytes to enforce the ≤800-token hard cap.
// When rc is nil (vector store unavailable or embed failed), writes a brief notice.
func renderRelatedContext(w *bytes.Buffer, rc *relatedContext) {
	fmt.Fprintln(w, "\n=== Related Context ===")

	if rc == nil {
		fmt.Fprintln(w, "  (unavailable — vector store unavailable or embedding failed)")
		return
	}

	// An errored search and an empty brain both leave the slices empty, so the
	// failure has to be reported explicitly — "(no related items found)" on a
	// failed lookup reads as "nothing was ever decided here" and is precisely
	// the wrong thing to tell an agent about to make a decision.
	if rc.items() == 0 {
		if rc.searchFailed {
			fmt.Fprintln(w, "  (unavailable — related-item search failed; treat as unknown, not as empty)")
			return
		}
		fmt.Fprintln(w, "  (no related items found)")
		return
	}
	if rc.searchFailed {
		fmt.Fprintln(w, "  ⚠ partial — at least one search failed; items below are incomplete")
	}

	var lines []string
	for _, dec := range rc.decisions {
		lines = append(lines, compactDecisionLine(dec))
	}
	for _, r := range rc.rejections {
		lines = append(lines, compactRejectionLine(r))
	}
	for _, n := range rc.notes {
		lines = append(lines, compactNoteLine(n))
	}

	// Enforce byte cap — stop adding lines once we'd exceed the limit.
	// Header already written; budget from current buffer length.
	budget := withContextMaxBytes - w.Len()
	for _, line := range lines {
		entry := "  " + line + "\n"
		if budget-len(entry) < 0 {
			fmt.Fprintln(w, "  … (truncated — context block size limit reached)")
			break
		}
		fmt.Fprint(w, entry)
		budget -= len(entry)
	}
}
