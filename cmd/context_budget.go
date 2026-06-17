package cmd

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/gurkangul/gg-cli/internal/telemetry"
)

// contextTieredItem is a single renderable item with a priority tier.
// Tier 1 (P1) = decisions + in-progress tasks — always emit first.
// Tier 2 (P2) = pending tasks + open discussions + rejections.
// Tier 3 (P3) = done/blocked tasks + resolved discussions.
// Tier 4 (P4) = notes + dismissed discussions.
//
// TASK-502: each item also carries a topic-relevance score (set when a
// <topic> is supplied) used to order items *within* a tier, and a force-inject
// flag for critical/architecture items that --budget must never drop.
type contextTieredItem struct {
	tier        int
	score       int    // topic-relevance score (higher = more relevant)
	forceInject bool   // critical/architecture — never dropped by --budget
	line        string // pre-rendered compact line
}

// approxTokens estimates the token cost of a string using BytesPerToken.
func approxTokens(s string) int {
	n := len(s) / telemetry.BytesPerToken
	if n == 0 {
		n = 1
	}
	return n
}

// buildTieredItems converts a bundle into a flat, tier-annotated item list.
// query (TASK-502) drives per-entry topic-relevance scoring used to order items
// within each tier; pass "" for the project-onboarding bundle (no topic), which
// scores purely on type/priority/recency and leaves tier ordering stable.
func buildTieredItems(query string, bundle contextBundle) []contextTieredItem {
	rc := newRelevanceContext(query)
	var items []contextTieredItem

	for _, dec := range bundle.decisions {
		tier := 1
		if dec.Status == "superseded" {
			tier = 3
		}
		score, _ := rc.scoreDecision(dec)
		line := fmt.Sprintf("[P%d] %s", tier, compactDecisionLine(dec))
		items = append(items, contextTieredItem{tier: tier, score: score, forceInject: hasForceInjectTag(dec.Tags), line: line})
	}

	for _, r := range bundle.rejections {
		score, _ := rc.scoreRejection(r)
		line := fmt.Sprintf("[P2] %s", compactRejectionLine(r))
		items = append(items, contextTieredItem{tier: 2, score: score, forceInject: hasForceInjectTag(r.Tags), line: line})
	}

	for _, t := range bundle.tasks {
		tier := 2
		switch t.Status {
		case "in_progress":
			tier = 1
		case "done", "blocked", "ready_for_live":
			tier = 3
		}
		score, _ := rc.scoreTask(t)
		fi := hasForceInjectTag(t.Tags) || strings.EqualFold(t.Priority, "critical")
		line := fmt.Sprintf("[P%d] %s", tier, compactTaskLine(t))
		items = append(items, contextTieredItem{tier: tier, score: score, forceInject: fi, line: line})
	}

	for _, disc := range bundle.discussions {
		tier := 2
		switch disc.Status {
		case "resolved":
			tier = 3
		case "dismissed":
			tier = 4
		}
		score, _ := rc.scoreDiscussion(disc)
		line := fmt.Sprintf("[P%d] %s", tier, compactDiscussionLine(disc))
		items = append(items, contextTieredItem{tier: tier, score: score, forceInject: hasForceInjectTag(disc.Tags), line: line})
	}

	for _, n := range bundle.notes {
		score, _ := rc.scoreNote(n)
		line := fmt.Sprintf("[P4] %s", compactNoteLine(n))
		items = append(items, contextTieredItem{tier: 4, score: score, forceInject: hasForceInjectTag(n.Tags), line: line})
	}

	for _, a := range bundle.artifacts {
		line := fmt.Sprintf("[P2] %s", compactArtifactLine(a))
		items = append(items, contextTieredItem{tier: 2, line: line})
	}

	// Stable sort by tier (P1 first), then by relevance score within a tier
	// (most topic-relevant + highest priority first). SliceStable keeps the
	// original collection order as the final deterministic tiebreaker.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].tier != items[j].tier {
			return items[i].tier < items[j].tier
		}
		return items[i].score > items[j].score
	})
	return items
}

// renderContextBudget renders a token-budget-aware compact view.
// Items are sorted P1→P4 and emitted until the budget (in tokens) is exhausted.
// Lower-priority tiers are dropped when the budget would be exceeded.
func renderContextBudget(w io.Writer, query string, bundle contextBundle, errs []string, budget int) {
	items := buildTieredItems(query, bundle)

	// Walk items in tier+score order. Force-injected items (critical/architecture)
	// are kept in place regardless of the budget — a tight budget must never drop
	// them (TASK-502 AC). They still draw down the remaining budget so it stays
	// honest, but are themselves never counted as dropped.
	remaining := budget
	var kept []contextTieredItem
	var dropped int
	for _, item := range items {
		cost := approxTokens(item.line)
		if item.forceInject {
			kept = append(kept, item)
			remaining -= cost
			continue
		}
		if remaining <= 0 || cost > remaining {
			dropped++
			continue
		}
		kept = append(kept, item)
		remaining -= cost
	}

	total := len(bundle.decisions) + len(bundle.rejections) + len(bundle.tasks) + len(bundle.discussions) + len(bundle.notes) + len(bundle.artifacts)
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
	items := buildTieredItems("", bundle)
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
