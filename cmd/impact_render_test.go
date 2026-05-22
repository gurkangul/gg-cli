package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderImpactCompact_MultiHopStructure(t *testing.T) {
	r := impactResult{
		File:       "internal/auth.go",
		TargetKind: "file",
		HopDepth:   2,
		Dependents: []string{
			"internal/api.go",
			"cmd/server.go",
		},
		DependentHops: []impactDependentHop{
			{Path: "internal/api.go", Hop: 1},
			{Path: "cmd/server.go", Hop: 2},
		},
		Traversal: impactTraversalMetadata{
			RequestedDepth: 2,
			MaxDepth:       2,
			Truncated:      true,
			Cycles:         []string{"internal/api.go"},
		},
	}
	var buf strings.Builder
	renderImpactCompact(&buf, r)
	out := buf.String()

	for _, want := range []string{
		"impact: internal/auth.go — 2 deps h2",
		"H1 internal/api.go",
		"H2 cmd/server.go",
		"C internal/api.go",
		"T truncated at h2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderImpactDefault_GroupsMultiHopDependents(t *testing.T) {
	r := impactResult{
		File:       "internal/auth.go",
		TargetKind: "file",
		HopDepth:   3,
		Dependents: []string{
			"internal/api.go",
			"cmd/server.go",
			"cmd/root.go",
		},
		DependentHops: []impactDependentHop{
			{Path: "internal/api.go", Hop: 1},
			{Path: "cmd/server.go", Hop: 2},
			{Path: "cmd/root.go", Hop: 3},
		},
		Traversal: impactTraversalMetadata{
			RequestedDepth: 3,
			MaxDepth:       3,
			Cycles:         []string{"internal/api.go"},
		},
	}
	var buf strings.Builder
	renderImpactDefault(&buf, r)
	out := buf.String()

	for _, want := range []string{
		"Hop 1:",
		"    → internal/api.go",
		"Hop 2:",
		"    → cmd/server.go",
		"Hop 3:",
		"    → cmd/root.go",
		"Cycles deduped: internal/api.go",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderImpactDefault_CodeGraphWarningAvoidsNonePlaceholder(t *testing.T) {
	r := impactResult{
		Warnings: []string{"CodeGraph for cmd/impact.go: stale (changed_files). Run: gg doctor --fix-index. Optional active mode: gg index --watch --lang go. No background index daemon."},
	}
	var buf strings.Builder
	renderImpactDefault(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "unavailable — code graph missing/stale; see Warnings") {
		t.Fatalf("missing unavailable warning in:\n%s", out)
	}
	if strings.Contains(out, "(none — or graph not indexed)") {
		t.Fatalf("unexpected stale placeholder in:\n%s", out)
	}
}

func TestRenderImpactDefault_GraphUnavailableAvoidsNonePlaceholder(t *testing.T) {
	r := impactResult{
		Warnings: []string{"Memgraph not configured — graph data unavailable (run 'gg index' first)"},
	}
	var buf strings.Builder
	renderImpactDefault(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "unavailable — code graph missing/stale; see Warnings") {
		t.Fatalf("missing unavailable warning in:\n%s", out)
	}
	if strings.Contains(out, "(none — or graph not indexed)") {
		t.Fatalf("unexpected stale placeholder in:\n%s", out)
	}
}

func TestImpactGraphEmptyWarning_NoGenericCommandWithoutSuggestedCommand(t *testing.T) {
	warn := impactGraphEmptyWarning(codeGraphStatus{
		Detail: "project gained supported source files since init, but no indexable module was found - add a supported module manifest (go.mod)",
	}, true)
	if strings.Contains(warn, "gg index --lang <lang>") || strings.Contains(warn, "gg index --lang go") {
		t.Fatalf("unexpected generic index suggestion: %q", warn)
	}
	if !strings.Contains(warn, "no indexable module") {
		t.Fatalf("missing non-indexable detail: %q", warn)
	}
}

func TestImpactGraphEmptyWarning_UsesFullIndexSuggestion(t *testing.T) {
	status := codeGraphStatus{
		Status:            "missing",
		Detail:            "index-state matches HEAD and working tree source files for go",
		DetectedLanguages: []string{"go"},
		SuggestedCommand:  "gg index --lang go --changed",
		MemgraphAvailable: true,
		GraphEmpty:        true,
	}
	status.finalize()

	warn := impactGraphEmptyWarning(status, true)
	joined := strings.Join(impactGraphFreshnessWarningsForStatus(status, "cmd/index.go"), "\n") + "\n" + warn
	if !strings.Contains(joined, "gg doctor --fix-index") {
		t.Fatalf("missing full index suggestion: %q", joined)
	}
	if strings.Contains(joined, "--changed") {
		t.Fatalf("empty graph should not suggest changed index: %q", joined)
	}
}

func TestImpactGraphFreshnessWarning_IsFileSpecificAndMentionsStaleGraphUse(t *testing.T) {
	warnings := impactGraphFreshnessWarningsForStatus(codeGraphStatus{
		Status:            "stale",
		Detail:            "code graph stale: 1 changed file since last index; run gg index --lang typescript --changed",
		ChangedFiles:      1,
		DetectedLanguages: []string{"typescript"},
		SuggestedCommand:  "gg index --lang typescript --changed",
		IndexedAt:         "2026-05-21T21:00:00Z",
	}, "src/foo.ts")
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"CodeGraph for src/foo.ts: stale (changed_files)", "Run: gg doctor --fix-index", "gg index --watch --lang typescript", "WARNING: using stale graph from 2026-05-21T21:00:00Z"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
}

func TestImpactResultJSONIncludesStandardCodeGraphObject(t *testing.T) {
	fresh := codeGraphFreshness{
		Status:                   codeGraphFreshnessStale,
		Reason:                   codeGraphReasonChangedFiles,
		DetectedLanguages:        []string{"go"},
		ChangedFiles:             1,
		SuggestedCommand:         codeGraphRepairCommand,
		BackgroundRefresh:        false,
		ForegroundWatchAvailable: true,
		ForegroundWatchCommand:   "gg index --watch --lang go",
	}
	payload, err := json.Marshal(impactResult{File: "main.go", CodeGraph: &fresh})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	out := string(payload)
	for _, want := range []string{"\"codegraph\"", "\"status\":\"stale\"", "\"reason\":\"changed_files\"", "\"suggested_command\":\"gg doctor --fix-index\"", "\"background_refresh\":false", "\"foreground_watch_available\":true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
}
