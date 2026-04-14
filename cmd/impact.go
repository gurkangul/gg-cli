package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

var impactCmd = &cobra.Command{
	Use:   "impact <file>",
	Short: "Show downstream impact of changing a source file",
	Long: `Show what a change to the given source file affects.

Reports:
  - Files that directly import it (1-hop dependents from the code graph)
  - Symbols the file exports (boundary symbols)
  - Decisions, tasks, and rejections related to the file (semantic search)

Requires Memgraph (gg index must have been run). The knowledge-store search
works even without Memgraph.`,
	Args: cobra.ExactArgs(1),
	RunE: runImpact,
}

var impactKBLimit uint64

func init() {
	impactCmd.Flags().Uint64Var(&impactKBLimit, "kb-limit", 5, "max results per knowledge-base collection")
	rootCmd.AddCommand(impactCmd)
}

type impactResult struct {
	File       string             `json:"file"`
	Dependents []string           `json:"dependents"`
	Symbols    []map[string]any   `json:"symbols"`
	Decisions  []store.Decision   `json:"decisions"`
	Tasks      []store.Task       `json:"tasks"`
	Rejections []store.Rejection  `json:"rejections"`
	Warnings   []string           `json:"warnings,omitempty"`
}

func runImpact(cmd *cobra.Command, args []string) error {
	rawPath, err := requireNonEmpty("file", args[0])
	if err != nil {
		return err
	}

	// Resolve to absolute path for graph queries.
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Load Qdrant deps (always needed for KB search).
	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	result := impactResult{File: absPath}

	// ── Graph queries (optional — skipped if Memgraph not configured) ──────
	cfg, _ := config.Load()
	if cfg != nil && cfg.Memgraph.URI != "" {
		gc, gcErr := graph.New(&cfg.Memgraph, cfg.ProjectID)
		if gcErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("graph client init: %v", gcErr))
		} else {
			defer func() { _ = gc.Close(ctx) }()

			gctx, gcancel := withTimeout(cmd.Context())
			defer gcancel()

			// 1. Downstream dependents.
			deps, depErr := gc.DependentsOf(gctx, absPath)
			if depErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("dependents query: %v", depErr))
			} else {
				result.Dependents = deps
			}

			// 2. Exported symbols.
			symbols, symErr := gc.FileSymbols(gctx, absPath)
			if symErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("symbols query: %v", symErr))
			} else {
				for _, n := range symbols {
					result.Symbols = append(result.Symbols, n.Properties)
				}
			}
		}
	} else {
		result.Warnings = append(result.Warnings, "Memgraph not configured — graph data unavailable (run 'gg index' first)")
	}

	// ── Knowledge-base search (semantic) ────────────────────────────────────
	// Use the file basename + relative path as the search query.
	searchQuery := filepath.Base(absPath)

	vector, embErr := d.embedder.Generate(ctx, searchQuery)
	if embErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("embedding: %v", embErr))
	} else {
		var wg sync.WaitGroup
		var decErr, taskErr, rejErr error

		wg.Add(3)
		go func() {
			defer wg.Done()
			result.Decisions, decErr = d.store.SearchDecisions(ctx, vector, impactKBLimit)
		}()
		go func() {
			defer wg.Done()
			result.Tasks, taskErr = d.store.SearchTasks(ctx, vector, impactKBLimit)
		}()
		go func() {
			defer wg.Done()
			result.Rejections, rejErr = d.store.SearchRejections(ctx, vector, impactKBLimit)
		}()
		wg.Wait()

		if decErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("decisions search: %v", decErr))
		}
		if taskErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("tasks search: %v", taskErr))
		}
		if rejErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("rejections search: %v", rejErr))
		}
	}

	return printJSON(result, func() {
		fmt.Printf("Impact: %s\n", absPath)
		fmt.Println(strings.Repeat("─", 60))

		fmt.Printf("\nDownstream Dependents (%d):\n", len(result.Dependents))
		if len(result.Dependents) == 0 {
			fmt.Println("  (none — or graph not indexed)")
		} else {
			for _, dep := range result.Dependents {
				fmt.Printf("  → %s\n", dep)
			}
		}

		fmt.Printf("\nExported Symbols (%d):\n", len(result.Symbols))
		if len(result.Symbols) == 0 {
			fmt.Println("  (none — or graph not indexed)")
		} else {
			for _, s := range result.Symbols {
				name, _ := s["name"].(string)
				kind, _ := s["kind"].(string)
				if kind != "" {
					fmt.Printf("  %-10s %s\n", kind, name)
				} else {
					fmt.Printf("  %s\n", name)
				}
			}
		}

		if len(result.Decisions) > 0 {
			fmt.Printf("\nRelated Decisions (%d):\n", len(result.Decisions))
			for _, dec := range result.Decisions {
				fmt.Printf("  • %s\n", dec.Text)
				if dec.Reason != "" {
					fmt.Printf("    Reason: %s\n", dec.Reason)
				}
			}
		}

		if len(result.Tasks) > 0 {
			fmt.Printf("\nRelated Tasks (%d):\n", len(result.Tasks))
			for _, t := range result.Tasks {
				fmt.Printf("  %s [%s] %s\n", taskStatusIcon(t.Status), t.ID, t.Title)
			}
		}

		if len(result.Rejections) > 0 {
			fmt.Printf("\nRelated Rejections (%d):\n", len(result.Rejections))
			for _, r := range result.Rejections {
				fmt.Printf("  ✗ %s\n", r.Approach)
				if r.Reason != "" {
					fmt.Printf("    Reason: %s\n", r.Reason)
				}
			}
		}

		if len(result.Warnings) > 0 {
			fmt.Printf("\nWarnings:\n")
			for _, w := range result.Warnings {
				fmt.Printf("  ~ %s\n", w)
			}
		}
	})
}
