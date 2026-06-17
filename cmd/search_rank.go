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
	Kind            string           `json:"kind"`
	Rank            int              `json:"rank"`
	FinalRank       int              `json:"final_rank"`
	Score           int              `json:"lexical_score"`
	SemanticScore   float32          `json:"semantic_score"`
	CosineScore     float32          `json:"cosine_score"`
	RankReason      string           `json:"rank_reason"`
	SourceBackend   string           `json:"source_backend"`
	SourceProjectID string           `json:"source_project_id,omitempty"`
	Decision        *store.Decision  `json:"decision,omitempty"`
	Rejection       *store.Rejection `json:"rejection,omitempty"`
	Task            *store.Task      `json:"task,omitempty"`
	Bug             *store.Bug       `json:"bug,omitempty"`
	Note            *store.Note      `json:"note,omitempty"`
	Message         *store.Message   `json:"message,omitempty"`
}

type searchScoreOverrides map[string]int

func (s searchScoreOverrides) set(kind, id string, score int) {
	if s == nil || id == "" {
		return
	}
	s[kind+":"+id] = score
}

func (s searchScoreOverrides) get(kind, id string) (int, bool) {
	if s == nil || id == "" {
		return 0, false
	}
	score, ok := s[kind+":"+id]
	return score, ok
}

func buildSearchResults(query string, decisions []store.Decision, rejections []store.Rejection, tasks []store.Task, bugs []store.Bug, notes []store.Note) []searchResult {
	return buildSearchResultsWithBackend(query, decisions, rejections, tasks, bugs, notes, "sqlite")
}

func buildSearchResultsWithBackend(query string, decisions []store.Decision, rejections []store.Rejection, tasks []store.Task, bugs []store.Bug, notes []store.Note, backend string) []searchResult {
	return buildSearchResultsWithBackendAndScores(query, decisions, rejections, tasks, bugs, notes, backend, nil)
}

func buildSearchResultsWithBackendAndScores(query string, decisions []store.Decision, rejections []store.Rejection, tasks []store.Task, bugs []store.Bug, notes []store.Note, backend string, scores searchScoreOverrides) []searchResult {
	return buildSearchResultsWithBackendScoresAndMessages(query, decisions, rejections, tasks, bugs, notes, nil, backend, scores)
}

func buildSearchResultsWithBackendScoresAndMessages(query string, decisions []store.Decision, rejections []store.Rejection, tasks []store.Task, bugs []store.Bug, notes []store.Note, messages []store.Message, backend string, scores searchScoreOverrides) []searchResult {
	var out []searchResult
	rank := 0
	for i := range decisions {
		d := decisions[i]
		score := lexicalScoreWithPrimary(query, d.ID, decisionSearchText(d))
		if override, ok := scores.get("decision", d.ID); ok {
			score = override
		}
		out = append(out, newSearchResult("decision", rank, score, d.SemanticScore, backend, &d, nil, nil, nil, nil, nil))
		rank++
	}
	for i := range rejections {
		r := rejections[i]
		score := lexicalScoreWithPrimary(query, r.ID, rejectionSearchText(r))
		if override, ok := scores.get("rejection", r.ID); ok {
			score = override
		}
		out = append(out, newSearchResult("rejection", rank, score, r.SemanticScore, backend, nil, &r, nil, nil, nil, nil))
		rank++
	}
	for i := range tasks {
		t := tasks[i]
		score := lexicalScoreWithPrimary(query, t.ID, taskSearchText(t))
		if override, ok := scores.get("task", t.ID); ok {
			score = override
		}
		out = append(out, newSearchResult("task", rank, score, t.SemanticScore, backend, nil, nil, &t, nil, nil, nil))
		rank++
	}
	for i := range bugs {
		b := bugs[i]
		score := lexicalScoreWithPrimary(query, b.ID, bugSearchText(b))
		if override, ok := scores.get("bug", b.ID); ok {
			score = override
		}
		out = append(out, newSearchResult("bug", rank, score, b.SemanticScore, backend, nil, nil, nil, &b, nil, nil))
		rank++
	}
	for i := range notes {
		n := notes[i]
		score := lexicalScoreWithPrimary(query, n.ID, noteSearchText(n))
		if override, ok := scores.get("note", n.ID); ok {
			score = override
		}
		out = append(out, newSearchResult("note", rank, score, n.SemanticScore, backend, nil, nil, nil, nil, &n, nil))
		rank++
	}
	for i := range messages {
		m := messages[i]
		score := lexicalScoreWithPrimary(query, m.ID, messageSearchText(m))
		if override, ok := scores.get("message", m.ID); ok {
			score = override
		}
		out = append(out, newSearchResult("message", rank, score, 0, backend, nil, nil, nil, nil, nil, &m))
		rank++
	}
	return rankSearchResults(out)
}

