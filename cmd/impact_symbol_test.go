package cmd

import (
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/graph"
)

func TestChooseSymbolMatchRequiresFileWhenAmbiguous(t *testing.T) {
	matches := []graph.SymbolMatch{
		{Name: "Run", SourceFile: "cmd/a.go"},
		{Name: "Run", SourceFile: "cmd/b.go"},
	}
	_, err := chooseSymbolMatch("Run", matches, "")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous error, got %v", err)
	}
	got, err := chooseSymbolMatch("Run", matches, "cmd/b.go")
	if err != nil {
		t.Fatalf("choose by file: %v", err)
	}
	if got.SourceFile != "cmd/b.go" {
		t.Fatalf("got %s, want cmd/b.go", got.SourceFile)
	}
}

func TestRenderImpactSymbolCompactShowsStaticLimits(t *testing.T) {
	result := impactResult{
		SymbolQuery: "Run",
		File:        "cmd/a.go",
		SymbolMatch: &graph.SymbolMatch{Name: "Run", SourceFile: "cmd/a.go"},
		SymbolMatches: []graph.SymbolMatch{
			{Name: "Run", SourceFile: "cmd/a.go"},
		},
		Warnings: []string{"static call flow uses indexed CALLS edges only"},
	}
	var buf strings.Builder
	renderImpactSymbolCompact(&buf, result)
	out := buf.String()
	if !strings.Contains(out, "S cmd/a.go:Run") || !strings.Contains(out, "indexed CALLS edges only") {
		t.Fatalf("unexpected compact output:\n%s", out)
	}
}
