package mcp

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

// This file holds the read-only code-graph MCP tools (gg_impact, gg_def,
// gg_uses) — the navigation surface that lets an agent reach for the graph
// instead of grep. The brain/knowledge tools live in tools_impl.go.

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
	var warnings []string

	// Graph dependents/symbols are optional — they require gg index to have run.
	// But "optional" must not mean "silently empty": an agent that receives an
	// empty dependents list from a missing/unbuilt graph would wrongly conclude
	// "nothing depends on this" and fall back to grep. Surface the gap explicitly
	// (parity with the CLI's freshness/empty-graph notices in cmd/impact.go).
	gc, gcErr := graph.New(b.cfg.DataDir, b.cfg.ProjectID)
	if gcErr != nil {
		warnings = append(warnings, "code graph unavailable ("+gcErr.Error()+"): dependents and symbols omitted — knowledge-base results only. Run 'gg index' if you expected graph data.")
	}
	if gcErr == nil {
		defer func() { _ = gc.Close(ctx) }()
		if n, err := gc.CountFileNodes(ctx); err == nil && n == 0 {
			warnings = append(warnings, "code graph is empty (0 files indexed): an empty 'dependents' list is NOT proof that nothing depends on this target — run 'gg index' first.")
		}
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
		safeSearch(&wg, &decErr, func() {
			decisions, decErr = fallbackSearch(b.ggDir, "decisions", searchQuery,
				func() ([]store.Decision, error) { return b.store.SearchDecisions(ctx, vector, defaultLimit, true) },
				decisionFromEntry)
		})
		safeSearch(&wg, &taskErr, func() {
			tasks, taskErr = fallbackSearch(b.ggDir, "tasks", searchQuery,
				func() ([]store.Task, error) { return b.store.SearchTasks(ctx, vector, defaultLimit, true) },
				taskFromEntry)
		})
		safeSearch(&wg, &rejErr, func() {
			rejections, rejErr = fallbackSearch(b.ggDir, "rejections", searchQuery,
				func() ([]store.Rejection, error) { return b.store.SearchRejections(ctx, vector, defaultLimit) },
				rejectionFromEntry)
		})
		wg.Wait()
		if msg := firstSearchError(decErr, taskErr, rejErr); msg != "" {
			return TextBlock("gg_impact: " + msg), true
		}
		result["decisions"] = decisions
		result["tasks"] = tasks
		result["rejections"] = rejections
	}

	if len(warnings) > 0 {
		result["warnings"] = warnings
	}
	return jsonContent(result), false
}

// ── gg_def ────────────────────────────────────────────────────────────────

// toolDef answers "where is symbol X defined" from the code graph — the
// offline, grep-free complement to gg_impact. It resolves a bare symbol name to
// every Symbol node (a name can be defined in more than one file), returning
// file + kind for each. An empty match set is reported as a warning, never a
// silent empty list, so the agent does not read "not found" as "does not exist"
// when the graph is simply unbuilt.
func (b *brain) toolDef(ctx context.Context, args map[string]any) ([]ContentBlock, bool) {
	name := argString(args, "name")
	if name == "" {
		return TextBlock(`gg_def requires a "name" (symbol name, e.g. DependentsOf)`), true
	}
	gc, err := graph.New(b.cfg.DataDir, b.cfg.ProjectID)
	if err != nil {
		return TextBlock("code graph unavailable (run 'gg index'): " + err.Error()), true
	}
	defer func() { _ = gc.Close(ctx) }()
	matches, err := gc.FindSymbols(ctx, name)
	if err != nil {
		return TextBlock("gg_def: " + err.Error()), true
	}
	result := map[string]any{"query": name, "matches": matches}
	if len(matches) == 0 {
		result["warnings"] = []string{"no symbol named " + name + " in the code graph — run 'gg index', or it may be unexported/aliased (try gg_impact on the file, or grep as a last resort)"}
	}
	return jsonContent(result), false
}

// ── gg_uses ───────────────────────────────────────────────────────────────

// toolUses answers "which files reference symbol X" from the code graph — the
// symbol-exact, barrel-proof reverse blast-radius. It resolves the name to
// Symbol node(s) (optionally narrowed by file) and returns the referencing files
// per symbol. An empty result is reported as a warning, never a silent empty
// list, because REFERENCES edges exist only for the semantic (SCIP) tier.
func (b *brain) toolUses(ctx context.Context, args map[string]any) ([]ContentBlock, bool) {
	name := argString(args, "name")
	if name == "" {
		return TextBlock(`gg_uses requires a "name" (symbol name, e.g. CollapsePanel)`), true
	}
	file := argString(args, "file")

	gc, err := graph.New(b.cfg.DataDir, b.cfg.ProjectID)
	if err != nil {
		return TextBlock("code graph unavailable (run 'gg index'): " + err.Error()), true
	}
	defer func() { _ = gc.Close(ctx) }()

	matches, err := gc.FindSymbols(ctx, name)
	if err != nil {
		return TextBlock("gg_uses: " + err.Error()), true
	}
	if file != "" {
		filtered := matches[:0]
		for _, m := range matches {
			if m.SourceFile == file {
				filtered = append(filtered, m)
			}
		}
		matches = filtered
	}

	var warnings []string
	uses := make([]map[string]any, 0, len(matches))
	for _, m := range matches {
		refs, refErr := gc.ReferencersOf(ctx, m.ID)
		if refErr != nil {
			warnings = append(warnings, "referencers of "+m.Name+": "+refErr.Error())
			continue
		}
		uses = append(uses, map[string]any{"symbol": m, "referencers": refs})
	}
	if len(matches) == 0 {
		warnings = append(warnings, "no symbol named "+name+" in the code graph — run 'gg index', or it may be unexported/aliased; an empty result is not proof the symbol is unused")
	} else {
		warnings = append(warnings, "REFERENCES come from the semantic (SCIP) tier only; dynamic dispatch, reflection, and generated code may be missing")
	}

	result := map[string]any{"query": name, "uses": uses}
	if len(warnings) > 0 {
		result["warnings"] = warnings
	}
	return jsonContent(result), false
}
