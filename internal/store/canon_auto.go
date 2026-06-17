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
//     preferred and, in the full view, always included regardless of age —
//     important-old must never be summarized away. Only the routine tail is
//     capped.
//   - Low-signal records (bypass-rationale audit rows, operational release logs,
//     and near-duplicates) are filtered out, so a newcomer sees knowledge, not
//     noise.
//
// Two views exist: the full canon (gg canon show) and a tighter compact canon
// (injected at session-start, where a hard total cap keeps the briefing lean).
const (
	autoCanonDecisionCap = 12
	autoCanonRejectCap   = 8
	autoCanonFailureCap  = 10
	autoCanonTextWidth   = 240

	autoCanonCompactDecisions = 8
	autoCanonCompactRejects   = 5
	autoCanonCompactFailures  = 5
	autoCanonCompactWidth     = 140
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

type canonLimits struct {
	decisions int // routine cap (full) or hard total cap (compact)
	rejects   int
	failures  int
	width     int
	hardCap   bool // compact: cap importance+routine together at `decisions`
}

// BuildAutoCanon returns the full auto-derived canon (gg canon show): every
// important decision regardless of age, plus the most recent routine ones.
// tasks are used to compute reference-degree importance (TASK-475).
func BuildAutoCanon(decs []Decision, rejs []Rejection, bugs []Bug, tasks []Task) []CanonEntry {
	return buildAutoCanon(decs, rejs, bugs, tasks, canonLimits{
		decisions: autoCanonDecisionCap, rejects: autoCanonRejectCap,
		failures: autoCanonFailureCap, width: autoCanonTextWidth,
	})
}

// BuildAutoCanonCompact returns a tighter digest for session-start: a hard total
// cap on decisions (important first) and shorter lines, so the per-session
// briefing stays lean. Full depth remains available via `gg canon show`.
func BuildAutoCanonCompact(decs []Decision, rejs []Rejection, bugs []Bug, tasks []Task) []CanonEntry {
	return buildAutoCanon(decs, rejs, bugs, tasks, canonLimits{
		decisions: autoCanonCompactDecisions, rejects: autoCanonCompactRejects,
		failures: autoCanonCompactFailures, width: autoCanonCompactWidth, hardCap: true,
	})
}

// taskReferenceDegree counts how many records point at each task — decisions and
// bugs linked to it, plus tasks that depend on it. A decision attached to a
// heavily-referenced task is on a central, high-impact part of the project, so
// it is treated as important regardless of age (TASK-475: self-distilling canon
// — important-old surfaces without anyone manually pinning it).
func taskReferenceDegree(decs []Decision, bugs []Bug, tasks []Task) map[string]int {
	ref := map[string]int{}
	for _, d := range decs {
		if d.TaskID != "" {
			ref[d.TaskID]++
		}
	}
	for _, b := range bugs {
		if b.TaskID != "" {
			ref[b.TaskID]++
		}
	}
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			ref[dep]++
		}
	}
	return ref
}

func buildAutoCanon(decs []Decision, rejs []Rejection, bugs []Bug, tasks []Task, lim canonLimits) []CanonEntry {
	taskRef := taskReferenceDegree(decs, bugs, tasks)
	var out []CanonEntry
	if s := autoCanonDecisions(decs, taskRef, lim); s != "" {
		out = append(out, CanonEntry{Area: "key-decisions", Text: s, Author: "auto"})
	}
	if s := autoCanonRejections(rejs, lim); s != "" {
		out = append(out, CanonEntry{Area: "rejected-approaches", Text: s, Author: "auto"})
	}
	if s := autoCanonFailures(bugs, lim); s != "" {
		out = append(out, CanonEntry{Area: "failure-modes", Text: s, Author: "auto"})
	}
	return out
}

// autoCanonRefThreshold: a decision whose linked task is referenced by at least
// this many records is treated as important (auto-surfaced) even without a pin.
const autoCanonRefThreshold = 3

func autoCanonDecisions(decs []Decision, taskRef map[string]int, lim canonLimits) string {
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
		highlyReferenced := d.TaskID != "" && taskRef[d.TaskID] >= autoCanonRefThreshold
		if d.Pinned || hasImportantTag(d.Tags) || highlyReferenced {
			important = append(important, d)
		} else {
			routine = append(routine, d)
		}
	}
	sortByRecency(important)
	sortByRecency(routine)

	var chosen []Decision
	if lim.hardCap {
		// Compact: cap important + routine together, important prioritized.
		chosen = important
		if len(chosen) > lim.decisions {
			chosen = chosen[:lim.decisions]
		}
		for _, d := range routine {
			if len(chosen) >= lim.decisions {
				break
			}
			chosen = append(chosen, d)
		}
	} else {
		// Full: every important one, routine tail capped.
		if len(routine) > lim.decisions {
			routine = routine[:lim.decisions]
		}
		chosen = append(append([]Decision{}, important...), routine...)
	}

	var b strings.Builder
	for _, d := range chosen {
		writeDecisionBullet(&b, d, lim.width)
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeDecisionBullet(b *strings.Builder, d Decision, width int) {
	marker := "•"
	if d.Pinned {
		marker = "📌"
	}
	line := d.Text
	if d.Reason != "" {
		line += " — " + d.Reason
	}
	b.WriteString(marker + " " + truncateCanon(line, width) + "\n")
}

func autoCanonRejections(rejs []Rejection, lim canonLimits) string {
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
	if len(kept) > lim.rejects {
		kept = kept[:lim.rejects]
	}
	var b strings.Builder
	for _, r := range kept {
		line := r.Approach
		if r.Reason != "" {
			line += " — " + r.Reason
		}
		b.WriteString("✗ " + truncateCanon(line, lim.width) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func autoCanonFailures(bugs []Bug, lim canonLimits) string {
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
	if len(kept) > lim.failures {
		kept = kept[:lim.failures]
	}
	var b strings.Builder
	for _, bg := range kept {
		b.WriteString("✓ " + truncateCanon(bg.Title+" [root cause: "+bg.RootCause+"]", lim.width) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// isLowSignalDecision drops records that are not durable project knowledge:
// bypass-rationale audit rows written when a gate is overridden, and operational
// release logs ("Release vX shipped…"). These otherwise crowd out real knowledge.
func isLowSignalDecision(d Decision) bool {
	t := strings.ToLower(strings.TrimSpace(d.Text))
	switch {
	case strings.HasPrefix(t, "bypass rationale"), strings.HasPrefix(t, "bypass:"):
		return true
	case strings.HasPrefix(t, "release v") || strings.HasPrefix(t, "released v"):
		return true
	case strings.Contains(t, "shipped and synced"):
		return true
	}
	for _, tag := range d.Tags {
		if strings.EqualFold(strings.TrimSpace(tag), "bypass") {
			return true
		}
	}
	return false
}

// FilterDecisionNoise drops low-signal audit/operational records and collapses
// near-identical duplicates, preserving input order. Used to keep the project
// overview free of the cruft that otherwise dominates a newcomer's first screen
// (e.g. four identical bypass-rationale rows).
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

// NormalizeForDedup exposes the canon dedup key so callers outside the store
// (e.g. `gg canon suggest`) can judge "already reflected" with the same logic
// the auto-canon dedup uses.
func NormalizeForDedup(s string) string { return normalizeForDedup(s) }

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

func truncateCanon(s string, width int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return strings.TrimSpace(string(r[:width])) + "…"
}

func sortByRecency(d []Decision) {
	sort.SliceStable(d, func(i, j int) bool { return d[i].CreatedAt > d[j].CreatedAt })
}
