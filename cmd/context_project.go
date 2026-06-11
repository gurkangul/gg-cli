package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

const projectContextQuery = "project"

func runProjectContext(cmd *cobra.Command) error {
	d, err := loadDepsReadOnly(true)
	if err != nil {
		return err
	}
	defer d.Close()

	if d.qdrantSlow {
		return fmt.Errorf("%s", withServiceHint("qdrant health check timed out — Qdrant may be overloaded; retry or check qdrant status", svcQdrant))
	}
	if d.qdrantDown {
		return serveContextFromCache(cmd, projectContextQuery)
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	limit := projectContextLimit()
	var bundle contextBundle
	// TASK-469: pinned decisions surface first, regardless of age, so important
	// decisions are never buried by recency. Then fill with recent active ones.
	var decs []store.Decision
	seenDec := map[string]bool{}
	if pinned, perr := d.store.ListPinnedDecisions(ctx); perr == nil {
		for _, p := range pinned {
			if !seenDec[p.ID] {
				seenDec[p.ID] = true
				decs = append(decs, p)
			}
		}
	}
	if recent, err := d.store.ListDecisions(ctx, limit, false); err == nil {
		for _, r := range recent {
			if !seenDec[r.ID] {
				seenDec[r.ID] = true
				decs = append(decs, r)
			}
		}
		bundle.decisions = store.FilterDecisionNoise(decs)
	} else if len(decs) > 0 {
		bundle.decisions = store.FilterDecisionNoise(decs)
	} else {
		bundle.decErr = err
	}
	if rejections, err := d.store.ListRejections(ctx, limit); err == nil {
		bundle.rejections = rejections
	} else {
		bundle.rejErr = err
	}
	if tasks, err := d.store.ListTasks(ctx, ""); err == nil {
		bundle.tasks = projectContextTasks(tasks, limit, contextIncludeResolved)
	} else {
		bundle.taskErr = err
	}
	statusFilter := "open"
	if contextIncludeResolved {
		statusFilter = ""
	}
	if discussions, err := d.store.ListDiscussions(ctx, statusFilter); err == nil {
		bundle.discussions = trimProjectDiscussions(discussions, limit)
	} else {
		bundle.discErr = err
	}
	if notes, err := d.store.ListNotes(ctx, limit); err == nil {
		bundle.notes = filterFixtureNotes(notes)
	} else {
		bundle.noteErr = err
	}
	if artifacts, err := searchContextArtifacts(projectContextQuery, contextLimit); err == nil {
		bundle.artifacts = artifacts
	} else {
		bundle.artifactErr = err
	}

	errs := collectBundleErrors(bundle)
	total := len(bundle.decisions) + len(bundle.rejections) + len(bundle.tasks) + len(bundle.discussions) + len(bundle.notes) + len(bundle.artifacts)
	if total == 0 && len(errs) > 0 {
		return fmt.Errorf("project context failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return printContextBundle(cmd, projectContextQuery, bundle, errs, time.Time{})
}

func projectContextLimit() int {
	if contextLimit == 0 {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	if contextLimit > uint64(maxInt) {
		return maxInt
	}
	return int(contextLimit)
}

func projectContextTasks(tasks []store.Task, limit int, includeResolved bool) []store.Task {
	filtered := make([]store.Task, 0, len(tasks))
	for _, task := range tasks {
		if includeResolved || projectContextTaskStatus(task.Status) {
			filtered = append(filtered, task)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		li, lj := projectTaskRank(filtered[i]), projectTaskRank(filtered[j])
		if li != lj {
			return li < lj
		}
		ni, _ := store.ParseTaskID(filtered[i].ID)
		nj, _ := store.ParseTaskID(filtered[j].ID)
		return ni < nj
	})
	if limit > 0 && len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}

func projectContextTaskStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "pending", "in_progress", "ready_for_live", "blocked":
		return true
	default:
		return false
	}
}

func projectTaskRank(task store.Task) int {
	switch task.Status {
	case "in_progress":
		return 0
	case "ready_for_live":
		return 1
	case "blocked":
		return 2
	case "pending":
		return 3
	}
	switch task.Priority {
	case "high":
		return 4
	case "medium":
		return 5
	case "low":
		return 6
	default:
		return 7
	}
}

func trimProjectDiscussions(in []store.Discussion, limit int) []store.Discussion {
	if limit > 0 && len(in) > limit {
		return in[:limit]
	}
	return in
}

// filterFixtureNotes drops notes that are clearly test/scrubber scaffolding so
// they don't pollute a fresh agent's project orientation. These markers are
// unambiguous gg-internal fixtures (secret-scanner canaries, smoke-test
// banners); real project notes never carry them. Scoped to the project-overview
// context — `gg search`/`gg context <topic>` still return everything.
func filterFixtureNotes(notes []store.Note) []store.Note {
	markers := []string{"smoke-test", "do-not-use", "scrubber verification", "sk-test-aaaa"}
	out := notes[:0]
	for _, n := range notes {
		body := strings.ToLower(n.Text)
		drop := strings.TrimSpace(body) == "test banner"
		for _, m := range markers {
			if strings.Contains(body, m) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, n)
		}
	}
	return out
}
