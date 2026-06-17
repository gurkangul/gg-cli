package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

func TestLinkedSearchRenderingLabelsSourceProject(t *testing.T) {
	result := searchResult{
		Kind:            "task",
		Rank:            1,
		SourceProjectID: "linked-project",
		Task: &store.Task{
			ID:       "TASK-999",
			Title:    "linked context",
			Priority: "low",
			Status:   "pending",
		},
	}

	var full bytes.Buffer
	renderSearchResultsDefault(&full, []searchResult{result})
	if !strings.Contains(full.String(), "[linked-project] [TASK-999]") {
		t.Fatalf("default output missing source project label:\n%s", full.String())
	}

	var compact bytes.Buffer
	renderSearchResultsCompact(&compact, []searchResult{result})
	if !strings.Contains(compact.String(), "@linked-project") {
		t.Fatalf("compact output missing source project label:\n%s", compact.String())
	}
}

func TestLinkedContextRenderingLabelsSourceProjects(t *testing.T) {
	bundle := contextBundle{
		decisions: []store.Decision{{ID: "dec-1", Text: "use linked context", CreatedAt: "2026-05-03T00:00:00Z"}},
		tasks:     []store.Task{{ID: "TASK-999", Title: "linked task", Priority: "low", Status: "pending"}},
		notes:     []store.Note{{ID: "note-1", Text: "linked note", CreatedAt: "2026-05-03T00:00:00Z"}},
		sources: sourceLabels{
			sourceKey("decision", "dec-1"): "linked-project",
			sourceKey("task", "TASK-999"):  "linked-project",
			sourceKey("note", "note-1"):    "linked-project",
		},
	}

	var full bytes.Buffer
	renderContextDefault(&full, "linked", bundle, nil, nil)
	if !strings.Contains(full.String(), "[linked-project] [2026-05-03]") ||
		!strings.Contains(full.String(), "[linked-project] [TASK-999]") {
		t.Fatalf("default context missing source labels:\n%s", full.String())
	}

	var compact bytes.Buffer
	renderContextCompact(&compact, "linked", bundle, nil, nil)
	if got := compact.String(); strings.Count(got, "@linked-project") < 3 {
		t.Fatalf("compact context missing source labels:\n%s", got)
	}
}

func TestOpenLinkedStoresWarnsForMissingPathWithoutFailing(t *testing.T) {
	_, warnings := openLinkedStores(&config.Config{
		ProjectID: "current-project",
		LinkedProjects: []config.LinkedProjectConfig{
			{Path: "/definitely/missing/gg-linked-project"},
		},
	})
	if len(warnings) != 1 {
		t.Fatalf("warnings: got %d, want 1", len(warnings))
	}
	if !strings.Contains(warnings[0], "linked project") {
		t.Fatalf("warning should identify linked project: %q", warnings[0])
	}
}
