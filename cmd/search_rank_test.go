package cmd

import (
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

func TestBuildSearchResults_ExactIDBeatsSemanticRank(t *testing.T) {
	results := buildSearchResults("TASK-371",
		[]store.Decision{{Text: "semantic fuzzy match about ranking"}},
		nil,
		[]store.Task{{ID: "TASK-371", Title: "exact target"}},
		nil,
		nil,
	)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Task == nil || results[0].Task.ID != "TASK-371" {
		t.Fatalf("exact task did not rank first: %+v", results[0])
	}
}

func TestBuildSearchResults_PathAndSymbolLexicalBoost(t *testing.T) {
	results := buildSearchResults("cmd/search.go",
		[]store.Decision{{Text: "general semantic search"}},
		nil,
		nil,
		[]store.Bug{{ID: "BUG-014", Title: "path hit", AffectedFiles: []string{"cmd/search.go"}}},
		nil,
	)
	if results[0].Bug == nil || results[0].Bug.ID != "BUG-014" {
		t.Fatalf("path hit did not rank first: %+v", results[0])
	}
}

func TestBuildSearchResults_NaturalLanguageKeepsSemanticOrder(t *testing.T) {
	results := buildSearchResults("agent memory",
		[]store.Decision{{Text: "first semantic result"}, {Text: "second semantic result"}},
		nil, nil, nil, nil,
	)
	if results[0].Decision == nil || results[0].Decision.Text != "first semantic result" {
		t.Fatalf("semantic order changed unexpectedly: %+v", results)
	}
}

func TestBuildSearchResultsWithBackend_JSONLFields(t *testing.T) {
	results := buildSearchResultsWithBackend("why qdrant",
		[]store.Decision{{ID: "dec-1", Text: "why qdrant matters", Reason: "reliability", Tags: []string{"qdrant", "reliability"}, SemanticScore: 0.42}},
		[]store.Rejection{{ID: "rej-1", Approach: "use redis", Reason: "rejected", SemanticScore: 0.11}},
		nil,
		nil,
		nil,
		"jsonl",
	)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	got := results[0]
	if got.SourceBackend != "jsonl" {
		t.Fatalf("SourceBackend = %q, want jsonl", got.SourceBackend)
	}
	if got.FinalRank != 1 || got.Rank != 1 {
		t.Fatalf("rank fields not populated: %+v", got)
	}
	if got.RankReason == "" {
		t.Fatal("rank reason should be populated")
	}
	if got.SemanticScore == 0 || got.CosineScore == 0 {
		t.Fatalf("semantic/cosine scores missing: %+v", got)
	}
	if got.Score == 0 {
		t.Fatalf("lexical score should be preserved: %+v", got)
	}
	if got.Decision == nil || got.Decision.ID != "dec-1" {
		t.Fatalf("expected decision result first, got %+v", got)
	}
}

func TestBuildSearchResultsWithBackend_JSONLScoreOverridesRanking(t *testing.T) {
	scores := searchScoreOverrides{}
	scores.set("decision", "dec-low", 10)
	scores.set("rejection", "rej-high", 260)

	results := buildSearchResultsWithBackendAndScores("why qdrant",
		[]store.Decision{{ID: "dec-low", Text: "why qdrant exact phrase"}},
		[]store.Rejection{{ID: "rej-high", Approach: "redis", Reason: "neden reddedildi qdrant fallback"}},
		nil,
		nil,
		nil,
		"jsonl",
		scores,
	)

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Rejection == nil || results[0].Rejection.ID != "rej-high" {
		t.Fatalf("jsonl field score override did not drive final rank: %+v", results)
	}
	if results[0].Score != 260 {
		t.Fatalf("lexical_score = %d, want JSONL field score 260", results[0].Score)
	}
	if results[0].RankReason != "jsonl token/field score" {
		t.Fatalf("RankReason = %q", results[0].RankReason)
	}
}
