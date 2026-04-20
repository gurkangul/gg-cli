package cmd

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

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
