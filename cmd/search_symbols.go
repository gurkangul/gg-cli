package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

// search_symbols.go — the TASK-505 lexical code-symbol tier for `gg search`.
//
// gg search is semantic-vector first; exact identifier lookups (e.g.
// "ForwardCallFlow") and camelCase fragments ("complex" → "complexOperation")
// rank worse under embedding similarity than a keyword match would. This adds a
// bm25/FTS5 lexical tier over the embedded code-graph symbol index, surfaced
// alongside — and clearly labeled apart from — the semantic results. The
// semantic default and the --compact contract are unchanged: the symbol block
// is purely additive (nothing prints when there are no symbol hits).

// lexicalSymbolMatches runs the FTS5 lexical symbol lookup against the embedded
// code graph for the current project. It is fully best-effort — any config,
// graph-open, or query failure yields no matches and never fails search.
func lexicalSymbolMatches(ctx context.Context, query string, limit int) []graph.SymbolMatch {
	cfg, err := config.Load()
	if err != nil || cfg.ProjectID == "" {
		return nil
	}
	gc, err := graph.New(cfg.DataDir, cfg.ProjectID)
	if err != nil {
		return nil
	}
	defer func() { _ = gc.Close(ctx) }()
	matches, err := gc.SearchSymbolsFTS(ctx, query, limit)
	if err != nil {
		return nil
	}
	return matches
}

// printSearchResultsWithSymbols renders the live semantic results plus the
// lexical symbol tier. It mirrors the semantic printer but adds a clearly
// labeled "SYMBOL MATCHES (lexical)" block and a symbol_matches JSON key. The
// semantic results are built and rendered exactly as the semantic-only printer
// does, so default/compact/JSON behaviour for them is unchanged.
func printSearchResultsWithSymbols(cmd *cobra.Command, query string, decisions []store.Decision, rejections []store.Rejection, tasks []store.Task, bugs []store.Bug, notes []store.Note, symbols []graph.SymbolMatch, banner string, cachedAt time.Time) error {
	const backend = "sqlite"
	results := trimSearchResults(buildSearchResultsWithBackendScoresAndMessages(query, decisions, rejections, tasks, bugs, notes, nil, backend, nil), searchLimit)
	jsonMap := map[string]any{
		"decisions":  decisions,
		"rejections": rejections,
		"tasks":      tasks,
		"bugs":       bugs,
		"notes":      notes,
		// messages: this live-semantic path never carries messages, but the
		// canonical printer (printSearchResultsWithBackendScoresAndMessages)
		// always emits the key (as null when empty). Emit it here too for --json
		// schema parity so consumers see a stable set of keys. TASK-509.
		"messages":       []store.Message(nil),
		"matches":        results,
		"symbol_matches": symbols,
		"ranking":        "semantic results plus a bm25/FTS5 lexical code-symbol tier (exact/keyword identifier matches)",
		"source_backend": backend,
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
		if isCompactActive(cmd) {
			emitCompact(cmd, "search",
				func(w io.Writer) {
					renderSymbolMatches(w, symbols, false)
					renderSearchResultsDefault(w, results)
				},
				func(w io.Writer) {
					renderSymbolMatches(w, symbols, true)
					renderSearchResultsCompact(w, results)
				},
				compactRendererV_search,
			)
			return
		}
		renderSymbolMatches(os.Stdout, symbols, false)
		renderSearchResultsDefault(os.Stdout, results)
	})
}

// renderSymbolMatches prints the lexical code-symbol tier. Empty input prints
// nothing (the semantic results stand alone, preserving the prior output for
// queries with no symbol hits). compact uses a single line per symbol.
func renderSymbolMatches(w io.Writer, symbols []graph.SymbolMatch, compact bool) {
	if len(symbols) == 0 {
		return
	}
	if compact {
		fmt.Fprintf(w, "symbols — %d lexical\n", len(symbols))
		for _, sym := range symbols {
			fmt.Fprintln(w, compactSymbolMatchLine(sym))
		}
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintln(w, "SYMBOL MATCHES (lexical):")
	for _, sym := range symbols {
		kind := sym.Kind
		if kind == "" {
			kind = "symbol"
		}
		fmt.Fprintf(w, "  ⌖ %s (%s)", sym.Name, kind)
		if sym.SourceFile != "" {
			fmt.Fprintf(w, " — %s", sym.SourceFile)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w)
}

// compactSymbolMatchLine is the one-line compact form for a symbol match.
func compactSymbolMatchLine(sym graph.SymbolMatch) string {
	kind := sym.Kind
	if kind == "" {
		kind = "sym"
	}
	if sym.SourceFile != "" {
		return fmt.Sprintf("⌖ %s [%s] %s", sym.Name, kind, sym.SourceFile)
	}
	return fmt.Sprintf("⌖ %s [%s]", sym.Name, kind)
}
