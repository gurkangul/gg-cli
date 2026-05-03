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
