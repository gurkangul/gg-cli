package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

func runContextForTaskJSONLFallback(cmd *cobra.Command, taskID, reason string) error {
	taskRef, err := normalizeTaskRef(taskID)
	if err != nil {
		return err
	}
	ggDir := config.GGDirOrEmpty()
	if ggDir == "" {
		return fmt.Errorf("%s — no .gg directory for JSONL fallback", reason)
	}

	entries, err := brain.ReadAll(ggDir, "tasks")
	if err != nil {
		return fmt.Errorf("%s — read tasks JSONL fallback: %w", reason, err)
	}
	tasksByID := make(map[string]store.Task, len(entries))
	for _, e := range entries {
		t := taskFromJSONLEntry(e)
		if t.ID != "" {
			tasksByID[t.ID] = t
		}
	}
	anchor, ok := tasksByID[taskRef]
	if !ok {
		return fmt.Errorf("%s — task %s not found in JSONL fallback", reason, taskRef)
	}

	deps := make([]store.Task, 0, len(anchor.DependsOn))
	for _, depID := range anchor.DependsOn {
		if dep, exists := tasksByID[depID]; exists {
			deps = append(deps, dep)
		}
	}

	queryText := strings.Join([]string{anchor.ID, anchor.Title, anchor.Detail, strings.Join(anchor.Tags, " ")}, " ")
	bundle := contextBundle{
		decisions:  jsonlDecisionsForTask(ggDir, anchor.ID, queryText, contextLimit),
		rejections: jsonlRejectionsForTask(ggDir, anchor.ID, queryText, contextLimit),
		notes:      jsonlNotesForTask(ggDir, anchor.ID, queryText, contextLimit),
	}
	messages := jsonlMessagesForTask(ggDir, anchor.ID, 5)
	tb := taskContextBundle{anchor: anchor, deps: deps, bundle: bundle, messages: messages}
	warnings := []string{reason + " — served from JSONL fallback; semantic ranking unavailable. Run: gg doctor"}

	return printJSON(map[string]any{
		"task":           anchor,
		"deps":           deps,
		"decisions":      bundle.decisions,
		"rejections":     bundle.rejections,
		"notes":          bundle.notes,
		"messages":       messages,
		"warnings":       warnings,
		"source_backend": "jsonl",
		"degraded":       true,
	}, func() {
		if isCompactActive(cmd) {
			emitCompact(cmd, "context",
				func(w io.Writer) { renderForTask(w, tb, warnings) },
				func(w io.Writer) { renderForTaskCompact(w, tb, warnings) },
				compactRendererV_context,
			)
			return
		}
		emitHydration(cmd, "context", false, func(w io.Writer) { renderForTask(w, tb, warnings) })
		renderForTask(cmd.OutOrStdout(), tb, warnings)
	})
}

func jsonlDecisionsForTask(ggDir, taskID, query string, limit uint64) []store.Decision {
	entries := linkedAndMatchedJSONLEntries(ggDir, "decisions", taskID, query)
	out := make([]store.Decision, 0, len(entries))
	for _, e := range entries {
		out = append(out, decisionFromJSONLEntry(e))
	}
	return trimJSONLDecisions(out, limit)
}

func jsonlRejectionsForTask(ggDir, taskID, query string, limit uint64) []store.Rejection {
	entries := linkedAndMatchedJSONLEntries(ggDir, "rejections", taskID, query)
	out := make([]store.Rejection, 0, len(entries))
	for _, e := range entries {
		out = append(out, rejectionFromJSONLEntry(e))
	}
	return trimJSONLRejections(out, limit)
}

func jsonlNotesForTask(ggDir, taskID, query string, limit uint64) []store.Note {
	entries := linkedAndMatchedJSONLEntries(ggDir, "notes", taskID, query)
	out := make([]store.Note, 0, len(entries))
	for _, e := range entries {
		out = append(out, noteFromJSONLEntry(e))
	}
	return trimJSONLNotes(out, limit)
}

func linkedAndMatchedJSONLEntries(ggDir, kind, taskID, query string) []brain.Entry {
	seen := map[string]bool{}
	var out []brain.Entry
	all, _ := brain.ReadAll(ggDir, kind)
	for _, e := range all {
		if stringPayload(e.Payload, "task_id", "") == taskID {
			out = append(out, e)
			seen[e.UUID] = true
		}
	}
	matches, _ := brain.SearchByText(ggDir, kind, query)
	for _, e := range matches {
		if seen[e.UUID] {
			continue
		}
		out = append(out, e)
		seen[e.UUID] = true
	}
	return out
}

func jsonlMessagesForTask(ggDir, taskID string, limit int) []store.Message {
	entries, _ := brain.ReadAll(ggDir, "messages")
	var out []store.Message
	for _, e := range entries {
		m := messageFromJSONLEntry(e)
		if m.TaskID == taskID {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func trimJSONLDecisions(in []store.Decision, limit uint64) []store.Decision {
	if limit == 0 || len(in) <= int(limit) {
		return in
	}
	return in[:limit]
}

func trimJSONLRejections(in []store.Rejection, limit uint64) []store.Rejection {
	if limit == 0 || len(in) <= int(limit) {
		return in
	}
	return in[:limit]
}

func trimJSONLNotes(in []store.Note, limit uint64) []store.Note {
	if limit == 0 || len(in) <= int(limit) {
		return in
	}
	return in[:limit]
}
