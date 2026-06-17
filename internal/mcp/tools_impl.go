package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

// ── gg_search ─────────────────────────────────────────────────────────────

func (b *brain) toolSearch(ctx context.Context, args map[string]any) ([]ContentBlock, bool) {
	query := argString(args, "query")
	if query == "" {
		return TextBlock(`gg_search requires a non-empty "query"`), true
	}
	vector, err := b.embedder.Generate(ctx, query)
	if err != nil {
		return TextBlock("embedding failed (is the embedding backend reachable?): " + err.Error()), true
	}

	limit := uint64(defaultLimit)
	var (
		decisions  []store.Decision
		rejections []store.Rejection
		tasks      []store.Task
		bugs       []store.Bug
		notes      []store.Note
	)
	var decErr, rejErr, taskErr, bugErr, noteErr error
	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		decisions, decErr = fallbackSearch(b.ggDir, "decisions", query,
			func() ([]store.Decision, error) { return b.store.SearchDecisions(ctx, vector, limit, false) },
			decisionFromEntry)
	}()
	go func() {
		defer wg.Done()
		rejections, rejErr = fallbackSearch(b.ggDir, "rejections", query,
			func() ([]store.Rejection, error) { return b.store.SearchRejections(ctx, vector, limit) },
			rejectionFromEntry)
	}()
	go func() {
		defer wg.Done()
		tasks, taskErr = fallbackSearch(b.ggDir, "tasks", query,
			func() ([]store.Task, error) { return b.store.SearchTasks(ctx, vector, limit, false) },
			taskFromEntry)
	}()
	go func() {
		defer wg.Done()
		bugs, bugErr = fallbackSearch(b.ggDir, "bugs", query,
			func() ([]store.Bug, error) { return b.store.SearchBugs(ctx, vector, limit, false) },
			bugFromEntry)
	}()
	go func() {
		defer wg.Done()
		notes, noteErr = fallbackSearch(b.ggDir, "notes", query,
			func() ([]store.Note, error) { return b.store.SearchNotes(ctx, vector, limit) },
			noteFromEntry)
	}()
	wg.Wait()
	if msg := firstSearchError(decErr, rejErr, taskErr, bugErr, noteErr); msg != "" {
		return TextBlock("gg_search: " + msg), true
	}

	payload := map[string]any{
		"query":      query,
		"decisions":  decisions,
		"rejections": rejections,
		"tasks":      tasks,
		"bugs":       bugs,
		"notes":      notes,
	}
	if argBool(args, "compact") {
		payload["counts"] = map[string]int{
			"decisions": len(decisions), "rejections": len(rejections),
			"tasks": len(tasks), "bugs": len(bugs), "notes": len(notes),
		}
	}
	return jsonContent(payload), false
}

// ── gg_context ────────────────────────────────────────────────────────────

func (b *brain) toolContext(ctx context.Context, args map[string]any) ([]ContentBlock, bool) {
	compact := argBool(args, "compact")
	if forTask := argString(args, "for_task"); forTask != "" {
		return b.contextForTask(ctx, forTask, compact)
	}
	if query := argString(args, "query"); query != "" {
		return b.contextTopic(ctx, query, compact)
	}
	return b.contextProject(ctx, compact)
}

// contextForTask returns the task, its dependency tasks, and semantically
// related decisions/rejections — the task-scoped rehydration bundle.
func (b *brain) contextForTask(ctx context.Context, taskID string, compact bool) ([]ContentBlock, bool) {
	if _, err := store.ParseTaskID(taskID); err != nil {
		return TextBlock(fmt.Sprintf("invalid task id %q: %v", taskID, err)), true
	}
	t, err := b.store.GetTask(ctx, taskID)
	if err != nil {
		return TextBlock(err.Error()), true
	}
	var deps []store.Task
	for _, dep := range t.DependsOn {
		if dt, derr := b.store.GetTask(ctx, dep); derr == nil && dt != nil {
			deps = append(deps, *dt)
		}
	}
	var decisions []store.Decision
	var rejections []store.Rejection
	query := t.Title + " " + t.Detail
	if vector, embErr := b.embedder.Generate(ctx, query); embErr == nil {
		var decErr, rejErr error
		decisions, decErr = fallbackSearch(b.ggDir, "decisions", query,
			func() ([]store.Decision, error) { return b.store.SearchDecisions(ctx, vector, defaultLimit, false) },
			decisionFromEntry)
		rejections, rejErr = fallbackSearch(b.ggDir, "rejections", query,
			func() ([]store.Rejection, error) { return b.store.SearchRejections(ctx, vector, defaultLimit) },
			rejectionFromEntry)
		if msg := firstSearchError(decErr, rejErr); msg != "" {
			return TextBlock("gg_context: " + msg), true
		}
	}
	payload := map[string]any{
		"task":         t,
		"dependencies": deps,
		"decisions":    decisions,
		"rejections":   rejections,
	}
	if compact {
		payload["counts"] = map[string]int{
			"dependencies": len(deps), "decisions": len(decisions), "rejections": len(rejections),
		}
	}
	return jsonContent(payload), false
}

