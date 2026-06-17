package cmd

import (
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/store"
)

// TASK-502: per-entry topic-relevance scoring for `gg context <topic>`.
//
// Ported from WrongStack's scoreRelevant() (memory-store.ts): each candidate
// entry is scored deterministically against the query by word/tag overlap plus
// boosts for priority, type, and recency. The score refines the ordering
// *within* the existing P1-P4 tiers (it does not replace the tiers). Items
// tagged critical/architecture are force-injected so a tight --budget can never
// drop them. No LLM, no network — pure string/field math.

// relevanceContext holds query-derived signals reused across every entry.
type relevanceContext struct {
	words []string  // query tokens, lowercased, len>2
	now   time.Time // reference clock for recency (injectable for tests)
}

// newRelevanceContext tokenizes the query once for reuse across entries.
func newRelevanceContext(query string) relevanceContext {
	return relevanceContext{words: relevanceQueryWords(query), now: time.Now()}
}

func relevanceQueryWords(query string) []string {
	var out []string
	for _, w := range strings.Fields(strings.ToLower(query)) {
		w = strings.Trim(w, `.,:;!?"'()[]{}`)
		if len(w) > 2 {
			out = append(out, w)
		}
	}
	return out
}

// forceInjectTags marks an entry as never-droppable under --budget. Critical
// severity and architecture-class tags are load-bearing project memory: a tight
// budget must not silently amputate them (AC: critical/architecture never dropped).
var forceInjectTags = map[string]bool{
	"architecture":    true,
	"arch":            true,
	"critical":        true,
	"security":        true,
	"breaking":        true,
	"breaking-change": true,
}

func hasForceInjectTag(tags []string) bool {
	for _, t := range tags {
		if forceInjectTags[strings.ToLower(strings.TrimSpace(t))] {
			return true
		}
	}
	return false
}

// scoreText accumulates word/tag overlap between the query and an entry.
// Tag hits weigh more than body hits (a tag is an explicit topic label).
func (rc relevanceContext) scoreText(score int, reasons *[]string, text string, tags []string) int {
	if len(rc.words) == 0 {
		return score
	}
	textLower := strings.ToLower(text)
	tagsLower := make([]string, len(tags))
	for i, t := range tags {
		tagsLower[i] = strings.ToLower(t)
	}
	hits := 0
	for _, w := range rc.words {
		if strings.Contains(textLower, w) {
			score += 2
			hits++
		}
		for _, t := range tagsLower {
			if strings.Contains(t, w) {
				score += 3
				hits++
				break
			}
		}
	}
	if hits > 0 {
		*reasons = append(*reasons, "topic match")
	}
	return score
}

// priorityBoost mirrors WrongStack's priority weighting (critical/high up,
// low down). Accepts task priority or bug severity strings.
func priorityBoost(score int, reasons *[]string, level string) int {
	switch strings.ToLower(level) {
	case "critical":
		*reasons = append(*reasons, "critical")
		return score + 5
	case "high":
		*reasons = append(*reasons, "high")
		return score + 3
	case "medium":
		return score + 1
	case "low":
		*reasons = append(*reasons, "low")
		return score - 2
	}
	return score
}

// recencyBoost rewards fresh entries and gently penalizes very old ones.
func (rc relevanceContext) recencyBoost(score int, createdAt string) int {
	if createdAt == "" {
		return score
	}
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		// Try date-only.
		t, err = time.Parse("2006-01-02", createdAt[:relevanceMin(len(createdAt), 10)])
		if err != nil {
			return score
		}
	}
	ageDays := rc.now.Sub(t).Hours() / 24
	switch {
	case ageDays < 1:
		return score + 1
	case ageDays > 30:
		return score - 1
	}
	return score
}

func relevanceMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// scoreDecision scores a decision: word/tag overlap + decision type boost +
// recency. Active decisions are high-value memory; superseded ones lose value.
func (rc relevanceContext) scoreDecision(d store.Decision) (int, string) {
	var reasons []string
	score := rc.scoreText(0, &reasons, d.Text+" "+d.Reason, d.Tags)
	score += 2 // decision type boost
	reasons = append(reasons, "decision")
	if d.Pinned {
		score += 3
		reasons = append(reasons, "pinned")
	}
	if d.Status == "superseded" {
		score -= 2
	}
	score = rc.recencyBoost(score, d.CreatedAt)
	return score, strings.Join(reasons, ", ")
}

func (rc relevanceContext) scoreRejection(r store.Rejection) (int, string) {
	var reasons []string
	score := rc.scoreText(0, &reasons, r.Approach+" "+r.Reason, r.Tags)
	score += 3 // anti-pattern: rejected approaches are high-value to surface
	reasons = append(reasons, "anti-pattern")
	score = rc.recencyBoost(score, r.CreatedAt)
	return score, strings.Join(reasons, ", ")
}

func (rc relevanceContext) scoreTask(t store.Task) (int, string) {
	var reasons []string
	score := rc.scoreText(0, &reasons, t.Title+" "+t.Detail, t.Tags)
	score = priorityBoost(score, &reasons, t.Priority)
	score = rc.recencyBoost(score, t.CreatedAt)
	return score, strings.Join(reasons, ", ")
}

func (rc relevanceContext) scoreDiscussion(d store.Discussion) (int, string) {
	var reasons []string
	score := rc.scoreText(0, &reasons, d.Topic+" "+d.Detail, d.Tags)
	score = rc.recencyBoost(score, d.CreatedAt)
	return score, strings.Join(reasons, ", ")
}

func (rc relevanceContext) scoreNote(n store.Note) (int, string) {
	var reasons []string
	score := rc.scoreText(0, &reasons, n.Text, n.Tags)
	score = rc.recencyBoost(score, n.CreatedAt)
	return score, strings.Join(reasons, ", ")
}
