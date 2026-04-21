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

// taskImpactResult is the rendered payload for `gg impact TASK-NNN` —
// shared by the default and compact renderers.
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
		if isCompactActive(cmd) {
			emitCompact(cmd, "impact",
				func(w io.Writer) { renderTaskImpactDefault(w, result) },
				func(w io.Writer) { renderTaskImpactCompact(w, result) },
			)
			return
		}
		renderTaskImpactDefault(os.Stdout, result)
	})
}

func renderTaskImpactDefault(w io.Writer, result taskImpactResult) {
	fmt.Fprintf(w, "Impact: %s\n", result.TaskID)
	fmt.Fprintln(w, strings.Repeat("─", 60))
	fmt.Fprintf(w, "\nDownstream Dependents (%d):\n", len(result.Dependents))
	if len(result.Dependents) == 0 {
		fmt.Fprintln(w, "  (none — no other task depends on or is blocked by this one)")
	} else {
		for _, id := range result.Dependents {
			fmt.Fprintf(w, "  → %s\n", id)
		}
	}
	if len(result.Siblings) > 0 {
		fmt.Fprintf(w, "\nSibling Tasks via Shared Decisions (%d):\n", len(result.Siblings))
		for _, id := range result.Siblings {
			fmt.Fprintf(w, "  ~ %s\n", id)
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
	if len(result.Bugs) > 0 {
		fmt.Fprintf(w, "\nRelated Bugs (%d):\n", len(result.Bugs))
		for _, b := range result.Bugs {
			fmt.Fprintf(w, "  %s [%s/%s] %s\n", b.ID, b.Severity, b.Status, b.Title)
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

func renderTaskImpactCompact(w io.Writer, r taskImpactResult) {
	fmt.Fprintf(w, "impact: %s — %d deps %d sib %dD %dT %dB %dR\n\n",
		r.TaskID, len(r.Dependents), len(r.Siblings),
		len(r.Decisions), len(r.Tasks), len(r.Bugs), len(r.Rejections))
	for _, id := range r.Dependents {
		fmt.Fprintf(w, "→ %s\n", id)
	}
	for _, id := range r.Siblings {
		fmt.Fprintf(w, "~ %s\n", id)
	}
	writeCompactDecisions(w, r.Decisions)
	writeCompactTasks(w, r.Tasks)
	writeCompactBugs(w, r.Bugs)
	writeCompactRejections(w, r.Rejections)
	if len(r.Warnings) > 0 {
		fmt.Fprintf(w, "\n! %s\n", strings.Join(r.Warnings, "; "))
	}
}
