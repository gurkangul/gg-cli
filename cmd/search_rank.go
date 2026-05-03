package cmd

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/gurkangul/gg-cli/internal/store"
)

var exactSearchID = regexp.MustCompile(`\b(TASK|BUG)-\d{3,}\b`)

type searchResult struct {
	Kind      string           `json:"kind"`
	Rank      int              `json:"rank"`
	Score     int              `json:"lexical_score"`
	Decision  *store.Decision  `json:"decision,omitempty"`
	Rejection *store.Rejection `json:"rejection,omitempty"`
	Task      *store.Task      `json:"task,omitempty"`
	Bug       *store.Bug       `json:"bug,omitempty"`
	Note      *store.Note      `json:"note,omitempty"`
}

func buildSearchResults(query string, decisions []store.Decision, rejections []store.Rejection, tasks []store.Task, bugs []store.Bug, notes []store.Note) []searchResult {
	var out []searchResult
	rank := 0
	for i := range decisions {
		d := decisions[i]
		out = append(out, searchResult{Kind: "decision", Rank: rank, Score: lexicalScoreWithPrimary(query, d.ID, decisionSearchText(d)), Decision: &d})
		rank++
	}
	for i := range rejections {
		r := rejections[i]
		out = append(out, searchResult{Kind: "rejection", Rank: rank, Score: lexicalScoreWithPrimary(query, r.ID, rejectionSearchText(r)), Rejection: &r})
		rank++
	}
	for i := range tasks {
		t := tasks[i]
		out = append(out, searchResult{Kind: "task", Rank: rank, Score: lexicalScoreWithPrimary(query, t.ID, taskSearchText(t)), Task: &t})
		rank++
	}
	for i := range bugs {
		b := bugs[i]
		out = append(out, searchResult{Kind: "bug", Rank: rank, Score: lexicalScoreWithPrimary(query, b.ID, bugSearchText(b)), Bug: &b})
		rank++
	}
	for i := range notes {
		n := notes[i]
		out = append(out, searchResult{Kind: "note", Rank: rank, Score: lexicalScoreWithPrimary(query, n.ID, noteSearchText(n)), Note: &n})
		rank++
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Rank < out[j].Rank
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}

func trimSearchResults(results []searchResult, limit uint64) []searchResult {
	if limit == 0 || len(results) <= int(limit) {
		return results
	}
	return results[:limit]
}

func lexicalScoreWithPrimary(query, primaryID, text string) int {
	q := strings.ToLower(strings.TrimSpace(query))
	t := strings.ToLower(text)
	if q == "" || t == "" {
		return 0
	}
	if primaryID != "" && strings.EqualFold(q, primaryID) {
		return 1200
	}
	if exactSearchID.MatchString(strings.ToUpper(q)) && strings.Contains(t, q) {
		return 1000
	}
	if strings.Contains(q, "/") || strings.Contains(q, ".") {
		if strings.Contains(t, q) {
			return 800
		}
	}
	if strings.Contains(t, q) {
		return 200
	}
	score := 0
	for _, term := range strings.Fields(q) {
		if len(term) >= 2 && strings.Contains(t, term) {
			score += 10
		}
	}
	return score
}

func decisionSearchText(d store.Decision) string {
	return strings.Join(append([]string{d.ID, d.Text, d.Reason, d.TaskID}, d.Tags...), " ")
}

func rejectionSearchText(r store.Rejection) string {
	return strings.Join(append([]string{r.ID, r.Approach, r.Reason, r.TaskID}, r.Tags...), " ")
}

func taskSearchText(t store.Task) string {
	return strings.Join(append([]string{t.ID, t.Title, t.Detail, t.Status, t.Priority}, t.Tags...), " ")
}

func bugSearchText(b store.Bug) string {
	parts := []string{b.ID, b.Title, b.Detail, b.Status, b.Severity, b.RootCause, b.FixSummary, b.TaskID}
	parts = append(parts, b.Tags...)
	parts = append(parts, b.AffectedFiles...)
	parts = append(parts, b.AffectedSymbols...)
	return strings.Join(parts, " ")
}

func noteSearchText(n store.Note) string {
	return strings.Join(append([]string{n.ID, n.Text, n.TaskID}, n.Tags...), " ")
}

func compactSearchResultLine(r searchResult) string {
	switch {
	case r.Decision != nil:
		return compactDecisionLine(*r.Decision)
	case r.Rejection != nil:
		return compactRejectionLine(*r.Rejection)
	case r.Task != nil:
		return compactTaskLine(*r.Task)
	case r.Bug != nil:
		return compactBugLine(*r.Bug)
	case r.Note != nil:
		return compactNoteLine(*r.Note)
	default:
		return fmt.Sprintf("?  %s", r.Kind)
	}
}