func newSearchResult(kind string, rank, lexicalScore int, semanticScore float32, backend string, d *store.Decision, r *store.Rejection, t *store.Task, b *store.Bug, n *store.Note, m *store.Message) searchResult {
	if backend == "" {
		backend = "sqlite"
	}
	return searchResult{
		Kind:          kind,
		Rank:          rank,
		Score:         lexicalScore,
		SemanticScore: semanticScore,
		CosineScore:   semanticScore,
		SourceBackend: backend,
		Decision:      d,
		Rejection:     r,
		Task:          t,
		Bug:           b,
		Note:          n,
		Message:       m,
	}
}

func rankSearchResults(results []searchResult) []searchResult {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		// Tie on lexical score (common — every record containing the query term
		// gets the same +token boost): prefer the more semantically relevant
		// result before falling back to build/insertion order. Without this a
		// less-relevant decision can outrank a higher-semantic bug/task purely by
		// kind precedence, and get truncated off the default --limit (audit VEC-3).
		if results[i].SemanticScore != results[j].SemanticScore {
			return results[i].SemanticScore > results[j].SemanticScore
		}
		return results[i].Rank < results[j].Rank
	})
	for i := range results {
		results[i].Rank = i + 1
		results[i].FinalRank = i + 1
		results[i].RankReason = searchRankReason(results[i])
	}
	return results
}

func searchRankReason(r searchResult) string {
	switch {
	case r.SourceBackend == "jsonl" && r.Score > 0:
		return "jsonl token/field score"
	case r.SourceBackend == "cache" && r.Score > 0:
		return "cached semantic results with lexical boost"
	case r.Score >= 1000:
		return "exact ID lexical boost over semantic result"
	case r.Score > 0 && r.SemanticScore > 0:
		return "semantic cosine score with lexical boost"
	case r.SemanticScore > 0:
		return "semantic cosine score"
	default:
		return "stable fallback order"
	}
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

func messageSearchText(m store.Message) string {
	return strings.Join([]string{m.ID, m.FromRole, m.ToRole, m.Content, m.Audience, m.TaskID, m.CreatedAt}, " ")
}

func compactSearchResultLine(r searchResult) string {
	switch {
	case r.Decision != nil:
		return compactDecisionLine(*r.Decision) + sourceSuffix(r.SourceProjectID)
	case r.Rejection != nil:
		return compactRejectionLine(*r.Rejection) + sourceSuffix(r.SourceProjectID)
	case r.Task != nil:
		return compactTaskLine(*r.Task) + sourceSuffix(r.SourceProjectID)
	case r.Bug != nil:
		return compactBugLine(*r.Bug) + sourceSuffix(r.SourceProjectID)
	case r.Note != nil:
		return compactNoteLine(*r.Note) + sourceSuffix(r.SourceProjectID)
	case r.Message != nil:
		return compactMessageLine(*r.Message) + sourceSuffix(r.SourceProjectID)
	default:
		return fmt.Sprintf("?  %s", r.Kind)
	}
}
