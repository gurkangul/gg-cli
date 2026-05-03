package cmd

import (
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
