package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
)

func runImpactSymbol(cmd *cobra.Command) error {
	query := impactSymbol
	mode := "symbol"
	if impactFlow != "" {
		query = impactFlow
		mode = "flow"
	}
	query, err := requireNonEmpty(mode, query)
	if err != nil {
		return err
	}
	if impactHops < 1 {
		return fmt.Errorf("--depth must be >= 1")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		return fmt.Errorf("graph client init: %w", err)
	}
	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()
	defer func() { _ = gc.Close(ctx) }()

	matches, err := gc.FindSymbols(ctx, query)
	if err != nil {
		return err
	}
	match, err := chooseSymbolMatch(query, matches, impactSymbolFile)
	if err != nil {
		return err
	}

	result := impactResult{
		TargetKind:    mode,
		File:          match.SourceFile,
		SymbolQuery:   query,
		SymbolMatch:   &match,
		SymbolMatches: matches,
		Traversal: impactTraversalMetadata{
			RequestedDepth: impactHops,
			MaxDepth:       impactHops,
		},
		Warnings: []string{
			"static call flow uses indexed CALLS edges only; dynamic dispatch, reflection, generated/framework code, and unresolved SCIP references may be missing",
		},
	}
	if mode == "flow" {
		flow, flowErr := gc.ForwardCallFlow(ctx, match, impactHops)
		if flowErr != nil {
			return flowErr
		}
		result.Flow = &flow
		result.Traversal.Truncated = flow.Truncated
		result.Traversal.Cycles = flow.Cycles
		if len(flow.Edges) == 0 {
			result.Warnings = append(result.Warnings, "no CALLS edges found for this symbol — run/extend indexing before treating the empty flow as proof of no callers")
		}
	}

	return printJSON(result, func() {
		if isCompactActive(cmd) {
			emitCompact(cmd, "impact",
				func(w io.Writer) { renderImpactSymbolDefault(w, result) },
				func(w io.Writer) { renderImpactSymbolCompact(w, result) },
				compactRendererV_impact,
			)
			return
		}
		renderImpactSymbolDefault(os.Stdout, result)
	})
}

func chooseSymbolMatch(query string, matches []graph.SymbolMatch, sourceFile string) (graph.SymbolMatch, error) {
	if len(matches) == 0 {
		return graph.SymbolMatch{}, fmt.Errorf("symbol %q not found in graph — run 'gg index' or check the symbol name", query)
	}
	if sourceFile != "" {
		for _, match := range matches {
			if match.SourceFile == sourceFile {
				return match, nil
			}
		}
		return graph.SymbolMatch{}, fmt.Errorf("symbol %q has no match in --file %s", query, sourceFile)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	var choices []string
	for _, match := range matches {
		choices = append(choices, fmt.Sprintf("%s:%s", match.SourceFile, match.Name))
	}
	return graph.SymbolMatch{}, fmt.Errorf("symbol %q is ambiguous (%d matches: %s); rerun with --file <source_file>", query, len(matches), strings.Join(choices, ", "))
}

func renderImpactSymbolDefault(w io.Writer, r impactResult) {
	fmt.Fprintf(w, "Impact: symbol %s (%s)\n", r.SymbolQuery, r.File)
	fmt.Fprintln(w, strings.Repeat("─", 60))
	if r.SymbolMatch != nil {
		fmt.Fprintf(w, "\nResolved Symbol:\n  %s %s:%s\n", r.SymbolMatch.Kind, r.SymbolMatch.SourceFile, r.SymbolMatch.Name)
	}
	if r.Flow != nil {
		fmt.Fprintf(w, "\nForward Call Flow (%d edges, depth %d):\n", len(r.Flow.Edges), r.Flow.MaxDepth)
		if len(r.Flow.Edges) == 0 {
			fmt.Fprintln(w, "  (none indexed)")
		}
		for _, edge := range r.Flow.Edges {
			cycle := ""
			if edge.Cycle {
				cycle = " (cycle)"
			}
			fmt.Fprintf(w, "  H%d %s:%s → %s:%s%s\n",
				edge.Depth, edge.From.SourceFile, edge.From.Name, edge.To.SourceFile, edge.To.Name, cycle)
		}
		if r.Flow.Truncated {
			fmt.Fprintf(w, "  Traversal truncated at depth %d\n", r.Flow.MaxDepth)
		}
		if len(r.Flow.Cycles) > 0 {
			fmt.Fprintf(w, "  Cycles deduped: %s\n", strings.Join(r.Flow.Cycles, ", "))
		}
	}
	if len(r.SymbolMatches) > 1 {
		fmt.Fprintf(w, "\nMatching Symbols (%d):\n", len(r.SymbolMatches))
		for _, match := range r.SymbolMatches {
			fmt.Fprintf(w, "  %s:%s\n", match.SourceFile, match.Name)
		}
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintln(w, "\nWarnings:")
		for _, warn := range r.Warnings {
			fmt.Fprintf(w, "  ~ %s\n", warn)
		}
	}
}

func renderImpactSymbolCompact(w io.Writer, r impactResult) {
	edges := 0
	if r.Flow != nil {
		edges = len(r.Flow.Edges)
	}
	fmt.Fprintf(w, "impact: symbol %s — %s %d matches %d flow\n\n", r.SymbolQuery, r.File, len(r.SymbolMatches), edges)
	if r.SymbolMatch != nil {
		fmt.Fprintf(w, "S %s:%s\n", r.SymbolMatch.SourceFile, r.SymbolMatch.Name)
	}
	if r.Flow != nil {
		for _, edge := range r.Flow.Edges {
			cycle := ""
			if edge.Cycle {
				cycle = " cycle"
			}
			fmt.Fprintf(w, "H%d %s:%s -> %s:%s%s\n", edge.Depth, edge.From.SourceFile, edge.From.Name, edge.To.SourceFile, edge.To.Name, cycle)
		}
		if r.Flow.Truncated {
			fmt.Fprintf(w, "T truncated at h%d\n", r.Flow.MaxDepth)
		}
		if len(r.Flow.Cycles) > 0 {
			fmt.Fprintf(w, "C %s\n", strings.Join(r.Flow.Cycles, ", "))
		}
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintf(w, "\n! %s\n", strings.Join(r.Warnings, "; "))
	}
}
