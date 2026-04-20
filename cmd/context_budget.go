package cmd

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
)

// contextTieredItem is a single renderable item with a priority tier.
// Tier 1 (P1) = decisions + in-progress tasks — always emit first.
// Tier 2 (P2) = pending tasks + open discussions + rejections.
// Tier 3 (P3) = done/blocked tasks + resolved discussions.
// Tier 4 (P4) = notes + dismissed discussions.
type contextTieredItem struct {
	tier int
	line string // pre-rendered compact line
}

// approxTokens estimates the token cost of a string (4 chars ≈ 1 token).
func approxTokens(s string) int {
	n := len(s) / 4
	if n == 0 {
		n = 1
	}
	return n
}

// buildTieredItems converts a bundle into a flat, tier-annotated item list.
func buildTieredItems(bundle contextBundle) []contextTieredItem {
	var items []contextTieredItem

	for _, dec := range bundle.decisions {
		tier := 1
		if dec.Status == "superseded" {
			tier = 3
		}
		suffix := ""
		if dec.TaskID != "" {
			suffix = " →" + dec.TaskID
		}
		line := fmt.Sprintf("[P%d] D  %s  %s%s", tier,
			shortDate(dec.CreatedAt), compactTrim(dec.Text, compactLineWidth), suffix)
		items = append(items, contextTieredItem{tier: tier, line: line})
	}

	for _, r := range bundle.rejections {
		suffix := ""
		if r.TaskID != "" {
			suffix = " →" + r.TaskID
		}
		line := fmt.Sprintf("[P2] R  %s  %s%s",
			shortDate(r.CreatedAt), compactTrim(r.Approach, compactLineWidth), suffix)
		items = append(items, contextTieredItem{tier: 2, line: line})
	}

	for _, t := range bundle.tasks {
		tier := 2
		switch t.Status {
		case "in_progress":
			tier = 1
		case "done", "blocked", "ready_for_live":
			tier = 3
		}
		line := fmt.Sprintf("[P%d] T %s %s  %s (%s)", tier,
			taskStatusIcon(t.Status), t.ID, compactTrim(t.Title, compactLineWidth), t.Priority)
		items = append(items, contextTieredItem{tier: tier, line: line})
	}

	for _, disc := range bundle.discussions {
		tier := 2
		switch disc.Status {
		case "resolved":
			tier = 3
		case "dismissed":
			tier = 4
		}
		suffix := ""
		if n := len(disc.Turns); n > 0 {
			suffix = fmt.Sprintf(" (%d turns)", n)
		}
		line := fmt.Sprintf("[P%d] ? %s %s  %s%s", tier,
			discStatusMark(disc.Status), disc.ID, compactTrim(disc.Topic, compactLineWidth), suffix)
		items = append(items, contextTieredItem{tier: tier, line: line})
	}

	for _, n := range bundle.notes {
		taskRef := ""
		if n.TaskID != "" {
			taskRef = "  (" + n.TaskID + ")"
		}
		line := fmt.Sprintf("[P4] N  %s%s  %s",
			shortDate(n.CreatedAt), taskRef, compactTrim(n.Text, compactLineWidth))
		items = append(items, contextTieredItem{tier: 4, line: line})
	}

	// Stable sort by tier so P1 items always come first.
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].tier < items[j].tier
	})
	return items
}

// renderContextBudget renders a token-budget-aware compact view.
// Items are sorted P1→P4 and emitted until the budget (in tokens) is exhausted.
// Lower-priority tiers are dropped when the budget would be exceeded.
func renderContextBudget(w io.Writer, query string, bundle contextBundle, errs []string, budget int) {
	items := buildTieredItems(bundle)

	remaining := budget
	var kept []contextTieredItem
	var dropped int
	for _, item := range items {
		cost := approxTokens(item.line)
		if remaining <= 0 {
			dropped++
			continue
		}
		if cost > remaining {
			dropped++
			continue
		}
		kept = append(kept, item)
		remaining -= cost
	}

	total := len(bundle.decisions) + len(bundle.rejections) + len(bundle.tasks) + len(bundle.discussions) + len(bundle.notes)
	header := fmt.Sprintf("context: %q — %d items, budget %d tok", query, total, budget)
	if dropped > 0 {
		header += fmt.Sprintf(" (%d dropped)", dropped)
	}
	fmt.Fprintln(w, header)
	fmt.Fprintln(w)

	for _, item := range kept {
		fmt.Fprintln(w, item.line)
	}

	if len(errs) > 0 {
		fmt.Fprintf(w, "\n! %s\n", strings.Join(errs, "; "))
	}
}

// tierBudgetSummary returns how many tokens each tier consumes in a bundle.
// Used in unit tests to verify tier boundary behaviour.
func tierBudgetSummary(bundle contextBundle) map[int]int {
	items := buildTieredItems(bundle)
	summary := make(map[int]int)
	for _, item := range items {
		summary[item.tier] += approxTokens(item.line)
	}
	return summary
}

// renderBudgetToString renders --budget output to a string (test helper).
func renderBudgetToString(query string, bundle contextBundle, budget int) string {
	var buf bytes.Buffer
	renderContextBudget(&buf, query, bundle, nil, budget)
	return buf.String()
}
