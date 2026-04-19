package cmd

import (
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
works even without Memgraph.`,
	Args: cobra.ExactArgs(1),
	RunE: runImpact,
}

var impactKBLimit uint64
var impactCompact bool

func init() {
	impactCmd.Flags().Uint64Var(&impactKBLimit, "kb-limit", 5, "max results per knowledge-base collection")
	impactCmd.Flags().BoolVar(&impactCompact, "compact", false, "one line per item — drops symbol kinds and reasons to preserve agent context window")
	rootCmd.AddCommand(impactCmd)
}

type impactResult struct {
	File            string            `json:"file"`
	Dependents      []string          `json:"dependents"`
	Symbols         []map[string]any  `json:"symbols"`
	Decisions       []store.Decision  `json:"decisions"`
	Tasks           []store.Task      `json:"tasks"`
	Rejections      []store.Rejection `json:"rejections"`
	HistoricalBugs  []graph.BugRef    `json:"historical_bugs,omitempty"`
	Warnings        []string          `json:"warnings,omitempty"`
}

func runImpact(cmd *cobra.Command, args []string) error {
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

	result := impactResult{File: relPath}

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

			// 0. Detect empty graph — Memgraph is up but nothing has been indexed yet.
			if fileCount, fcErr := gc.CountFileNodes(gctx); fcErr == nil && fileCount == 0 {
				result.Warnings = append(result.Warnings, "graph is empty — run 'gg index --lang <lang>' to populate it (e.g. 'gg index --lang go')")
			}

			// 1. Downstream dependents.
			deps, depErr := gc.DependentsOf(gctx, relPath)
			if depErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("dependents query: %v", depErr))
			} else {
				result.Dependents = deps
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
			result.Decisions, decErr = d.store.SearchDecisions(ctx, vector, impactKBLimit)
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
		if impactCompact {
			emitCompact(cmd, "impact",
				func(w io.Writer) { renderImpactDefault(w, result) },
				func(w io.Writer) { renderImpactCompact(w, result) },
			)
			return
		}
		renderImpactDefault(os.Stdout, result)
	})
}

func renderImpactDefault(w io.Writer, result impactResult) {
	fmt.Fprintf(w, "Impact: %s\n", result.File)
	fmt.Fprintln(w, strings.Repeat("─", 60))

	fmt.Fprintf(w, "\nDownstream Dependents (%d):\n", len(result.Dependents))
	if len(result.Dependents) == 0 {
		fmt.Fprintln(w, "  (none — or graph not indexed)")
	} else {
		for _, dep := range result.Dependents {
			fmt.Fprintf(w, "  → %s\n", dep)
		}
	}

	fmt.Fprintf(w, "\nExported Symbols (%d):\n", len(result.Symbols))
	if len(result.Symbols) == 0 {
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

func renderImpactCompact(w io.Writer, r impactResult) {
	fmt.Fprintf(w, "impact: %s — %d deps %d sym %dD %dT %dR %dB\n\n",
		r.File, len(r.Dependents), len(r.Symbols),
		len(r.Decisions), len(r.Tasks), len(r.Rejections), len(r.HistoricalBugs))
	for _, dep := range r.Dependents {
		fmt.Fprintf(w, "→ %s\n", dep)
	}
	for _, s := range r.Symbols {
		name, _ := s["name"].(string)
		if name != "" {
			fmt.Fprintf(w, "S %s\n", name)
		}
	}
	for _, dec := range r.Decisions {
		suffix := ""
		if dec.TaskID != "" {
			suffix = " →" + dec.TaskID
		}
		fmt.Fprintf(w, "D  %s  %s%s\n",
			shortDate(dec.CreatedAt), compactTrim(dec.Text, compactLineWidth), suffix)
	}
	for _, t := range r.Tasks {
		fmt.Fprintf(w, "T %s %s  %s (%s)\n",
			taskStatusIcon(t.Status), t.ID, compactTrim(t.Title, compactLineWidth), t.Priority)
	}
	for _, rej := range r.Rejections {
		suffix := ""
		if rej.TaskID != "" {
			suffix = " →" + rej.TaskID
		}
		fmt.Fprintf(w, "R  %s  %s%s\n",
			shortDate(rej.CreatedAt), compactTrim(rej.Approach, compactLineWidth), suffix)
	}
	for _, b := range r.HistoricalBugs {
		fmt.Fprintf(w, "B  %s  %s\n", b.BugID, compactTrim(b.Title, compactLineWidth))
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintf(w, "\n! %s\n", strings.Join(r.Warnings, "; "))
	}
}

// runImpactBug handles `gg impact BUG-NNN` — shows what files/symbols a bug affects.
func runImpactBug(cmd *cobra.Command, bugID string) error {
	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	type bugImpactResult struct {
		BugID      string            `json:"bug_id"`
		Files      []string          `json:"files"`
		Symbols    []string          `json:"symbols"`
		Decisions  []store.Decision  `json:"decisions"`
		Tasks      []store.Task      `json:"tasks"`
		Rejections []store.Rejection `json:"rejections"`
		Warnings   []string          `json:"warnings,omitempty"`
	}

	result := bugImpactResult{BugID: bugID}

	// Graph: get affected files + symbols.
	cfg, _ := config.Load()
	if cfg != nil && cfg.Memgraph.URI != "" {
		gc, gcErr := graph.New(&cfg.Memgraph, cfg.ProjectID)
		if gcErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("graph client init: %v", gcErr))
		} else {
			defer func() { _ = gc.Close(ctx) }()
			gctx, gcancel := withTimeout(cmd.Context())
			defer gcancel()
			files, symbols, affErr := gc.BugAffects(gctx, bugID)
			if affErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("bug affects query: %v", affErr))
			} else {
				result.Files = files
				result.Symbols = symbols
			}
		}
	} else {
		result.Warnings = append(result.Warnings, "Memgraph not configured — graph data unavailable")
	}

	// Semantic KB search for decisions/tasks/rejections related to this bug ID.
	vector, embErr := d.embedder.Generate(ctx, bugID)
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
		fmt.Printf("Impact: %s\n", bugID)
		fmt.Fprintln(os.Stdout, strings.Repeat("─", 60))
		fmt.Fprintf(os.Stdout, "\nAffected Files (%d):\n", len(result.Files))
		if len(result.Files) == 0 {
			fmt.Fprintln(os.Stdout, "  (none recorded — use gg bug report --files or gg bug fix --files)")
		} else {
			for _, f := range result.Files {
				fmt.Fprintf(os.Stdout, "  → %s\n", f)
			}
		}
		fmt.Fprintf(os.Stdout, "\nAffected Symbols (%d):\n", len(result.Symbols))
		if len(result.Symbols) == 0 {
			fmt.Fprintln(os.Stdout, "  (none recorded)")
		} else {
			for _, s := range result.Symbols {
				fmt.Fprintf(os.Stdout, "  S %s\n", s)
			}
		}
		if impactCompact {
			fmt.Fprintf(os.Stdout, "\n%s — %d files %d sym %dD %dT %dR\n",
				bugID, len(result.Files), len(result.Symbols),
				len(result.Decisions), len(result.Tasks), len(result.Rejections))
		}
		if len(result.Warnings) > 0 {
			fmt.Fprintln(os.Stdout, "\nWarnings:")
			for _, w := range result.Warnings {
				fmt.Fprintf(os.Stdout, "  ~ %s\n", w)
			}
		}
	})
}

// runImpactTask handles `gg impact TASK-NNN` — shows which other tasks
// depend on or are blocked by this one (Memgraph DEPENDS_ON / BLOCKS edges
// shipped with TASK-233), plus semantically related decisions/tasks from
// the knowledge store.
func runImpactTask(cmd *cobra.Command, taskID string) error {
	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	type taskImpactResult struct {
		TaskID       string            `json:"task_id"`
		Dependents   []string          `json:"dependents"`
		Siblings     []string          `json:"siblings"`
		Decisions    []store.Decision  `json:"decisions"`
		Tasks        []store.Task      `json:"tasks"`
		Bugs         []store.Bug       `json:"bugs"`
		Rejections   []store.Rejection `json:"rejections"`
		Warnings     []string          `json:"warnings,omitempty"`
	}

	result := taskImpactResult{TaskID: taskID}

	cfg, _ := config.Load()
	if cfg != nil && cfg.Memgraph.URI != "" {
		gc, gcErr := graph.New(&cfg.Memgraph, cfg.ProjectID)
		if gcErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("graph client init: %v", gcErr))
		} else {
			defer func() { _ = gc.Close(ctx) }()
			gctx, gcancel := withTimeout(cmd.Context())
			defer gcancel()
			deps, depErr := gc.TaskDependents(gctx, taskID)
			if depErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("task dependents query: %v", depErr))
			} else {
				result.Dependents = deps
			}
			siblings, sibErr := gc.TaskSiblings(gctx, taskID)
			if sibErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("task siblings query: %v", sibErr))
			} else {
				result.Siblings = siblings
			}
		}
	} else {
		result.Warnings = append(result.Warnings, "Memgraph not configured — graph data unavailable")
	}

	vector, embErr := d.embedder.Generate(ctx, taskID)
	if embErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("embedding: %v", embErr))
	} else {
		var wg sync.WaitGroup
		var decErr, taskErr, bugErr, rejErr error
		wg.Add(4)
		go func() {
			defer wg.Done()
			result.Decisions, decErr = d.store.SearchDecisions(ctx, vector, impactKBLimit)
		}()
		go func() {
			defer wg.Done()
			result.Tasks, taskErr = d.store.SearchTasks(ctx, vector, impactKBLimit, true)
		}()
		go func() {
			defer wg.Done()
			result.Bugs, bugErr = d.store.SearchBugs(ctx, vector, impactKBLimit)
		}()
		go func() {
			defer wg.Done()
			result.Rejections, rejErr = d.store.SearchRejections(ctx, vector, impactKBLimit)
		}()
		wg.Wait()
		for _, e := range []error{decErr, taskErr, bugErr, rejErr} {
			if e != nil {
				result.Warnings = append(result.Warnings, e.Error())
			}
		}
	}

	return printJSON(result, func() {
		fmt.Printf("Impact: %s\n", taskID)
		fmt.Fprintln(os.Stdout, strings.Repeat("─", 60))
		fmt.Fprintf(os.Stdout, "\nDownstream Dependents (%d):\n", len(result.Dependents))
		if len(result.Dependents) == 0 {
			fmt.Fprintln(os.Stdout, "  (none — no other task depends on or is blocked by this one)")
		} else {
			for _, id := range result.Dependents {
				fmt.Fprintf(os.Stdout, "  → %s\n", id)
			}
		}
		if len(result.Siblings) > 0 {
			fmt.Fprintf(os.Stdout, "\nSibling Tasks via Shared Decisions (%d):\n", len(result.Siblings))
			for _, id := range result.Siblings {
				fmt.Fprintf(os.Stdout, "  ~ %s\n", id)
			}
		}
		if len(result.Decisions) > 0 {
			fmt.Fprintf(os.Stdout, "\nRelated Decisions (%d):\n", len(result.Decisions))
			for _, dec := range result.Decisions {
				fmt.Fprintf(os.Stdout, "  • %s\n", compactTrim(dec.Text, compactLineWidth))
			}
		}
		if len(result.Tasks) > 0 {
			fmt.Fprintf(os.Stdout, "\nRelated Tasks (%d):\n", len(result.Tasks))
			for _, t := range result.Tasks {
				fmt.Fprintf(os.Stdout, "  T %s — %s\n", t.ID, compactTrim(t.Title, compactLineWidth))
			}
		}
		if len(result.Bugs) > 0 {
			fmt.Fprintf(os.Stdout, "\nRelated Bugs (%d):\n", len(result.Bugs))
			for _, b := range result.Bugs {
				fmt.Fprintf(os.Stdout, "  %s [%s] %s\n", b.ID, b.Status, compactTrim(b.Title, compactLineWidth))
			}
		}
		if impactCompact {
			fmt.Fprintf(os.Stdout, "\n%s — %d downstream %d siblings %dD %dT %dB %dR\n",
				taskID, len(result.Dependents), len(result.Siblings),
				len(result.Decisions), len(result.Tasks), len(result.Bugs), len(result.Rejections))
		}
		if len(result.Warnings) > 0 {
			fmt.Fprintln(os.Stdout, "\nWarnings:")
			for _, w := range result.Warnings {
				fmt.Fprintf(os.Stdout, "  ~ %s\n", w)
			}
		}
	})
}
