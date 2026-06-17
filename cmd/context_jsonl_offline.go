package cmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
)

// serveContextFromJSONL answers `gg context <topic>` from the JSONL source of
// truth when Qdrant is down (BUG-075). It mirrors `gg search`'s offline
// behaviour: a live lexical scan of the durable brain files is preferred over
// the last-known-good cache, which can be up to 7 days stale. Falls back to the
// LKG cache only when JSONL yields nothing for the query.
func serveContextFromJSONL(cmd *cobra.Command, query string) error {
	ggDir, err := config.GGDir()
	if err != nil {
		return serveContextFromCache(cmd, query)
	}

	limit := int(contextLimit)
	var bundle contextBundle

	for _, e := range jsonlScan(ggDir, "decisions", query, limit) {
		bundle.decisions = append(bundle.decisions, decisionFromJSONLEntry(e))
	}
	for _, e := range jsonlScan(ggDir, "rejections", query, limit) {
		bundle.rejections = append(bundle.rejections, rejectionFromJSONLEntry(e))
	}
	for _, e := range jsonlScan(ggDir, "tasks", query, limit) {
		bundle.tasks = append(bundle.tasks, taskFromJSONLEntry(e))
	}
	for _, e := range jsonlScan(ggDir, "notes", query, limit) {
		bundle.notes = append(bundle.notes, noteFromJSONLEntry(e))
	}

	total := len(bundle.decisions) + len(bundle.rejections) + len(bundle.tasks) + len(bundle.notes)
	if total == 0 {
		// Nothing live to show — defer to the cache (still better than empty).
		return serveContextFromCache(cmd, query)
	}

	warn := []string{"⚠ vector store offline — results from a live JSONL scan of durable brain files (semantic ranking unavailable; discussions/artifacts omitted)."}
	// Live JSONL data, not a dated cache snapshot — pass a zero time (no staleness banner).
	return printContextBundle(cmd, query, bundle, warn, time.Time{})
}

// jsonlScan returns up to limit folded-latest brain entries matching query.
func jsonlScan(ggDir, kind, query string, limit int) []brain.Entry {
	matches, err := brain.SearchByText(ggDir, kind, query)
	if err != nil {
		return nil
	}
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}
