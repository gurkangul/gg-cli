package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

var impactCmd = &cobra.Command{
	Use:   "impact <file|BUG-NNN|TASK-NNN>",
	Short: "Show downstream impact of changing a file, or blast radius of a bug or task",
	Long: `Show what a change to the given source file affects, or what a bug/task touches.

File mode (default):
  - Files that directly import it (1-hop dependents from the code graph)
  - Symbols the file exports (boundary symbols)
  - Decisions, tasks, and rejections related to the file (semantic search)
  - Historical bugs that have affected this file (Bug→File graph edges)

Bug mode (BUG-NNN argument):
  - Files and symbols the bug affects (Bug→File/Symbol graph edges)
  - Decisions, tasks, and rejections related to the bug (semantic search)

Task mode (TASK-NNN argument):
  - Downstream dependents (tasks that DEPENDS_ON this one, or are BLOCKED by it)
  - Decisions and related tasks from the knowledge store (semantic search)

Requires Memgraph (gg index must have been run). The knowledge-store search
works even without Memgraph.

When CodeGraph is missing, stale, or unavailable, impact uses the shared
freshness notice contract: repair is explicit with gg doctor --fix-index;
gg never refreshes the graph in the background.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runImpact,
}

var impactKBLimit uint64
var impactCompact bool
var impactHops int
var impactSymbol string
var impactFlow string
var impactSymbolFile string

const maxImpactHops = 10

func init() {
	impactCmd.Flags().Uint64Var(&impactKBLimit, "kb-limit", 5, "max results per knowledge-base collection")
	impactCmd.Flags().BoolVar(&impactCompact, "compact", false, "one line per item — drops symbol kinds and reasons to preserve agent context window")
	impactCmd.Flags().IntVar(&impactHops, "hops", 1, "max downstream dependency hops to traverse in file mode")
	impactCmd.Flags().IntVar(&impactHops, "depth", 1, "alias for --hops")
	impactCmd.Flags().StringVar(&impactSymbol, "symbol", "", "show impact for a symbol name; use --file to disambiguate duplicates")
	impactCmd.Flags().StringVar(&impactFlow, "flow", "", "render bounded forward CALLS flow for a symbol name; use --depth N and --file if ambiguous")
	impactCmd.Flags().StringVar(&impactSymbolFile, "file", "", "source file used to disambiguate --symbol or --flow")
	rootCmd.AddCommand(impactCmd)
}

type impactDependentHop struct {
	Path string `json:"path"`
	Hop  int    `json:"hop"`
}

type impactTraversalMetadata struct {
	RequestedDepth int      `json:"requested_depth"`
	MaxDepth       int      `json:"max_depth"`
	Truncated      bool     `json:"truncated"`
	Cycles         []string `json:"cycles,omitempty"`
}

type impactResult struct {
	File           string                  `json:"file"`
	TargetKind     string                  `json:"target_kind"`
	HopDepth       int                     `json:"hop_depth"`
	Dependents     []string                `json:"dependents"`
	DependentHops  []impactDependentHop    `json:"dependent_hops,omitempty"`
	Traversal      impactTraversalMetadata `json:"traversal"`
	Symbols        []map[string]any        `json:"symbols"`
	Decisions      []store.Decision        `json:"decisions"`
	Tasks          []store.Task            `json:"tasks"`
	Rejections     []store.Rejection       `json:"rejections"`
	HistoricalBugs []graph.BugRef          `json:"historical_bugs,omitempty"`
	Warnings       []string                `json:"warnings,omitempty"`
	CodeGraph      *codeGraphFreshness     `json:"codegraph,omitempty"`
	SymbolQuery    string                  `json:"symbol_query,omitempty"`
	SymbolMatch    *graph.SymbolMatch      `json:"symbol_match,omitempty"`
	SymbolMatches  []graph.SymbolMatch     `json:"symbol_matches,omitempty"`
	Flow           *graph.CallFlow         `json:"flow,omitempty"`
}

func runImpact(cmd *cobra.Command, args []string) error {
	if impactSymbol != "" || impactFlow != "" {
		return runImpactSymbol(cmd)
	}
	if len(args) == 0 {
		return fmt.Errorf("file, BUG-NNN, TASK-NNN, --symbol, or --flow is required")
	}
	rawArg, err := requireNonEmpty("file", args[0])
	if err != nil {
		return err
	}

	// Detect BUG-NNN / TASK-NNN mode vs file mode.
	if _, parseErr := store.ParseBugID(rawArg); parseErr == nil {
		return runImpactBug(cmd, rawArg)
	}
	if _, parseErr := store.ParseTaskID(rawArg); parseErr == nil {
		return runImpactTask(cmd, rawArg)
	}
	if impactHops < 1 {
		return fmt.Errorf("--hops/--depth must be >= 1")
	}

	// Resolve to project-relative path — that's what the graph indexes (BUG-010).
	projRoot, err := config.FindRoot()
	if err != nil {
		return fmt.Errorf("find project root: %w", err)
	}
	relPath, ok := normalizeProjectPath(projRoot, "", rawArg)
	if !ok {
		return fmt.Errorf("path %q is outside project root %q", rawArg, projRoot)
	}

	// Load Qdrant deps (always needed for KB search).
	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	result := impactResult{
		File:       relPath,
		TargetKind: "file",
		HopDepth:   impactHops,
		Traversal: impactTraversalMetadata{
			RequestedDepth: impactHops,
			MaxDepth:       impactHops,
		},
	}
	queryHops := impactHops
	if queryHops > maxImpactHops {
		queryHops = maxImpactHops
		result.Traversal.MaxDepth = maxImpactHops
		result.Traversal.Truncated = true
		result.Warnings = append(result.Warnings, fmt.Sprintf("--hops capped at %d (requested %d)", maxImpactHops, impactHops))
	}
	if _, statErr := os.Stat(filepath.Join(projRoot, filepath.FromSlash(relPath))); os.IsNotExist(statErr) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("target file %s does not exist in the working tree; if it was deleted, run gg index --changed to remove stale graph nodes", relPath))
	}

	// ── Graph queries (optional — skipped if Memgraph not configured) ──────
	cfg, _ := config.Load()
	var graphStatus codeGraphStatus
	graphStatusKnown := false
	if cfg != nil {
		graphStatus = collectCodeGraphStatus(ctx, projRoot, filepath.Join(projRoot, config.DirName), cfg)
		graphStatusKnown = true
		fresh := graphStatus.freshnessContract()
		result.CodeGraph = &fresh
		result.Warnings = append(result.Warnings, impactGraphFreshnessWarningsForStatus(graphStatus, relPath)...)
	} else {
		result.Warnings = append(result.Warnings, "code graph freshness unknown: config unavailable")
	}
	if cfg != nil && cfg.Memgraph.URI != "" {
		gc, gcErr := graph.New(&cfg.Memgraph, cfg.ProjectID)
		if gcErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("graph client init: %v", gcErr))
		} else {
			defer func() { _ = gc.Close(ctx) }()

			gctx, gcancel := withTimeout(cmd.Context())
			defer gcancel()

			// 0. Detect empty graph — Memgraph is up but nothing has been indexed yet.
			if fileCount, fcErr := gc.CountFileNodes(gctx); fcErr == nil && fileCount == 0 {
				result.Warnings = append(result.Warnings, impactGraphEmptyWarning(graphStatus, graphStatusKnown))
			}

			// 1. Downstream dependents.
			if queryHops == 1 {
				deps, depErr := gc.DependentsOf(gctx, relPath)
				if depErr != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("dependents query: %v", depErr))
				} else {
					result.Dependents = deps
					for _, dep := range deps {
						result.DependentHops = append(result.DependentHops, impactDependentHop{Path: dep, Hop: 1})
					}
				}
			} else {
				traversal, depErr := gc.DependentsOfDepth(gctx, relPath, queryHops)
				if depErr != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("dependents query: %v", depErr))
				} else {
					result.Traversal.MaxDepth = traversal.MaxDepth
					result.Traversal.Truncated = result.Traversal.Truncated || traversal.Truncated
					result.Traversal.Cycles = traversal.Cycles
					for _, dep := range traversal.Dependents {
						result.Dependents = append(result.Dependents, dep.Path)
						result.DependentHops = append(result.DependentHops, impactDependentHop{Path: dep.Path, Hop: dep.Hop})
					}
				}
			}

			// 2. Exported symbols.
			symbols, symErr := gc.FileSymbols(gctx, relPath)
			if symErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("symbols query: %v", symErr))
			} else {
				for _, n := range symbols {
					result.Symbols = append(result.Symbols, n.Properties)
				}
			}

			// 3. Historical bugs that have affected this file.
			bugs, bugErr := gc.BugsAffectingFile(gctx, relPath)
			if bugErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("historical bugs query: %v", bugErr))
			} else {
				result.HistoricalBugs = bugs
			}
		}
	} else {
		result.Warnings = append(result.Warnings, "Memgraph not configured — graph data unavailable (run 'gg index' first)")
	}

	// ── Knowledge-base search (semantic) ────────────────────────────────────
	// Use the file basename + relative path as the search query.
	searchQuery := filepath.Base(relPath)

	vector, embErr := d.embedder.Generate(ctx, searchQuery)
	if embErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("embedding: %v", embErr))
	} else {
		var wg sync.WaitGroup
		var decErr, taskErr, rejErr error

		wg.Add(3)
		go func() {
			defer wg.Done()
			result.Decisions, decErr = d.store.SearchDecisions(ctx, vector, impactKBLimit, true)
		}()
		go func() {
			defer wg.Done()
			result.Tasks, taskErr = d.store.SearchTasks(ctx, vector, impactKBLimit, true)
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
		if isCompactActive(cmd) {
			emitCompact(cmd, "impact",
				func(w io.Writer) { renderImpactDefault(w, result) },
				func(w io.Writer) { renderImpactCompact(w, result) },
				compactRendererV_impact,
			)
			return
		}
		renderImpactDefault(os.Stdout, result)
	})
}

func impactGraphFreshnessWarnings(ctx context.Context, root string, cfg *config.Config) []string {
	if cfg == nil {
		return []string{"code graph freshness unknown: config unavailable"}
	}
	status := collectCodeGraphStatus(ctx, root, filepath.Join(root, config.DirName), cfg)
	return impactGraphFreshnessWarningsForStatus(status, "")
}

func impactGraphFreshnessWarningsForStatus(status codeGraphStatus, file string) []string {
	if msg := codeGraphNoticeOneLine(status); msg != "" {
		if file != "" {
			msg = strings.Replace(msg, "CodeGraph:", "CodeGraph for "+file+":", 1)
		}
		warnings := []string{msg}
		if status.IndexedAt != "" && status.freshnessContract().Status == codeGraphFreshnessStale {
			warnings = append(warnings, "WARNING: using stale graph from "+status.IndexedAt)
		}
		return warnings
	}
	if codeGraphNeedsAction(status.Status) {
		msg := "CodeGraph stale or missing"
		if file != "" {
			msg += " for " + file
		}
		msg += "."
		if status.SuggestedCommand != "" {
			msg += " Run: " + status.SuggestedCommand
		}
		if status.Detail != "" {
			msg += " Detail: " + status.Detail
		}
		warnings := []string{msg}
		if status.IndexedAt != "" && status.Status != "missing" {
			warnings = append(warnings, "WARNING: using stale graph from "+status.IndexedAt)
		}
		return warnings
	}
	switch status.Status {
	case "ready":
		return nil
	default:
		if status.Detail != "" {
			return []string{fmt.Sprintf("code graph freshness unknown: %s", status.Detail)}
		}
		return []string{fmt.Sprintf("code graph freshness unknown: %s", status.Status)}
	}
}

func impactGraphEmptyWarning(status codeGraphStatus, statusKnown bool) string {
	if statusKnown {
		fresh := status.freshnessContract()
		if fresh.SuggestedCommand != "" {
			return "graph is empty — run '" + fresh.SuggestedCommand + "' to repair it"
		}
		if strings.Contains(status.Detail, "no indexable module") {
			return "graph is empty — " + status.Detail
		}
	}
	return "graph is empty — run 'gg index --lang <lang>' to populate it (e.g. 'gg index --lang go')"
}

func renderImpactDefault(w io.Writer, result impactResult) {
	fmt.Fprintf(w, "Impact: %s\n", result.File)
	fmt.Fprintln(w, strings.Repeat("─", 60))
	graphWarning := impactHasCodeGraphWarning(result.Warnings)

	fmt.Fprintf(w, "\nDownstream Dependents (%d):\n", len(result.Dependents))
	switch {
	case len(result.Dependents) == 0 && graphWarning:
		fmt.Fprintln(w, "  (unavailable — code graph missing/stale; see Warnings)")
	case len(result.Dependents) == 0:
		fmt.Fprintln(w, "  (none — or graph not indexed)")
	case result.HopDepth > 1:
		for hop := 1; hop <= result.Traversal.MaxDepth; hop++ {
			var group []string
			for _, dep := range result.DependentHops {
				if dep.Hop == hop {
					group = append(group, dep.Path)
				}
			}
			if len(group) == 0 {
				continue
			}
			fmt.Fprintf(w, "  Hop %d:\n", hop)
			for _, dep := range group {
				fmt.Fprintf(w, "    → %s\n", dep)
			}
		}
		if len(result.Traversal.Cycles) > 0 {
			fmt.Fprintf(w, "  Cycles deduped: %s\n", strings.Join(result.Traversal.Cycles, ", "))
		}
		if result.Traversal.Truncated {
			fmt.Fprintf(w, "  Traversal truncated at hop %d\n", result.Traversal.MaxDepth)
		}
	default:
		for _, dep := range result.Dependents {
			fmt.Fprintf(w, "  → %s\n", dep)
		}
	}

	fmt.Fprintf(w, "\nExported Symbols (%d):\n", len(result.Symbols))
	if len(result.Symbols) == 0 && graphWarning {
		fmt.Fprintln(w, "  (unavailable — code graph missing/stale; see Warnings)")
	} else if len(result.Symbols) == 0 {
		fmt.Fprintln(w, "  (none — or graph not indexed)")
	} else {
		for _, s := range result.Symbols {
			name, _ := s["name"].(string)
			kind, _ := s["kind"].(string)
			if kind != "" {
				fmt.Fprintf(w, "  %-10s %s\n", kind, name)
			} else {
				fmt.Fprintf(w, "  %s\n", name)
			}
		}
	}

	if len(result.Decisions) > 0 {
		fmt.Fprintf(w, "\nRelated Decisions (%d):\n", len(result.Decisions))
		for _, dec := range result.Decisions {
			fmt.Fprintf(w, "  • %s\n", dec.Text)
			if dec.Reason != "" {
				fmt.Fprintf(w, "    Reason: %s\n", dec.Reason)
			}
		}
	}

	if len(result.Tasks) > 0 {
		fmt.Fprintf(w, "\nRelated Tasks (%d):\n", len(result.Tasks))
		for _, t := range result.Tasks {
			fmt.Fprintf(w, "  %s [%s] %s\n", taskStatusIcon(t.Status), t.ID, t.Title)
		}
	}

	if len(result.Rejections) > 0 {
		fmt.Fprintf(w, "\nRelated Rejections (%d):\n", len(result.Rejections))
		for _, r := range result.Rejections {
			fmt.Fprintf(w, "  ✗ %s\n", r.Approach)
			if r.Reason != "" {
				fmt.Fprintf(w, "    Reason: %s\n", r.Reason)
			}
		}
	}

	if len(result.HistoricalBugs) > 0 {
		fmt.Fprintf(w, "\nHistorical Bugs (%d):\n", len(result.HistoricalBugs))
		for _, b := range result.HistoricalBugs {
			fmt.Fprintf(w, "  %s %s\n", b.BugID, b.Title)
		}
	}

	if len(result.Warnings) > 0 {
		fmt.Fprintln(w, "\nWarnings:")
		for _, warn := range result.Warnings {
			fmt.Fprintf(w, "  ~ %s\n", warn)
		}
	}
}

func impactHasCodeGraphWarning(warnings []string) bool {
	for _, warning := range warnings {
		lower := strings.ToLower(warning)
		if strings.HasPrefix(lower, "codegraph:") ||
			strings.HasPrefix(lower, "codegraph for ") ||
			strings.Contains(lower, "code graph stale or missing") ||
			strings.Contains(lower, "code graph missing/stale") ||
			strings.Contains(lower, "code graph freshness unknown") ||
			strings.Contains(warning, "graph is empty") ||
			strings.Contains(warning, "graph data unavailable") ||
			strings.Contains(warning, "graph client init:") ||
			strings.Contains(warning, "dependents query:") ||
			strings.Contains(warning, "symbols query:") ||
			strings.Contains(warning, "historical bugs query:") {
			return true
		}
	}
	return false
}