// contextTopic returns a semantic bundle across decisions, rejections, tasks,
// discussions, and notes for a free-text topic.
func (b *brain) contextTopic(ctx context.Context, query string, compact bool) ([]ContentBlock, bool) {
	vector, err := b.embedder.Generate(ctx, query)
	if err != nil {
		return TextBlock("embedding failed (is the embedding backend reachable?): " + err.Error()), true
	}
	limit := uint64(defaultLimit)
	var (
		decisions   []store.Decision
		rejections  []store.Rejection
		tasks       []store.Task
		discussions []store.Discussion
		notes       []store.Note
	)
	var decErr, rejErr, taskErr, discErr, noteErr error
	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		decisions, decErr = fallbackSearch(b.ggDir, "decisions", query,
			func() ([]store.Decision, error) { return b.store.SearchDecisions(ctx, vector, limit, false) },
			decisionFromEntry)
	}()
	go func() {
		defer wg.Done()
		rejections, rejErr = fallbackSearch(b.ggDir, "rejections", query,
			func() ([]store.Rejection, error) { return b.store.SearchRejections(ctx, vector, limit) },
			rejectionFromEntry)
	}()
	go func() {
		defer wg.Done()
		tasks, taskErr = fallbackSearch(b.ggDir, "tasks", query,
			func() ([]store.Task, error) { return b.store.SearchTasks(ctx, vector, limit, false) },
			taskFromEntry)
	}()
	go func() {
		defer wg.Done()
		discussions, discErr = fallbackSearch(b.ggDir, "discussions", query,
			func() ([]store.Discussion, error) { return b.store.SearchDiscussions(ctx, vector, limit, false) },
			discussionFromEntry)
	}()
	go func() {
		defer wg.Done()
		notes, noteErr = fallbackSearch(b.ggDir, "notes", query,
			func() ([]store.Note, error) { return b.store.SearchNotes(ctx, vector, limit) },
			noteFromEntry)
	}()
	wg.Wait()
	if msg := firstSearchError(decErr, rejErr, taskErr, discErr, noteErr); msg != "" {
		return TextBlock("gg_context: " + msg), true
	}
	payload := map[string]any{
		"query":       query,
		"decisions":   decisions,
		"rejections":  rejections,
		"tasks":       tasks,
		"discussions": discussions,
		"notes":       notes,
	}
	if compact {
		payload["counts"] = map[string]int{
			"decisions": len(decisions), "rejections": len(rejections),
			"tasks": len(tasks), "discussions": len(discussions), "notes": len(notes),
		}
	}
	return jsonContent(payload), false
}

// contextProject returns a project onboarding overview: recent decisions,
// rejections, active tasks, open bugs, and the canon.
func (b *brain) contextProject(ctx context.Context, compact bool) ([]ContentBlock, bool) {
	decisions, _ := b.store.ListDecisions(ctx, defaultLimit, false)
	rejections, _ := b.store.ListRejections(ctx, defaultLimit)
	tasks, _ := b.store.ListTasks(ctx, "pending")
	bugs, _ := b.store.ListBugs(ctx, "open")
	canon, _ := b.store.ListCanon()
	payload := map[string]any{
		"decisions":  decisions,
		"rejections": rejections,
		"tasks":      tasks,
		"bugs":       bugs,
		"canon":      canon,
	}
	if compact {
		payload["counts"] = map[string]int{
			"decisions": len(decisions), "rejections": len(rejections),
			"tasks": len(tasks), "bugs": len(bugs), "canon": len(canon),
		}
	}
	return jsonContent(payload), false
}

// ── gg_impact ─────────────────────────────────────────────────────────────

