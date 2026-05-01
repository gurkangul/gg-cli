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

// detectConflicts cross-references all narrative text in the bundle against
// canonical task/bug statuses in the same bundle.
func detectConflicts(bundle contextBundle) []contextConflict {
	// Build lookup maps for IDs present in this bundle.
	taskStatus := make(map[string]string, len(bundle.tasks))
	for _, t := range bundle.tasks {
		taskStatus[t.ID] = t.Status
	}
	bugStatus := make(map[string]string) // bugs not directly in contextBundle; only tasks are
	_ = bugStatus                        // reserved for future extension when bugs join the bundle

	type source struct {
		kind string
		text string
	}

	// Collect all narrative text from decisions, notes, and rejections.
	var sources []source
	for _, d := range bundle.decisions {
		sources = append(sources, source{"decision", d.Text + " " + d.Reason})
	}
	for _, n := range bundle.notes {
		sources = append(sources, source{"note", n.Text})
	}
	for _, r := range bundle.rejections {
		sources = append(sources, source{"rejection", r.Approach + " " + r.Reason})
	}

	// Deduplicate conflicts by ID so the same ID mentioned multiple times
	// across sources emits only one conflict.
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
			// Only flag IDs present in this bundle (avoids false positives from
			// historical references to items in other projects or already done).
			if status, ok := taskStatus[id]; ok {
				if nonTerminalTaskStatus(status) {
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
	}

	return conflicts
}
