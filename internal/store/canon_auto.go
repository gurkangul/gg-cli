package store

import (
	"sort"
	"strings"
)

// Auto-canon turns the raw ledger into a bounded, deduplicated "what every dev
// must know" digest with NO manual curation. gg has no cloud LLM (no-network),
// so "summarization" here is deterministic selection + dedup + ranking, computed
// fresh on every read — never a stored blob that goes stale or bloats.
//
// Two invariants make this safe as institutional memory:
//   - Important decisions (pinned, or tagged architecture/constraint/etc.) are
//     ALWAYS included regardless of age — important-old must never be summarized
//     away. Only the routine tail is capped.
//   - Low-signal audit cruft (bypass-rationale records and the like) is filtered
//     out, so a newcomer sees knowledge, not noise.
const (
	autoCanonDecisionCap = 12
	autoCanonRejectCap   = 8
	autoCanonFailureCap  = 10
	autoCanonTextWidth   = 240
)

// importantDecisionTags mark durable architecture/constraint knowledge that must
// always surface in the canon regardless of age.
var importantDecisionTags = map[string]bool{
	"architecture": true,
	"constraint":   true,
	"invariant":    true,
	"canon":        true,
	"security":     true,
}

// BuildAutoCanon distills decisions, rejections and fixed-bug root causes into
// the auto-derived canon. Pure and deterministic so it is unit-testable and
// produces identical output for identical input.
func BuildAutoCanon(decs []Decision, rejs []Rejection, bugs []Bug) []CanonEntry {
	var out []CanonEntry
	if s := autoCanonDecisions(decs); s != "" {
		out = append(out, CanonEntry{Area: "key-decisions", Text: s, Author: "auto"})
	}
	if s := autoCanonRejections(rejs); s != "" {
		out = append(out, CanonEntry{Area: "rejected-approaches", Text: s, Author: "auto"})
	}
	if s := autoCanonFailures(bugs); s != "" {
		out = append(out, CanonEntry{Area: "failure-modes", Text: s, Author: "auto"})
	}
	return out
}

func autoCanonDecisions(decs []Decision) string {
	seen := map[string]bool{}
	var important, routine []Decision
	for _, d := range decs {
		if d.Status != "" && d.Status != "active" {
			continue
		}
		if isLowSignalDecision(d) {
			continue
		}
		key := normalizeForDedup(d.Text)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if d.Pinned || hasImportantTag(d.Tags) {
			important = append(important, d)
		} else {
			routine = append(routine, d)
		}
	}
	sortByRecency(important)
	sortByRecency(routine)
	if len(routine) > autoCanonDecisionCap {
		routine = routine[:autoCanonDecisionCap]
	}
	var b strings.Builder
	for _, d := range important {
		writeDecisionBullet(&b, d)
	}
	for _, d := range routine {
		writeDecisionBullet(&b, d)
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeDecisionBullet(b *strings.Builder, d Decision) {
	marker := "•"
	if d.Pinned {
		marker = "📌"
	}
	line := d.Text
	if d.Reason != "" {
		line += " — " + d.Reason
	}
	b.WriteString(marker + " " + truncateCanon(line) + "\n")
}

func autoCanonRejections(rejs []Rejection) string {
	seen := map[string]bool{}
	var kept []Rejection
	for _, r := range rejs {
		key := normalizeForDedup(r.Approach)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, r)
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].CreatedAt > kept[j].CreatedAt })
	if len(kept) > autoCanonRejectCap {
		kept = kept[:autoCanonRejectCap]
	}
	var b strings.Builder
	for _, r := range kept {
		line := r.Approach
		if r.Reason != "" {
			line += " — " + r.Reason
		}
		b.WriteString("✗ " + truncateCanon(line) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func autoCanonFailures(bugs []Bug) string {
	seen := map[string]bool{}
	var kept []Bug
	for _, bg := range bugs {
		if strings.TrimSpace(bg.RootCause) == "" {
			continue
		}
		key := normalizeForDedup(bg.RootCause)
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, bg)
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].CreatedAt > kept[j].CreatedAt })
	if len(kept) > autoCanonFailureCap {
		kept = kept[:autoCanonFailureCap]
	}
	var b strings.Builder
	for _, bg := range kept {
		b.WriteString("✓ " + truncateCanon(bg.Title+" [root cause: "+bg.RootCause+"]") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// isLowSignalDecision drops audit/bookkeeping records that are not durable
// project knowledge — chiefly the bypass-rationale decisions written when a gate
// is overridden, which otherwise dominate a newcomer's first view.
func isLowSignalDecision(d Decision) bool {
	t := strings.ToLower(strings.TrimSpace(d.Text))
	if strings.HasPrefix(t, "bypass rationale") || strings.HasPrefix(t, "bypass:") {
		return true
	}
	for _, tag := range d.Tags {
		if strings.EqualFold(strings.TrimSpace(tag), "bypass") {
			return true
		}
	}
	return false
}

// FilterDecisionNoise drops low-signal audit records (bypass rationales) and
// collapses near-identical duplicates, preserving input order. Used to keep the
// project overview free of the cruft that otherwise dominates a newcomer's first
// screen (e.g. four identical bypass-rationale rows).
func FilterDecisionNoise(decs []Decision) []Decision {
	seen := map[string]bool{}
	out := make([]Decision, 0, len(decs))
	for _, d := range decs {
		if isLowSignalDecision(d) {
			continue
		}
		key := normalizeForDedup(d.Text)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}

func hasImportantTag(tags []string) bool {
	for _, t := range tags {
		if importantDecisionTags[strings.ToLower(strings.TrimSpace(t))] {
			return true
		}
	}
	return false
}

// normalizeForDedup collapses a record's text to a stable key so near-identical
// duplicates (e.g. the four identical bypass-rationale rows) fold to one.
func normalizeForDedup(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	r := []rune(s)
	if len(r) > 80 {
		r = r[:80]
	}
	return string(r)
}

func truncateCanon(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= autoCanonTextWidth {
		return s
	}
	return strings.TrimSpace(string(r[:autoCanonTextWidth])) + "…"
}

func sortByRecency(d []Decision) {
	sort.SliceStable(d, func(i, j int) bool { return d[i].CreatedAt > d[j].CreatedAt })
}
