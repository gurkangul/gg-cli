package cmd

import (
	"regexp"
	"strings"
)

// contextConflict records a single narrative-vs-canonical state mismatch.
type contextConflict struct {
	ID            string `json:"id"`            // TASK-NNN or BUG-NNN
	SourceType    string `json:"source_type"`   // "decision" | "note" | "rejection"
	ClosureToken  string `json:"closure_token"` // the token that triggered the conflict
	LiveStatus    string `json:"live_status"`
	Authoritative string `json:"authoritative"` // always "live_status"
}

// closureTokens are words that imply an item is in a terminal/resolved state.
var closureTokens = []string{"done", "fixed", "closed", "shipped", "bypass", "approved", "reject"}

// idExtractor matches TASK-NNN and BUG-NNN references in prose.
var idExtractor = regexp.MustCompile(`\b(TASK|BUG)-\d+\b`)

// nonTerminalTaskStatus returns true when a task status is not terminal.
func nonTerminalTaskStatus(status string) bool {
	switch status {
	case "pending", "in_progress", "blocked", "ready_for_live":
		return true
	}
	return false
}

// hasClosureToken reports whether text contains any closure token.
// Returns the first matching token or "".
func hasClosureToken(text string) string {
	lower := strings.ToLower(text)
	for _, tok := range closureTokens {
		if strings.Contains(lower, tok) {
			return tok
		}
	}
	return ""
}

// conflictSource is one narrative blob to scan for closure-token-vs-live drift.
// kind is the source label that lands in contextConflict.SourceType (e.g.
// "decision", "note", "canon").
type conflictSource struct {
	kind string
	text string
}

// detectTextConflicts is the shared drift detector (TASK-499): it scans each
// narrative source for a closure token plus a TASK/BUG reference, then flags a
// conflict when the referenced ID is a non-terminal live task in taskStatus.
// Both `gg context` (via detectConflicts) and `gg canon show` reuse this so the
// canon and the ledger can never tell divergent done/pending stories.
//
// Conflicts are deduplicated by ID so the same ID mentioned across multiple
// sources emits a single conflict.
func detectTextConflicts(sources []conflictSource, taskStatus map[string]string) []contextConflict {
	seen := make(map[string]bool)
	var conflicts []contextConflict

	for _, src := range sources {
		tok := hasClosureToken(src.text)
		if tok == "" {
			continue
		}
		ids := idExtractor.FindAllString(src.text, -1)
		for _, id := range ids {
			if seen[id] {
				continue
			}
			// Only flag IDs whose live status is known (avoids false positives
			// from historical references to items in other projects or already
			// done) and still non-terminal.
			if status, ok := taskStatus[id]; ok && nonTerminalTaskStatus(status) {
				seen[id] = true
				conflicts = append(conflicts, contextConflict{
					ID:            id,
					SourceType:    src.kind,
					ClosureToken:  tok,
					LiveStatus:    status,
					Authoritative: "live_status",
				})
			}
		}
	}

	return conflicts
}

// detectConflicts cross-references all narrative text in the bundle against
// canonical task/bug statuses in the same bundle. Thin wrapper over the shared
// detectTextConflicts so the canon path and the context path stay in lockstep.
func detectConflicts(bundle contextBundle) []contextConflict {
	// Build lookup map for task IDs present in this bundle. (Bugs aren't in
	// contextBundle yet — reserved for future extension when they join.)
	taskStatus := make(map[string]string, len(bundle.tasks))
	for _, t := range bundle.tasks {
		taskStatus[t.ID] = t.Status
	}

	// Collect all narrative text from decisions, notes, and rejections.
	var sources []conflictSource
	for _, d := range bundle.decisions {
		sources = append(sources, conflictSource{"decision", d.Text + " " + d.Reason})
	}
	for _, n := range bundle.notes {
		sources = append(sources, conflictSource{"note", n.Text})
	}
	for _, r := range bundle.rejections {
		sources = append(sources, conflictSource{"rejection", r.Approach + " " + r.Reason})
	}

	return detectTextConflicts(sources, taskStatus)
}