func (b *brain) toolImpact(ctx context.Context, args map[string]any) ([]ContentBlock, bool) {
	target := argString(args, "target")
	if target == "" {
		return TextBlock(`gg_impact requires a "target" (file path, BUG-NNN, or TASK-NNN)`), true
	}

	// Knowledge-base search uses the target basename so file paths still match
	// related decisions/tasks/rejections semantically (mirrors cmd/impact.go).
	searchQuery := target
	if _, err := store.ParseBugID(target); err != nil {
		if _, err := store.ParseTaskID(target); err != nil {
			searchQuery = filepath.Base(target)
		}
	}

	result := map[string]any{"target": target}

	// Graph dependents/symbols are optional — they require gg index to have run.
	gc, gcErr := graph.New(b.cfg.DataDir, b.cfg.ProjectID)
	if gcErr == nil {
		defer func() { _ = gc.Close(ctx) }()
		hops := argInt(args, "hops", 1)
		if hops < 1 {
			hops = 1
		}
		if hops == 1 {
			if deps, err := gc.DependentsOf(ctx, target); err == nil {
				result["dependents"] = deps
			}
		} else {
			if trav, err := gc.DependentsOfDepth(ctx, target, hops); err == nil {
				paths := make([]string, 0, len(trav.Dependents))
				for _, d := range trav.Dependents {
					paths = append(paths, d.Path)
				}
				result["dependents"] = paths
			}
		}
		if syms, err := gc.FileSymbols(ctx, target); err == nil {
			names := make([]map[string]any, 0, len(syms))
			for _, n := range syms {
				names = append(names, n.Properties)
			}
			result["symbols"] = names
		}
		if bugs, err := gc.BugsAffectingFile(ctx, target); err == nil {
			result["historical_bugs"] = bugs
		}
	}

	// Knowledge-base semantic search.
	if vector, err := b.embedder.Generate(ctx, searchQuery); err == nil {
		var (
			decisions  []store.Decision
			tasks      []store.Task
			rejections []store.Rejection
		)
		var decErr, taskErr, rejErr error
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			decisions, decErr = fallbackSearch(b.ggDir, "decisions", searchQuery,
				func() ([]store.Decision, error) { return b.store.SearchDecisions(ctx, vector, defaultLimit, true) },
				decisionFromEntry)
		}()
		go func() {
			defer wg.Done()
			tasks, taskErr = fallbackSearch(b.ggDir, "tasks", searchQuery,
				func() ([]store.Task, error) { return b.store.SearchTasks(ctx, vector, defaultLimit, true) },
				taskFromEntry)
		}()
		go func() {
			defer wg.Done()
			rejections, rejErr = fallbackSearch(b.ggDir, "rejections", searchQuery,
				func() ([]store.Rejection, error) { return b.store.SearchRejections(ctx, vector, defaultLimit) },
				rejectionFromEntry)
		}()
		wg.Wait()
		if msg := firstSearchError(decErr, taskErr, rejErr); msg != "" {
			return TextBlock("gg_impact: " + msg), true
		}
		result["decisions"] = decisions
		result["tasks"] = tasks
		result["rejections"] = rejections
	}

	return jsonContent(result), false
}

// ── gg_canon ──────────────────────────────────────────────────────────────

func (b *brain) toolCanon(ctx context.Context, args map[string]any) ([]ContentBlock, bool) {
	manual, _ := b.store.ListCanon()

	// Auto-derived canon is computed live from the ledger (no manual upkeep).
	decs, _ := b.store.ListDecisions(ctx, 0, false)
	rejs, _ := b.store.ListRejections(ctx, 0)
	bugs, _ := b.store.ListBugs(ctx, "fixed")
	tasks, _ := b.store.ListTasks(ctx, "")
	var auto []store.CanonEntry
	if argBool(args, "compact") {
		auto = store.BuildAutoCanonCompact(decs, rejs, bugs, tasks)
	} else {
		auto = store.BuildAutoCanon(decs, rejs, bugs, tasks)
	}

	combined := append(append([]store.CanonEntry{}, manual...), auto...)
	return jsonContent(map[string]any{
		"canon":   combined,
		"curated": manual,
		"auto":    auto,
	}), false
}

// ── gg_task_get / gg_bug_get ──────────────────────────────────────────────

func (b *brain) toolTaskGet(ctx context.Context, args map[string]any) ([]ContentBlock, bool) {
	id := argString(args, "id")
	if id == "" {
		return TextBlock(`gg_task_get requires an "id" (e.g. TASK-501)`), true
	}
	if _, err := store.ParseTaskID(id); err != nil {
		return TextBlock(fmt.Sprintf("invalid task id %q: %v", id, err)), true
	}
	t, err := b.store.GetTask(ctx, id)
	if err != nil {
		return TextBlock(err.Error()), true
	}
	return jsonContent(t), false
}

func (b *brain) toolBugGet(ctx context.Context, args map[string]any) ([]ContentBlock, bool) {
	id := argString(args, "id")
	if id == "" {
		return TextBlock(`gg_bug_get requires an "id" (e.g. BUG-084)`), true
	}
	if _, err := store.ParseBugID(id); err != nil {
		return TextBlock(fmt.Sprintf("invalid bug id %q: %v", id, err)), true
	}
	bug, err := b.store.GetBug(ctx, id)
	if err != nil {
		return TextBlock(err.Error()), true
	}
	return jsonContent(bug), false
}
