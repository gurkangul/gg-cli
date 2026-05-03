package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func printSearchMatches(cmd *cobra.Command, query string, results []searchResult, banner string, cachedAt time.Time, warnings []string) error {
	jsonMap := map[string]any{
		"matches":  results,
		"ranking":  "semantic results with deterministic lexical exact-match boost; BM25/sparse fallback not required",
		"warnings": warnings,
	}
	if banner != "" {
		jsonMap["warning"] = banner
		if !cachedAt.IsZero() {
			jsonMap["cached_at"] = cachedAt.UTC().Format(time.RFC3339)
			jsonMap["stale_seconds"] = int(time.Since(cachedAt).Seconds())
		}
	}
	return printJSON(jsonMap, func() {
		if banner != "" {
			fmt.Fprintln(cmd.OutOrStderr(), banner)
		}
		if len(warnings) > 0 {
			fmt.Fprintf(cmd.OutOrStderr(), "Warnings:\n  %s\n", strings.Join(warnings, "\n  "))
		}
		if isCompactActive(cmd) {
			emitCompact(cmd, "search",
				func(w io.Writer) { renderSearchResultsDefault(w, results) },
				func(w io.Writer) { renderSearchResultsCompact(w, results) },
				compactRendererV_search,
			)
			return
		}
		renderSearchResultsDefault(os.Stdout, results)
	})
}
