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
		TaskID     string            `json:"task_id"`
		Dependents []string          `json:"dependents"`
		Siblings   []string          `json:"siblings"`
		Decisions  []store.Decision  `json:"decisions"`
		Tasks      []store.Task      `json:"tasks"`
		Bugs       []store.Bug       `json:"bugs"`
		Rejections []store.Rejection `json:"rejections"`
		Warnings   []string          `json:"warnings,omitempty"`
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
