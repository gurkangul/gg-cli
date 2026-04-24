package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

// bugImpactResult is the rendered payload for `gg impact BUG-NNN` —
// shared by the default and compact renderers.
type bugImpactResult struct {
	BugID      string            `json:"bug_id"`
	Files      []string          `json:"files"`
	Symbols    []string          `json:"symbols"`
	Decisions  []store.Decision  `json:"decisions"`
	Tasks      []store.Task      `json:"tasks"`
	Rejections []store.Rejection `json:"rejections"`
	Warnings   []string          `json:"warnings,omitempty"`
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

	result := bugImpactResult{BugID: bugID}

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
		if isCompactActive(cmd) {
			emitCompact(cmd, "impact",
				func(w io.Writer) { renderBugImpactDefault(w, result) },
				func(w io.Writer) { renderBugImpactCompact(w, result) },
				compactRendererV_impact,
			)
			return
		}
		renderBugImpactDefault(os.Stdout, result)
	})
}

func renderBugImpactDefault(w io.Writer, result bugImpactResult) {
	fmt.Fprintf(w, "Impact: %s\n", result.BugID)
	fmt.Fprintln(w, strings.Repeat("─", 60))
	fmt.Fprintf(w, "\nAffected Files (%d):\n", len(result.Files))
	if len(result.Files) == 0 {
		fmt.Fprintln(w, "  (none recorded — use gg bug report --files or gg bug fix --files)")
	} else {
		for _, f := range result.Files {
			fmt.Fprintf(w, "  → %s\n", f)
		}
	}
	fmt.Fprintf(w, "\nAffected Symbols (%d):\n", len(result.Symbols))
	if len(result.Symbols) == 0 {
		fmt.Fprintln(w, "  (none recorded)")
	} else {
		for _, s := range result.Symbols {
			fmt.Fprintf(w, "  S %s\n", s)
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
			fmt.Fprintf(w, "  %s %s [%s] %s\n", taskStatusIcon(t.Status), t.ID, t.Priority, t.Title)
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
	if len(result.Warnings) > 0 {
		fmt.Fprintln(w, "\nWarnings:")
		for _, warn := range result.Warnings {
			fmt.Fprintf(w, "  ~ %s\n", warn)
		}
	}
}

func renderBugImpactCompact(w io.Writer, r bugImpactResult) {
	fmt.Fprintf(w, "impact: %s — %d files %d sym %dD %dT %dR\n\n",
		r.BugID, len(r.Files), len(r.Symbols),
		len(r.Decisions), len(r.Tasks), len(r.Rejections))
	for _, f := range r.Files {
		fmt.Fprintf(w, "→ %s\n", f)
	}
	for _, s := range r.Symbols {
		fmt.Fprintf(w, "S %s\n", s)
	}
	writeCompactDecisions(w, r.Decisions)
	writeCompactTasks(w, r.Tasks)
	writeCompactRejections(w, r.Rejections)
	if len(r.Warnings) > 0 {
		fmt.Fprintf(w, "\n! %s\n", strings.Join(r.Warnings, "; "))
	}
}
